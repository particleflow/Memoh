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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/media"
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
	calls                                            []string
	progress                                         agenvo.BatchProgress
	receipt                                          agenvo.BatchReceipt
	stdout, stderr                                   []byte
	control                                          string
	missing, hiddenFrame, corrupt, changedPane, lost bool
	reason                                           string
	exit                                             int
	onPage                                           func()
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
			if count > agenvo.BatchPageBytes || offset > len(data) {
				f.t.Fatal("page exceeds bounds")
			}
			data = append([]byte(nil), data[offset:min(offset+count, len(data))]...)
			if f.corrupt && len(data) > 0 {
				data[0] ^= 1
			}
			f.control = "\r\nMEMOH_FILE_" + m[1] + ":" + base64.StdEncoding.EncodeToString(data) + ":END_FILE_" + m[1] + "\r\n"
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
		body = map[string]any{"type": "pane_info", "pane": map[string]string{"pane_id": "control", "terminal_id": terminal, "workspace_id": "w-test"}}
	case "pane.read":
		text := f.control
		if params["paneId"] == "job" {
			text = "prompt without receipt"
			if !f.missing && !f.hiddenFrame {
				raw, _ := json.Marshal(f.receipt)
				text = "echo is not a receipt\r\nMEMOH_BATCH_" + f.progress.Generation + ":" + base64.StdEncoding.EncodeToString(raw) + ":END_BATCH_" + f.progress.Generation + "\r\n"
			}
		}
		return envelope(map[string]any{"session": "synthetic", "output": text}), nil
	default:
		f.t.Fatalf("unexpected native operation: %s", method)
	}
	return envelope(map[string]any{"session": "synthetic", "result": body}), nil
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
	f := &batchTransport{t: t, store: store, reason: "stop"}
	a := &batchAssets{data: map[string][]byte{}}
	source.service.relay = agenvo.NewService(store, permissionChecker{}, f.call)
	source.service.assets = a
	return source, store, f, a
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
	got := parseResult(t, invoke(t, c, "computer_task_result", map[string]any{"operation_id": r.ID}, false))
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
