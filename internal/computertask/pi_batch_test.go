package computertask

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type batchAssets struct {
	mu   sync.Mutex
	data map[string][]byte
	fail bool
}

func (a *batchAssets) Ingest(_ context.Context, in media.IngestInput) (media.Asset, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fail {
		return media.Asset{}, errors.New("PRIVATE-storage-diagnostic")
	}
	b, err := io.ReadAll(in.Reader)
	if err != nil {
		return media.Asset{}, err
	}
	if len(b) == 0 {
		return media.Asset{}, errors.New("asset payload is empty")
	}
	h := sha256.Sum256(b)
	id := hex.EncodeToString(h[:])
	a.data[id] = b
	return media.Asset{ContentHash: id}, nil
}

func (a *batchAssets) Open(_ context.Context, _, hash string) (io.ReadCloser, media.Asset, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.data[hash]
	if !ok {
		return nil, media.Asset{}, errors.New("PRIVATE-storage-diagnostic")
	}
	return io.NopCloser(bytes.NewReader(b)), media.Asset{ContentHash: hash}, nil
}

// This fixture is a standard MCP -> service -> fixed Agenvo transport test.
// Pi/Herdr are synthetic: it is not a vendor binary or Windows acceptance.
type batchTransport struct {
	t                                                *testing.T
	store                                            *fixtureStore
	runs, reads                                      int
	producerWakes                                    int
	calls                                            []string
	progress                                         agenvo.BatchProgress
	receipt                                          agenvo.BatchReceipt
	stdout, stderr                                   []byte
	producerFeedback                                 []byte
	control                                          string
	missing, hiddenFrame, corrupt, changedPane, lost bool
	reason                                           string
	exit                                             int
	onPage                                           func()
	readSchema                                       *jsonschema.Resolved
	columns, delay, remaining, maxLines, oversized   int
	pending                                          string
	rejectRead                                       bool
	pageOffsets                                      []int
}

