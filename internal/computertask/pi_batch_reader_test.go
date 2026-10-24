package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

// Real coordinator/relay/CAS, synthetic official-method responses. The existing
// bounded-file fixture still serves the original invocation's immutable files.
type recoveryTransport struct {
	mu                       sync.Mutex
	base                     *batchTransport
	id                       string
	creates, readerRuns      int
	lost, malformed, changed bool
	timeout, wrongWorkspace  bool
	onCreate, beforeRead     func()
}

func (f *recoveryTransport) record() (dbstore.ComputerOperationRecord, Details) {
	f.base.store.mu.Lock()
	defer f.base.store.mu.Unlock()
	r := f.base.store.records[f.id]
	var d Details
	if err := json.Unmarshal(r.Details, &d); err != nil {
		f.base.t.Fatal(err)
	}
	return r, d
}

func (f *recoveryTransport) call(ctx context.Context, owner, connection, tool string, args map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	code, _ := args["code"].(string)
	var parts []json.RawMessage
	if err := json.Unmarshal([]byte("["+strings.TrimSuffix(strings.TrimPrefix(code, "return await call("), ");")+"]"), &parts); err != nil || len(parts) != 3 {
		f.base.t.Fatal("invalid relay call")
	}
	var method string
	var params map[string]any
	_ = json.Unmarshal(parts[1], &method)
	_ = json.Unmarshal(parts[2], &params)
	if method == "workspace.create" {
		_, d := f.record()
		reader := d.Batch.RecoveryReader
		if reader == nil || reader.Phase != "creating" || reader.RequestID == "" || params["label"] != "memoh-reader-"+reader.RequestID || params["cwd"] != `C:\Synthetic` {
			f.base.t.Fatal("reader creation preceded durable reservation or wrong root")
		}
		f.creates++
		if f.onCreate != nil {
			f.onCreate()
		}
		if f.lost {
			return nil, errors.New("synthetic lost creation response")
		}
		if f.timeout {
			return nil, context.DeadlineExceeded
		}
		workspace := "w-reader"
		if f.malformed {
			workspace = d.Batch.Progress.WorkspaceID
		}
		return envelope(map[string]any{"session": "synthetic", "result": map[string]any{
			"workspace": map[string]string{"workspace_id": workspace, "label": params["label"].(string)},
			"root_pane": map[string]string{"workspace_id": workspace, "pane_id": "reader", "terminal_id": "reader-terminal"},
		}}), nil
	}
	if method == "pane.get" && params["paneId"] == "control" && f.wrongWorkspace {
		return envelope(map[string]any{"session": "synthetic", "result": map[string]any{"type": "pane_info", "pane": map[string]string{"workspace_id": "another-workspace", "pane_id": "control", "terminal_id": "changed"}}}), nil
	}
	if params["paneId"] == "reader" {
		_, d := f.record()
		r := d.Batch.RecoveryReader
		if r == nil || r.Phase != "ready" || r.PaneID != "reader" || r.TerminalID != "reader-terminal" || r.WorkspaceID != "w-reader" {
			f.base.t.Fatal("reader accessed before durable ownership")
		}
		if method == "pane.get" {
			terminal := "reader-terminal"
			if f.changed {
				terminal = "another-reader-terminal"
			}
			if f.beforeRead != nil {
				f.beforeRead()
			}
			return envelope(map[string]any{"session": "synthetic", "result": map[string]any{"type": "pane_info", "pane": map[string]string{"workspace_id": "w-reader", "pane_id": "reader", "terminal_id": terminal}}}), nil
		}
		if method == "pane.run" {
			f.readerRuns++
		}
		// Reuse the existing schema, nonce, rendered-row and paging fixture.
		// Only the synthetic transport aliases this new shell, never product
		// Progress/receipt identities or persisted offsets.
		params["paneId"] = "control"
		parts[2], _ = json.Marshal(params)
		args = map[string]any{"code": "return await call(" + string(parts[0]) + "," + string(parts[1]) + "," + string(parts[2]) + ");"}
	}
	return f.base.call(ctx, owner, connection, tool, args)
}

func installRecoveryTransport(source *Source, remote *batchTransport, id string) *recoveryTransport {
	f := &recoveryTransport{base: remote, id: id}
	source.service.relay = agenvo.NewService(remote.store, permissionChecker{}, f.call)
	return f
}

