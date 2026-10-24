package computertask

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
)

// This is a known-gap regression, not a recovery implementation. The standard
// MCP/service/relay path is real; Herdr metadata and completed Pi files use the
// existing synthetic transport. No vendor binary or Windows restart is claimed.
func TestBatchCompletedControlRestartRecoveryBoundary(t *testing.T) {
	for _, stage := range []string{"receipt_not_collected", "receipt_only", "summary_ready", "all_cached"} {
		t.Run(stage, func(t *testing.T) {
			source, store, remote, assets := setupBatch(t)
			client := standardClient(t, source, "pc-a", false)
			input := map[string]any{"kind": "herdr_pi_task", "task": "synthetic completed invocation"}
			original := parseResult(t, invoke(t, client, "computer_task_submit", input, false))
			remote.projectCompletion()
			if remote.receipt.Validate(remote.progress) != nil || !remote.receipt.Exited || remote.receipt.ExitCode != 0 || remote.receipt.Completion == nil {
				t.Fatal("fixture must have an already-exited, correlated invocation")
			}
			if parsed, err := agenvo.ParseBatchCompletion(remote.completion, remote.progress, remote.receipt); err != nil || parsed.Outcome != "succeeded" {
				t.Fatal("fixture must have a valid small semantic result before restart")
			}
			switch stage {
			case "receipt_only":
				// Model the real durable boundary immediately after the exit
				// receipt was saved, before the first result.json page.
				store.mu.Lock()
				record := store.records[original.ID]
				var details Details
				if err := json.Unmarshal(record.Details, &details); err != nil {
					t.Fatal(err)
				}
				receipt := remote.receipt
				details.Batch.Receipt = &receipt
				details.Batch.Files = []BatchTransfer{{Meta: receipt.Stdout}, {Meta: receipt.Stderr}}
				details.Phase, record.State = "pi_exited_result_pending", "completed"
				details.ExitCode = &receipt.ExitCode
				record.Details, _ = json.Marshal(details)
				store.records[original.ID] = record
				store.mu.Unlock()
			case "summary_ready", "all_cached":
				ready := batchResultUntilReady(t, client, original.ID)
				if ready.PiResult.Outcome != "succeeded" {
					t.Fatal("summary setup did not finish")
				}
				if stage == "all_cached" {
					batchLogsUntilReady(t, client, original.ID)
				}
			}
			store.mu.Lock()
			before, _ := json.Marshal(store.records[original.ID])
			store.mu.Unlock()
			filesBefore, _ := json.Marshal([]any{remote.progress, remote.receipt, remote.stdout, remote.stderr, remote.completion})
			assetsBefore, _ := json.Marshal(assets.data)
			readsBefore, callsBefore := remote.reads, len(remote.calls)
			// After Herdr restart the pane/workspace IDs can remain, while
			// the terminal incarnation changes and its old scrollback is gone.
			remote.changedPane, remote.hiddenFrame = true, true
			remote.control, remote.pending = "", ""
			restarted := NewSource(NewService(store, store, source.service.relay, assets, source.service.authorize))
			resumed := standardClient(t, restarted, "pc-a", false)
			uncollected := stage == "receipt_not_collected" || stage == "receipt_only"
			got := parseResult(t, invoke(t, resumed, "computer_task_result", map[string]any{"operation_id": original.ID}, uncollected))
			if uncollected {
				if got.PiResult != nil {
					t.Fatal("uncollected result was invented")
				}
				t.Log("KNOWN GAP: unchanged valid receipt/result files remain unreadable after control terminal replacement")
			} else {
				if got.ID != original.ID || got.PiResult == nil || got.PiResult.Outcome != "succeeded" || got.Output != "42 中文 ✓" || len(remote.calls) != callsBefore {
					t.Fatal("durable summary should survive without contacting the replacement terminal")
				}
				invoke(t, resumed, "computer_task_logs", map[string]any{"operation_id": original.ID}, stage != "all_cached")
				if stage == "summary_ready" {
					t.Log("KNOWN GAP: cached summary survives; uncollected diagnostics remain unavailable")
				} else if len(remote.calls) != callsBefore {
					t.Fatal("already cached diagnostics contacted the restarted peer")
				}
			}
			if remote.runs != 1 || remote.reads != readsBefore {
				t.Fatal("recovery re-dispatched Pi or typed into a replacement terminal")
			}
			store.mu.Lock()
			after, _ := json.Marshal(store.records[original.ID])
			store.mu.Unlock()
			filesAfter, _ := json.Marshal([]any{remote.progress, remote.receipt, remote.stdout, remote.stderr, remote.completion})
			assetsAfter, _ := json.Marshal(assets.data)
			if !bytes.Equal(before, after) || !bytes.Equal(filesBefore, filesAfter) || !bytes.Equal(assetsBefore, assetsAfter) {
				t.Fatal("observation changed original identity, receipt, files, offsets or stored references")
			}
		})
	}
}