func syntheticPiStream(session, reason string) []byte {
	var out bytes.Buffer
	for _, event := range []any{
		map[string]any{"type": "session", "version": 3, "id": session},
		map[string]string{"type": "agent_start"},
		map[string]string{"type": "turn_start"},
		map[string]any{"type": "message_start", "message": map[string]string{"role": "assistant"}},
		map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "provider": "synthetic", "model": "synthetic-model", "stopReason": reason, "content": []any{map[string]string{"type": "text", "text": "42 中文 ✓"}}}},
		map[string]string{"type": "turn_end"},
		map[string]any{"type": "agent_end", "willRetry": false},
		map[string]any{"type": "agent_settled", "aborted": false},
	} {
		raw, _ := json.Marshal(event)
		out.Write(raw)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func batchFile(name string, raw []byte) agenvo.BatchFile {
	h := sha256.Sum256(raw)
	return agenvo.BatchFile{Name: name, Hash: hex.EncodeToString(h[:]), Size: int64(len(raw))}
}

func (f *batchTransport) call(_ context.Context, owner, connection, tool string, args map[string]any) (json.RawMessage, error) {
	f.t.Helper()
	if owner != "owner" || connection != "11111111-1111-4111-8111-111111111111" || tool != "execute" {
		f.t.Fatal("wrong relay identity")
	}
	code, _ := args["code"].(string)
	var parts []json.RawMessage
	if json.Unmarshal([]byte("["+strings.TrimSuffix(strings.TrimPrefix(code, "return await call("), ");")+"]"), &parts) != nil || len(parts) != 3 {
		f.t.Fatal("arbitrary relay call")
	}
	var target map[string]string
	var method string
	var params map[string]any
	_ = json.Unmarshal(parts[0], &target)
	_ = json.Unmarshal(parts[1], &method)
	_ = json.Unmarshal(parts[2], &params)
	if target["deviceId"] != "device-a" || target["instanceId"] != "herdr-a" {
		f.t.Fatalf("wrong device mapping: %v", target)
	}
	f.calls = append(f.calls, method)
	body := map[string]any{}
	switch method {
	case "workspace.create":
		body = map[string]any{"workspace": map[string]any{"workspace_id": "w-test", "label": params["label"]}, "root_pane": map[string]string{"pane_id": "control", "terminal_id": "control-terminal", "workspace_id": "w-test"}}
	case "tab.create":
		body = map[string]any{"root_pane": map[string]string{"pane_id": "job", "terminal_id": "job-terminal", "workspace_id": "w-test"}}
	case "pane.run":
		command, _ := params["command"].(string)
		if command == `& { herdr plugin action invoke collect-result --plugin memoh.delivery *> $null }` {
			if params["paneId"] != "control" {
				f.t.Fatal("producer wake on Pi pane")
			}
			f.producerWakes++
			return envelope(map[string]any{"session": "synthetic", "result": map[string]string{"type": "accepted"}}), nil
		}
		match := regexp.MustCompile(`printf %s ([A-Za-z0-9+/=]+) \| base64`).FindStringSubmatch(command)
		if len(match) != 2 {
			f.t.Fatal("missing fixed Bash launch")
		}
		raw, _ := base64.StdEncoding.DecodeString(match[1])
		script := string(raw)
		switch params["paneId"] {
		case "job":
			f.runs++
			f.store.mu.Lock()
			for _, r := range f.store.records {
				var d Details
				_ = json.Unmarshal(r.Details, &d)
				if d.Batch != nil && d.Batch.Progress.Phase == "batch_submitting" {
					f.progress = d.Batch.Progress
				}
			}
			f.store.mu.Unlock()
			p := f.progress
			if p.Attempt == "" || !strings.Contains(script, "pi --mode json") || !strings.Contains(script, p.Attempt) || !strings.Contains(script, p.PiSessionID) {
				f.t.Fatal("dispatch preceded durable identity or wrong CLI")
			}
			f.stdout = syntheticPiStream(p.PiSessionID, f.reason)
			// More than one page and the old 64KiB relay limit, never in one call.
			f.stderr = []byte(strings.Repeat("diagnostic 中文\n", 6000))
			f.receipt = agenvo.BatchReceipt{Version: 1, Attempt: p.Attempt, Generation: p.Generation, RequestHash: p.RequestHash, PiSessionID: p.PiSessionID, WorkspaceID: p.WorkspaceID, PaneID: p.PaneID, TerminalID: p.TerminalID, Exited: true, Started: true, ExitCode: f.exit, Stdout: batchFile("stdout.jsonl", f.stdout), Stderr: batchFile("stderr.txt", f.stderr)}
			if f.lost {
				return nil, errors.New("lost dispatch reply")
			}
		case "control":
			f.reads++
			if strings.Contains(script, "pi --mode") {
				f.t.Fatal("Pi on control pane")
			}
			m := regexp.MustCompile(`MEMOH_FILE_([a-f0-9]{32}):`).FindStringSubmatch(script)
			page := regexp.MustCompile(`skip=(\d+) count=(\d+)`).FindStringSubmatch(script)
			if len(m) != 2 || len(page) != 3 {
				f.t.Fatal("not bounded fixed file read")
			}
			offset, _ := strconv.Atoi(page[1])
			count, _ := strconv.Atoi(page[2])
			f.pageOffsets = append(f.pageOffsets, offset)
			data := f.stdout
			if strings.Contains(script, "/stderr.txt\"") {
				data = f.stderr
			}
			if strings.Contains(script, "/receipt.json\"") {
				data, _ = json.Marshal(f.receipt)
				if f.missing {
					data = nil
				}
			}
			limit := agenvo.BatchPageBytes
			if strings.Contains(script, "/receipt.json\"") {
				limit = 4096 // Separate complete-receipt budget, not data-page size.
			}
			if strings.Contains(script, "/delivery-feedback.json\"") {
				data = f.producerFeedback
				limit = 8192
			}
			if count > limit || offset > len(data) {
				f.t.Fatal("page exceeds bounds")
			}
			data = append([]byte(nil), data[offset:min(offset+count, len(data))]...)
			if f.corrupt && len(data) > 0 {
				data[0] ^= 1
			}
			f.control += "\r\nPS C:\\Synthetic> " + command + "\r\n"
			f.pending = "\r\nMEMOH_FILE_" + m[1] + ":" + base64.StdEncoding.EncodeToString(data) + ":END_FILE_" + m[1] + "\r\nPS C:\\Synthetic>\r\n"
			f.remaining = f.delay
			if f.onPage != nil {
				f.onPage()
			}
		default:
			f.t.Fatal("query or dispatch on an unowned pane")
		}
	case "pane.get":
		terminal := "control-terminal"
		if f.changedPane {
			terminal = "reused-terminal"
		}
		body = map[string]any{"type": "pane_info", "pane": map[string]string{"pane_id": "control", "terminal_id": terminal, "workspace_id": f.progress.WorkspaceID}}
	case "pane.read":
		if err := f.readSchema.Validate(params); err != nil {
			f.t.Errorf("Agenvo v0.2.0 pane.read schema rejected params: %v", err)
			return nil, err
		}
		if strings.HasPrefix(params["paneId"].(string), "-") {
			return nil, errors.New("vendor pane ID refinement rejected leading dash")
		}
		lines := int(params["lines"].(float64))
		f.maxLines = max(f.maxLines, lines)
		if f.rejectRead {
			return batchReadError("not_started", "invalid_params"), nil
		}
		if params["paneId"] == "control" && f.pending != "" {
			if f.remaining > 0 {
				if lines > 8 {
					f.t.Error("expanded into unrelated history before current response arrived")
				}
				f.remaining--
			} else {
				f.control += f.pending
				f.pending = ""
			}
		}
		text := f.control
		if params["paneId"] == f.progress.PaneID {
			text = "prompt without receipt"
			if !f.missing && !f.hiddenFrame {
				raw, _ := json.Marshal(f.receipt)
				text = "echo is not a receipt\r\nMEMOH_BATCH_" + f.progress.Generation + ":" + base64.StdEncoding.EncodeToString(raw) + ":END_BATCH_" + f.progress.Generation + "\r\n"
			}
		}
		// Accumulated history, command echo, delayed current frame and prompt
		// all consume rendered rows before recent-unwrapped joins them.
		if params["paneId"] == "control" {
			text = renderedTail(text, f.columns, lines)
		}
		bounded, _ := json.Marshal(map[string]any{"execution": "accepted", "result": map[string]any{"session": "synthetic", "output": text}})
		if len(bounded) >= 64*1024-2048 {
			f.oversized++
			return batchReadError("accepted", "result_too_large"), nil
		}
		return envelope(map[string]any{"session": "synthetic", "output": text}), nil
	default:
		f.t.Fatalf("unexpected native operation: %s", method)
	}
	return envelope(map[string]any{"session": "synthetic", "result": body}), nil
}

func batchReadError(execution, code string) json.RawMessage {
	body, _ := json.Marshal(map[string]any{"execution": execution, "error": map[string]string{"code": code, "message": "PRIVATE-read-diagnostic"}})
	raw, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": string(body)}}})
	return raw
}

func renderedTail(text string, columns, limit int) string {
	rows := []string{}
	for _, line := range strings.SplitAfter(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		for len(line) > columns {
			rows = append(rows, line[:columns])
			line = line[columns:]
		}
		if line != "" {
			rows = append(rows, line)
		}
	}
	return strings.Join(rows[max(0, len(rows)-limit):], "")
}

func setupBatch(t *testing.T) (*Source, *fixtureStore, *batchTransport, *batchAssets) {
	t.Helper()
	source, store, _ := setup(t)
	r := store.mounts["pc-a"]
	var target agenvo.Target
	_ = json.Unmarshal(r.RuntimeProviderConfig, &target)
	target.Agent = agenvo.AgentProfile{Provider: "synthetic", Model: "synthetic-model", ConfigDir: `C:\Synthetic\Pi`, ExecutionMode: "pi-json-v1"}
	r.RuntimeProviderConfig, _ = json.Marshal(target)
	store.mounts["pc-a"] = r
	raw, err := os.ReadFile("testdata/agenvo-v0.2.0-pane-read.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &batchTransport{t: t, store: store, reason: "stop", readSchema: resolved, columns: 80}
	a := &batchAssets{data: map[string][]byte{}}
	source.service.relay = agenvo.NewService(store, permissionChecker{}, f.call)
	source.service.assets = a
	return source, store, f, a
}

func batchResultUntilReady(t *testing.T, c *sdkmcp.ClientSession, id string) Result {
	t.Helper()
	for range 10 {
		r := parseResult(t, invoke(t, c, "computer_task_result", map[string]any{"operation_id": id}, false))
		if r.PiResult != nil {
			return r
		}
	}
	t.Fatal("bounded observations did not finish stored byte offsets")
	return Result{}
}

func TestStandardMCPPiBatchCompletesWithReadyHashedResults(t *testing.T) {
	source, _, f, a := setupBatch(t)
	f.hiddenFrame = true // Recover the durable receipt after terminal eviction.
	c := standardClient(t, source, "pc-a", false)
	listing := invoke(t, c, "computer_targets", map[string]any{}, false)
	if !strings.Contains(string(listing), "pi-json-v1") || strings.Contains(string(listing), "pc-b") {
		t.Fatal("wrong inventory scope/protocol")
	}
	for _, target := range []string{"pc-b", "native"} {
		invoke(t, c, "computer_task_submit", map[string]any{"target_id": target, "kind": "herdr_pi_task", "task": "compute 42"}, true)
	}
	if len(f.calls) != 0 {
		t.Fatal("denial dispatched")
	}
	args := map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}
	r := parseResult(t, invoke(t, c, "computer_task_submit", args, false))
	if r.State != "submitted" || r.ProcessStopped || r.PiResult != nil || f.runs != 1 {
		t.Fatalf("submission=%+v", r)
	}
	if f.progress.LayoutVersion != agenvo.BatchLayoutVersion {
		t.Fatal("new operation did not persist its layout before dispatch")
	}
	got := batchResultUntilReady(t, c, r.ID)
	if got.State != "completed" || !got.ProcessStopped || got.ExitCode == nil || *got.ExitCode != 0 || got.PiResult == nil || got.PiResult.Outcome != "succeeded" || got.Output != "42 中文 ✓" {
		t.Fatalf("result=%+v", got)
	}
	if got.StdoutHash != f.receipt.Stdout.Hash || got.StderrHash != f.receipt.Stderr.Hash || f.reads < 3 {
		t.Fatal("missing paged ready refs")
	}
	if !bytes.Equal(a.data[got.StderrHash], f.stderr) {
		t.Fatal("large result lost")
	}
	before := len(f.calls)
	for _, tool := range []string{"computer_task_get", "computer_task_result", "computer_task_artifact"} {
		invoke(t, c, tool, map[string]any{"operation_id": r.ID}, false)
	}
	again := parseResult(t, invoke(t, c, "computer_task_submit", args, false))
	if again.ID != r.ID || len(f.calls) != before {
		t.Fatal("completed task or results re-dispatched")
	}
}

func TestStandardMCPPiBatchUnknownDoesNotRetryAndCancellationStaysHonest(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	f.lost = true
	f.missing = true
	c := standardClient(t, source, "pc-a", false)
	r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}, true))
	if r.State != "unknown" || f.runs != 1 {
		t.Fatal("lost submission not retained")
	}
	for i := 0; i < 2; i++ {
		blocked := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": fmt.Sprintf("other %d", i)}, true))
		if blocked.BlockedByOperation != r.ID {
			t.Fatal("unknown task not fenced")
		}
	}
	cancel := parseResult(t, invoke(t, c, "computer_task_cancel", map[string]any{"operation_id": r.ID}, false))
	if !cancel.CancelRequested || cancel.ProcessStopped {
		t.Fatal("pretended cancellation")
	}
	got := parseResult(t, invoke(t, c, "computer_task_result", map[string]any{"operation_id": r.ID}, false))
	if got.PiResult != nil || got.ProcessStopped || got.State != "unknown" {
		t.Fatal("missing receipt completed")
	}
	f.missing = false
	got = parseResult(t, invoke(t, c, "computer_task_result", map[string]any{"operation_id": r.ID}, false))
	if !got.CancelRequested || !got.ProcessStopped || f.runs != 1 {
		t.Fatal("late exit rewrote cancellation intent or resubmitted")
	}
	store.revoked = true
	before := len(f.calls)
	for _, tool := range []string{"computer_task_get", "computer_task_result", "computer_task_artifact", "computer_task_cancel"} {
		invoke(t, c, tool, map[string]any{"operation_id": r.ID}, true)
	}
	if before != len(f.calls) {
		t.Fatal("revoked observation reached relay")
	}
}

func TestStandardMCPPiBatchFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"exit0_error", "nonzero", "output_limit", "bad_hash", "reused_control", "storage_failure", "wrong_receipt", "revoked_mid_read"} {
		t.Run(mode, func(t *testing.T) {
			source, store, f, a := setupBatch(t)
			if mode == "exit0_error" {
				f.reason = "error"
			}
			if mode == "nonzero" {
				f.exit = 7
			}
			c := standardClient(t, source, "pc-a", false)
			r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}, false))
			switch mode {
			case "output_limit":
				f.receipt.Stdout.Size = 65 << 20
			case "bad_hash":
				f.corrupt = true
			case "reused_control":
				f.changedPane = true
			case "storage_failure":
				a.fail = true
			case "wrong_receipt":
				f.receipt.RequestHash = strings.Repeat("0", 64)
			case "revoked_mid_read":
				f.onPage = func() { store.revoked = true }
			}
			wantError := mode != "exit0_error" && mode != "nonzero" && mode != "output_limit"
			raw := invoke(t, c, "computer_task_result", map[string]any{"operation_id": r.ID}, wantError)
			if strings.Contains(string(raw), "PRIVATE-") {
				t.Fatal("private cause leaked")
			}
			if !wantError {
				got := parseResult(t, raw)
				if got.PiResult == nil {
					got = batchResultUntilReady(t, c, r.ID)
				}
				outcome := "failed"
				if mode == "output_limit" {
					outcome = "output_limit"
				}
				if got.PiResult == nil || got.PiResult.Outcome != outcome || !got.ProcessStopped {
					t.Fatalf("failed invocation=%+v", got)
				}
				if mode == "output_limit" && (f.reads != 0 || got.StdoutHash != "" || got.StderrHash != "") {
					t.Fatal("oversized output imported")
				}
			} else {
				store.mu.Lock()
				persisted := store.records[r.ID]
				store.mu.Unlock()
				var d Details
				_ = json.Unmarshal(persisted.Details, &d)
				if d.Batch.Result != nil {
					t.Fatal("failure published Pi success")
				}
				if mode == "wrong_receipt" && persisted.State == "completed" {
					t.Fatal("unrelated receipt released slot")
				}
			}
			if f.runs != 1 {
				t.Fatal("automatic Pi retry")
			}
		})
	}
}