func batchReaderFixture(t *testing.T) (*Source, *fixtureStore, *recoveryTransport, Result) {
	t.Helper()
	source, store, remote, _ := setupBatch(t)
	client := standardClient(t, source, "pc-a", false)
	original := parseResult(t, invoke(t, client, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic recovery"}, false))
	remote.changedPane, remote.hiddenFrame = true, true
	return source, store, installRecoveryTransport(source, remote, original.ID), original
}

func TestBatchReaderUnknownCreationNeverRecreates(t *testing.T) {
	for _, mode := range []string{"lost", "timeout", "malformed", "reservation_save", "ready_save", "revoked_after_create", "left_creating"} {
		t.Run(mode, func(t *testing.T) {
			source, store, remote, original := batchReaderFixture(t)
			before, _ := remote.record()
			switch mode {
			case "lost":
				remote.lost = true
			case "timeout":
				remote.timeout = true
			case "malformed":
				remote.malformed = true
			case "reservation_save":
				store.failRevision = &before.Revision
			case "ready_save":
				revision := before.Revision + 1
				store.failRevision = &revision
			case "revoked_after_create":
				remote.onCreate = func() { store.revoked = true }
			case "left_creating":
				_, d := remote.record()
				d.Batch.RecoveryReader = &agenvo.BatchRecoveryReader{RequestID: "11111111-1111-4111-8111-111111111111", Phase: "creating"}
				before.Details, _ = json.Marshal(d)
				if _, err := store.SaveComputerOperation(t.Context(), before); err != nil {
					t.Fatal(err)
				}
			}
			client := standardClient(t, source, "pc-a", false)
			invoke(t, client, "computer_task_result", map[string]any{"operation_id": original.ID}, true)
			// A new coordinator has no in-memory lease to justify retrying.
			restarted := NewSource(NewService(store, store, source.service.relay, source.service.assets, source.service.authorize))
			client = standardClient(t, restarted, "pc-a", false)
			invoke(t, client, "computer_task_result", map[string]any{"operation_id": original.ID}, true)
			want := 1
			if mode == "reservation_save" || mode == "left_creating" {
				want = 0
			}
			if remote.creates != want || remote.readerRuns != 0 || remote.base.runs != 1 {
				t.Fatalf("creates=%d reads=%d Pi=%d", remote.creates, remote.readerRuns, remote.base.runs)
			}
			_, d := remote.record()
			if d.Batch.Result != nil || mode != "reservation_save" && d.Batch.RecoveryReader.Phase != "creating" {
				t.Fatal("unknown creation became ready/completed")
			}
		})
	}
}

func TestBatchReaderOnlyCASWinnerCreates(t *testing.T) {
	source, store, remote, original := batchReaderFixture(t)
	reserved, release := make(chan struct{}), make(chan struct{})
	remote.onCreate = func() { close(reserved); <-release }
	binding := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "run-a"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	r, d, err := source.service.load(ctx, binding, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := source.service.observe(ctx, binding, original.ID, true); finished <- err }()
	<-reserved
	// Simulate a contender that loaded the same old revision before the winner.
	// Its CAS fails, so it cannot create even though its read sees the same
	// replaced control pane. The transport create is released after this test
	// of the store CAS to avoid serializing the contender on the fixture lock.
	d.Batch.RecoveryReader = &agenvo.BatchRecoveryReader{RequestID: "22222222-2222-4222-8222-222222222222", Phase: "creating"}
	r.Details, _ = json.Marshal(d)
	if _, err := store.SaveComputerOperation(ctx, r); err == nil {
		t.Fatal("stale reservation overwrote CAS winner")
	}
	if _, err := source.service.observe(ctx, binding, original.ID, true); !errors.Is(err, agenvo.ErrUnknown) {
		t.Fatalf("concurrent collector did not stop on creating: %v", err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if remote.creates != 1 || remote.base.runs != 1 {
		t.Fatal("concurrent collector created or submitted twice")
	}
}

func TestBatchReaderReadOnlyGetDoesNotCreate(t *testing.T) {
	source, _, remote, original := batchReaderFixture(t)
	client := standardClient(t, source, "pc-a", false)
	invoke(t, client, "computer_task_get", map[string]any{"operation_id": original.ID}, false)
	if remote.creates != 0 || remote.readerRuns != 0 {
		t.Fatal("status created a resource")
	}
}

func TestBatchReaderDoesNotTreatOtherIdentityErrorsAsRestart(t *testing.T) {
	source, _, remote, original := batchReaderFixture(t)
	remote.wrongWorkspace = true
	client := standardClient(t, source, "pc-a", false)
	invoke(t, client, "computer_task_result", map[string]any{"operation_id": original.ID}, true)
	if remote.creates != 0 || remote.readerRuns != 0 {
		t.Fatal("unrelated identity mismatch created a reader")
	}
}

func TestBatchReaderChangedIdentityAndRevocationPreserveProgress(t *testing.T) {
	for _, mode := range []string{"identity", "revoked", "revoked_after_get"} {
		t.Run(mode, func(t *testing.T) {
			source, store, remote, original := batchReaderFixture(t)
			client := standardClient(t, source, "pc-a", false)
			batchResultUntilReady(t, client, original.ID)
			invoke(t, client, "computer_task_logs", map[string]any{"operation_id": original.ID}, false)
			_, partial := remote.record()
			if partial.Batch.Files[1].Offset == 0 || partial.Batch.Files[1].Offset >= partial.Batch.Files[1].Meta.Size {
				t.Fatal("fixture needs a persisted partial diagnostic offset")
			}
			before, _ := remote.record()
			reads := remote.readerRuns
			switch mode {
			case "identity":
				remote.changed = true
			case "revoked":
				store.revoked = true
			case "revoked_after_get":
				remote.beforeRead = func() { store.revoked = true }
			}
			invoke(t, client, "computer_task_logs", map[string]any{"operation_id": original.ID}, true)
			after, _ := remote.record()
			if after.Revision != before.Revision || string(after.Details) != string(before.Details) || remote.readerRuns != reads || remote.creates != 1 || remote.base.runs != 1 {
				t.Fatal("denied read changed offsets, created again or typed after identity/grant change")
			}
		})
	}
}

func TestBatchReaderReceiptAndHashValidationRemainOriginal(t *testing.T) {
	for _, mode := range []string{"receipt_attempt", "completion_hash", "diagnostic_hash"} {
		t.Run(mode, func(t *testing.T) {
			source, _, remote, original := batchReaderFixture(t)
			client := standardClient(t, source, "pc-a", false)
			switch mode {
			case "receipt_attempt":
				remote.base.receipt.Attempt = strings.Repeat("a", 32)
			case "completion_hash":
				// Collect the valid receipt first, then corrupt only read data.
				remote.beforeRead = func() {
					_, d := remote.record()
					remote.base.corrupt = d.Batch.Receipt != nil
				}
			case "diagnostic_hash":
				batchResultUntilReady(t, client, original.ID)
				remote.base.corrupt = true
			}
			tool := "computer_task_result"
			if mode == "diagnostic_hash" {
				tool = "computer_task_logs"
			}
			invoke(t, client, tool, map[string]any{"operation_id": original.ID}, true)
			_, saved := remote.record()
			if mode == "diagnostic_hash" {
				if saved.Batch.Result == nil || saved.Batch.Result.Outcome != "succeeded" || saved.Batch.Files[0].ReadyHash != "" {
					t.Fatal("bad diagnostics altered semantic result or produced a ready reference")
				}
			} else if saved.Batch.Result != nil {
				t.Fatal("foreign/corrupt result accepted")
			}
			if mode == "receipt_attempt" && saved.Batch.Receipt != nil {
				t.Fatal("foreign receipt persisted")
			}
			if remote.creates != 1 || remote.base.runs != 1 {
				t.Fatal("hash error caused a new resource or business dispatch")
			}
		})
	}
}

func TestBatchReaderPageRereadDoesNotDuplicateCommittedChunks(t *testing.T) {
	source, store, remote, original := batchReaderFixture(t)
	client := standardClient(t, source, "pc-a", false)
	batchResultUntilReady(t, client, original.ID)
	before, _ := remote.record()
	store.failRevision = &before.Revision
	invoke(t, client, "computer_task_logs", map[string]any{"operation_id": original.ID}, true)
	_, failed := remote.record()
	if failed.Batch.Files[0].Offset != 0 || len(failed.Batch.Files[0].Chunks) != 0 {
		t.Fatal("failed CAS advanced a file")
	}
	store.failRevision = nil
	restarted := NewSource(NewService(store, store, source.service.relay, source.service.assets, source.service.authorize))
	client = standardClient(t, restarted, "pc-a", false)
	ready := batchLogsUntilReady(t, client, original.ID)
	if ready.StdoutHash != remote.base.receipt.Stdout.Hash || ready.StderrHash != remote.base.receipt.Stderr.Hash {
		t.Fatal("reread did not finish original file hashes")
	}
	_, saved := remote.record()
	for _, file := range saved.Batch.Files {
		var total int64
		for _, chunk := range file.Chunks {
			total += chunk.Size
		}
		if total != file.Meta.Size || file.Offset != file.Meta.Size {
			t.Fatal("reread appended duplicate bytes")
		}
	}
	reads := remote.readerRuns
	batchLogsUntilReady(t, client, original.ID)
	if remote.readerRuns != reads || remote.creates != 1 || remote.base.runs != 1 {
		t.Fatal("cached reread dispatched more work")
	}
}
