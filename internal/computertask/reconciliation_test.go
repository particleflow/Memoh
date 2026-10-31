package computertask

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestRecoveryReconcilesPersistedBatchWithoutGrantingStop(t *testing.T) {
	for _, stage := range []string{"receipt_missing", "receipt_only", "result_ready", "logs_recorded"} {
		t.Run(stage, func(t *testing.T) {
			source, store, remote, assets := setupBatch(t)
			client := standardClient(t, source, "pc-a", false)
			original := parseResult(t, invoke(t, client, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic reconciliation"}, false))
			remote.projectCompletion()
			switch stage {
			case "receipt_only":
				r := store.records[original.ID]
				var d Details
				_ = json.Unmarshal(r.Details, &d)
				receipt := remote.receipt
				d.Batch.Receipt = &receipt
				d.Batch.Files = []BatchTransfer{{Meta: receipt.Stdout}, {Meta: receipt.Stderr}}
				d.Phase, r.State = "pi_exited_result_pending", "completed"
				r.Details, _ = json.Marshal(d)
				store.records[r.ID] = r
			case "result_ready", "logs_recorded":
				batchResultUntilReady(t, client, original.ID)
				if stage == "logs_recorded" {
					batchLogsUntilReady(t, client, original.ID)
				}
			}
			r := store.records[original.ID]
			r.Revision = 1<<53 + 1 // Cannot round through a JavaScript number.
			store.records[r.ID] = r
			var d Details
			_ = json.Unmarshal(r.Details, &d)
			transport := &observationTransport{t: t, refs: d.Batch.Progress, fault: "mismatch"}
			source.service.relay = agenvo.NewService(store, permissionChecker{}, transport.call)
			before := observationSnapshot(t, store)
			assetsBefore, _ := json.Marshal(assets.data)
			raw := invoke(t, client, "computer_task_recover", map[string]any{"operation_id": r.ID}, false)
			var wire struct {
				Computer Recovery `json:"computer"`
			}
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			got := wire.Computer
			evidence := got.Reconciliation
			p := d.Batch.Progress
			if evidence == nil || evidence.Revision != "9007199254740993" || evidence.TargetFingerprint != r.TargetFingerprint ||
				got.OperationID != r.ID || got.Attempt != p.Attempt || got.PiSessionID != p.PiSessionID ||
				evidence.Generation != p.Generation || evidence.RequestHash != p.RequestHash ||
				got.WorkspaceID != p.WorkspaceID || got.PaneID != p.PaneID || evidence.TerminalID != p.TerminalID ||
				evidence.ControlPaneID != p.ControlPane || evidence.ControlTerminalID != p.ControlTerminal {
				t.Fatalf("lost owner identity: %+v", got)
			}
			if got.Native.Match == "observed" || got.BusinessOutcome != "unknown" {
				t.Fatal("replaced terminal or persisted result became business acceptance")
			}
			if stage == "receipt_missing" {
				if evidence.ReceiptState != "missing" || evidence.Receipt != nil || len(evidence.Files) != 0 || got.ProcessState != "unknown" {
					t.Fatal("local exit/native snapshot manufactured owner receipt")
				}
			} else {
				if evidence.ReceiptState != "validated" || evidence.Receipt == nil || evidence.Receipt.Validate(p) != nil || len(evidence.Files) != 2 {
					t.Fatal("verified receipt evidence lost")
				}
				if stage == "receipt_only" && (got.PiResult != "unknown" || got.ResultSummary != nil) {
					t.Fatal("completed operation became collected semantic result")
				}
				for _, f := range evidence.Files {
					if f.CurrentAvailability != "not_rechecked" {
						t.Fatal("historical media evidence became a live file check")
					}
					if stage == "logs_recorded" && f.Meta.Size > 0 {
						if f.State != "recorded" || f.RecordedContentHash != f.Meta.Hash || f.Offset != f.Meta.Size {
							t.Fatal("collected log checkpoint lost")
						}
					} else if f.Meta.Size > 0 && (f.State != "not_collected" || f.RecordedContentHash != "" || f.Offset != 0) {
						t.Fatal("summary availability implied full log collection")
					}
				}
			}
			assetsAfter, _ := json.Marshal(assets.data)
			if !bytes.Equal(before, observationSnapshot(t, store)) || !bytes.Equal(assetsBefore, assetsAfter) || remote.runs != 1 {
				t.Fatal("read-only reconciliation mutated or replayed the task")
			}
			for _, key := range []string{"stop_authorized", "safe_to_stop", "admission_paused"} {
				if strings.Contains(string(raw), `"`+key+`"`) {
					t.Fatal("read projection invented stop or admission authority")
				}
			}
		})
	}
}

func TestRecoveryRejectsConcurrentRevisionChange(t *testing.T) {
	source, store, transport, id := recoveryFixture(t)
	transport.after = func() {
		store.mu.Lock()
		defer store.mu.Unlock()
		r := store.records[id]
		r.Revision++
		store.records[id] = r
	}
	binding := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "run-a"}
	got, err := source.service.Recover(t.Context(), binding, id)
	if !errors.Is(err, agenvo.ErrUnknown) || got.OperationID != "" || got.Reconciliation != nil {
		t.Fatal("stale revision was returned as current reconciliation")
	}
}

func TestReconciliationRejectsMismatchedOrDuplicateFileCheckpoints(t *testing.T) {
	for _, fault := range []string{"receipt", "file_hash", "offset", "ready_hash", "duplicate", "partial", "empty"} {
		t.Run(fault, func(t *testing.T) {
			_, store, _, id := recoveryFixture(t)
			r := store.records[id]
			var d Details
			_ = json.Unmarshal(r.Details, &d)
			file := BatchTransfer{Meta: d.Batch.Receipt.Stdout, Offset: d.Batch.Receipt.Stdout.Size, ReadyHash: d.Batch.Receipt.Stdout.Hash}
			d.Batch.Files = []BatchTransfer{file}
			switch fault {
			case "receipt":
				d.Batch.Receipt.Generation = strings.Repeat("f", 32)
			case "file_hash":
				d.Batch.Files[0].Meta.Hash = strings.Repeat("e", 64)
			case "offset":
				d.Batch.Files[0].Offset++
			case "ready_hash":
				d.Batch.Files[0].ReadyHash = strings.Repeat("e", 64)
			case "duplicate":
				d.Batch.Files = append(d.Batch.Files, file)
			case "partial":
				d.Batch.Files[0].Offset, d.Batch.Files[0].ReadyHash = 1, ""
			case "empty":
				d.Batch.Receipt.Stdout = batchFile("stdout.jsonl", nil)
				d.Batch.Files = []BatchTransfer{{Meta: d.Batch.Receipt.Stdout}}
			}
			got := operationReconciliation(r, d)
			if fault == "receipt" {
				if got.ReceiptState != "invalid" || got.Receipt != nil || len(got.Files) != 0 {
					t.Fatal("foreign receipt projected")
				}
				return
			}
			want := "invalid"
			if fault == "partial" {
				want = "collecting"
			}
			if fault == "empty" {
				want = "empty"
			}
			if len(got.Files) != 2 || got.Files[0].State != want || got.Files[0].RecordedContentHash != "" {
				t.Fatalf("file checkpoint=%+v want=%s", got.Files, want)
			}
		})
	}
}