func TestStandardMCPPiBatchRejectsOversizeBeforeClaim(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": strings.Repeat("x", 32000)}, true)
	if len(store.records) != 0 || len(f.calls) != 0 {
		t.Fatal("oversized launch reserved or dispatched")
	}
}

func TestStandardMCPPiBatchEmptyOutputsDoNotInventMediaRefs(t *testing.T) {
	for _, mode := range []string{"stderr", "stdout", "not_started"} {
		t.Run(mode, func(t *testing.T) {
			source, _, f, _ := setupBatch(t)
			// Exercise the actual media spool/hash/persistence contract, not
			// a permissive fake that accidentally accepts zero-byte assets.
			assets := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
			assets.SetRequirePersistence(true)
			if _, err := assets.Ingest(t.Context(), media.IngestInput{BotID: "bot-a", Reader: strings.NewReader("")}); err == nil {
				t.Fatal("media empty-payload contract changed")
			}
			source.service.assets = assets
			c := standardClient(t, source, "pc-a", false)
			r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}, false))
			if mode != "stdout" {
				f.stderr = nil
				f.receipt.Stderr = batchFile("stderr.txt", nil)
			}
			if mode != "stderr" {
				f.stdout = nil
				f.receipt.Stdout = batchFile("stdout.jsonl", nil)
			}
			if mode == "not_started" {
				f.receipt.Started = false
				f.receipt.ExitCode = 125
			}
			got := batchResultUntilReady(t, c, r.ID)
			want := map[string]string{"stderr": "succeeded", "stdout": "invalid_protocol", "not_started": "not_started"}[mode]
			if !got.ProcessStopped || got.PiResult == nil || got.PiResult.Outcome != want {
				t.Fatalf("empty output=%+v", got)
			}
			if mode != "stdout" && (!got.StderrEmpty || got.StderrHash != "") {
				t.Fatal("empty stderr claimed a media ref")
			}
			if mode != "stderr" && (!got.StdoutEmpty || got.StdoutHash != "") {
				t.Fatal("empty stdout claimed a media ref")
			}
		})
	}
}

func TestStandardMCPPiBatchDelayedFramesWithinVendorLimits(t *testing.T) {
	for _, columns := range []int{16, 43, 44, 80, 240, 16384} {
		t.Run(strconv.Itoa(columns), func(t *testing.T) {
			source, _, f, _ := setupBatch(t)
			f.columns, f.delay, f.hiddenFrame = columns, 2, true
			// Old 16 KiB pages and command echoes remain in the same pane.
			// At very wide widths even eight legal rendered rows can exceed
			// the connector's actual 64 KiB minus envelope allowance.
			f.control = strings.Repeat("old-frame:"+strings.Repeat("x", 21848)+":old-end\r\n", 8)
			if columns == 16384 {
				f.control += strings.Repeat("historical decoration ", 20000) + "\r\n"
			}
			c := standardClient(t, source, "pc-a", false)
			r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}, false))
			f.stderr = []byte(strings.Repeat("z", 6000))
			f.receipt.Stderr = batchFile("stderr.txt", f.stderr)
			got := batchResultUntilReady(t, c, r.ID)
			if got.PiResult.Outcome != "succeeded" || got.StderrHash != f.receipt.Stderr.Hash || f.maxLines > 500 || f.runs != 1 || f.reads != 5 {
				t.Fatalf("result or single read dispatch contract: result=%+v runs=%d reads=%d lines=%d", got, f.runs, f.reads, f.maxLines)
			}
			if columns == 16384 && f.oversized == 0 {
				t.Fatal("fixture did not exercise actual oversized history rejection")
			}
		})
	}
}

func TestAgenvoV020PaneReadSchemaBounds(t *testing.T) {
	_, _, f, _ := setupBatch(t)
	for _, n := range []int{1, 8, 256, 500} {
		if err := f.readSchema.Validate(map[string]any{"session": "synthetic", "paneId": "control", "lines": n, "source": "recent-unwrapped"}); err != nil {
			t.Fatalf("vendor valid lines %d: %v", n, err)
		}
	}
	for _, n := range []int{0, 501, 512, 1000} {
		if err := f.readSchema.Validate(map[string]any{"session": "synthetic", "paneId": "control", "lines": n}); err == nil {
			t.Fatalf("vendor invalid lines %d accepted", n)
		}
	}
	if err := f.readSchema.Validate(map[string]any{"session": "synthetic", "paneId": "control", "lines": 500, "raw": true}); err == nil {
		t.Fatal("invented pane.read parameter accepted")
	}
}

func TestStandardMCPPiBatchReadRejectionPreservesSubmittedOperation(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}, false))
	f.rejectRead = true
	raw := invoke(t, c, "computer_task_result", map[string]any{"operation_id": r.ID}, true)
	got := parseResult(t, raw)
	if got.ID != r.ID || got.State != "submitted" || got.ProcessStopped || got.PiResult != nil || strings.Contains(string(raw), "not_started") || strings.Contains(string(raw), "PRIVATE-") {
		t.Fatalf("read rejection changed task outcome: %s", raw)
	}
	store.mu.Lock()
	persisted := store.records[r.ID]
	store.mu.Unlock()
	if persisted.State != "submitted" {
		t.Fatal("read rejection released invocation slot")
	}
	blocked := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "different task"}, true))
	if blocked.BlockedByOperation != r.ID {
		t.Fatal("unknown read permitted another task")
	}
	f.rejectRead = false
	got = batchResultUntilReady(t, c, r.ID)
	if got.PiResult.Outcome != "succeeded" || f.runs != 1 {
		t.Fatal("existing submitted operation did not recover without redispatch")
	}
}

func TestStandardMCPPiBatchResumesOldPageOffsetsAfterServiceRestart(t *testing.T) {
	source, store, f, a := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}, false))
	// Seed the durable format from the previous 16 KiB reader. No attempt,
	// identity, chunk hash or byte offset may be reset by the corrected reader.
	stdout := batchFile("stdout.jsonl", f.stdout)
	prefix := batchFile("stderr.txt", f.stderr[:16384])
	a.data[stdout.Hash], a.data[prefix.Hash] = f.stdout, f.stderr[:16384]
	store.mu.Lock()
	old := store.records[r.ID]
	var d Details
	_ = json.Unmarshal(old.Details, &d)
	attempt := d.Batch.Progress.Attempt
	d.Batch.Progress.LayoutVersion = 0 // Historical JSON had no layout_version.
	d.Batch.Receipt = &f.receipt
	d.Batch.Files = []BatchTransfer{
		{Meta: stdout, Offset: stdout.Size, ReadyHash: stdout.Hash, Chunks: []BatchChunk{{Hash: stdout.Hash, Size: stdout.Size}}},
		{Meta: f.receipt.Stderr, Offset: 16384, Chunks: []BatchChunk{{Hash: prefix.Hash, Size: 16384}}},
	}
	d.Phase, old.State = "pi_exited_output_pending", "completed"
	d.ExitCode = &f.receipt.ExitCode
	old.Details, _ = json.Marshal(d)
	if bytes.Contains(old.Details, []byte("layout_version")) {
		t.Fatal("legacy fixture contains a layout version")
	}
	store.records[r.ID] = old
	store.mu.Unlock()
	restarted := NewSource(NewService(store, store, source.service.relay, a, source.service.authorize))
	resumed := standardClient(t, restarted, "pc-a", false)
	got := batchResultUntilReady(t, resumed, r.ID)
	if got.PiResult.Outcome != "succeeded" || got.StdoutHash != stdout.Hash || got.StderrHash != f.receipt.Stderr.Hash || f.runs != 1 || len(f.pageOffsets) == 0 || f.pageOffsets[0] != 16384 {
		t.Fatal("resume reset byte offset, refs or business attempt")
	}
	store.mu.Lock()
	_ = json.Unmarshal(store.records[r.ID].Details, &d)
	store.mu.Unlock()
	if d.Batch.Progress.Attempt != attempt || d.Batch.Progress.LayoutVersion != 0 || d.Batch.Files[1].Chunks[0].Hash != prefix.Hash || d.Batch.Files[1].Chunks[0].Size != 16384 {
		t.Fatal("legacy persisted chunk was replaced")
	}
}

func TestStandardMCPPiBatchUnknownLayoutDoesNotReadOrRedispatch(t *testing.T) {
	source, store, f, a := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}, false))
	store.mu.Lock()
	record := store.records[r.ID]
	var d Details
	_ = json.Unmarshal(record.Details, &d)
	d.Batch.Progress.LayoutVersion = agenvo.BatchLayoutVersion + 1
	record.Details, _ = json.Marshal(d)
	store.records[r.ID] = record
	store.mu.Unlock()
	before := len(f.calls)
	restarted := NewSource(NewService(store, store, source.service.relay, a, source.service.authorize))
	resumed := standardClient(t, restarted, "pc-a", false)
	got := parseResult(t, invoke(t, resumed, "computer_task_result", map[string]any{"operation_id": r.ID}, true))
	if got.ID != r.ID || got.State != "submitted" || got.ProcessStopped || got.PiResult != nil || len(f.calls) != before || f.runs != 1 {
		t.Fatal("unknown layout performed remote IO or released the invocation slot")
	}
}

func TestStandardMCPPiBatchCompleteReceiptBudgetWithLongNativeIDs(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	f.hiddenFrame = true
	c := standardClient(t, source, "pc-a", false)
	r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}, false))
	// Preserve the maximum identities accepted by the earlier saved format.
	f.progress.WorkspaceID = strings.Repeat("w", 256)
	f.progress.PaneID = strings.Repeat("p", 128)
	f.progress.TerminalID = strings.Repeat("t", 256)
	f.receipt.WorkspaceID, f.receipt.PaneID, f.receipt.TerminalID = f.progress.WorkspaceID, f.progress.PaneID, f.progress.TerminalID
	raw, _ := json.Marshal(f.receipt)
	if len(raw) <= 1024 {
		t.Fatal("receipt fixture does not exceed one KiB")
	}
	store.mu.Lock()
	record := store.records[r.ID]
	var d Details
	_ = json.Unmarshal(record.Details, &d)
	d.Batch.Progress = f.progress
	record.Details, _ = json.Marshal(d)
	store.records[r.ID] = record
	store.mu.Unlock()
	got := batchResultUntilReady(t, c, r.ID)
	if got.PiResult.Outcome != "succeeded" || f.runs != 1 {
		t.Fatal("receipt recovery was truncated by a data-page budget")
	}
}
