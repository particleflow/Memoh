package computertask

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
)

// The standard MCP/service/relay path is real; Herdr metadata and completed
// Pi files use a synthetic transport. The prior ed4a checkpoint records the
// failing boundary. No vendor binary or Windows restart is claimed here.
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
			var beforeDetails Details
			if err := json.Unmarshal(store.records[original.ID].Details, &beforeDetails); err != nil {
				t.Fatal(err)
			}
			store.mu.Unlock()
			filesBefore, _ := json.Marshal([]any{remote.progress, remote.receipt, remote.stdout, remote.stderr, remote.completion})
			assetsBefore, _ := json.Marshal(assets.data)
			readsBefore, callsBefore := remote.reads, len(remote.calls)
			// After Herdr restart the pane/workspace IDs can remain, while
			// the terminal incarnation changes and its old scrollback is gone.
			remote.changedPane, remote.hiddenFrame = true, true
			remote.control, remote.pending = "", ""
			reader := installRecoveryTransport(source, remote, original.ID)
			restarted := NewSource(NewService(store, store, source.service.relay, assets, source.service.authorize))
			resumed := standardClient(t, restarted, "pc-a", false)
			got := parseResult(t, invoke(t, resumed, "computer_task_result", map[string]any{"operation_id": original.ID}, false))
			if got.ID != original.ID || got.PiResult == nil || got.PiResult.Outcome != "succeeded" || got.Output != "42 中文 ✓" {
				t.Fatal("same completed invocation was not recovered")
			}
			if (stage == "summary_ready" || stage == "all_cached") && len(remote.calls) != callsBefore {
				t.Fatal("cached summary contacted Herdr")
			}
			logs := batchLogsUntilReady(t, resumed, original.ID)
			if logs.StdoutHash != remote.receipt.Stdout.Hash || logs.StderrHash != remote.receipt.Stderr.Hash {
				t.Fatal("diagnostics were not recovered to the original hashes")
			}
			if stage == "all_cached" {
				if len(remote.calls) != callsBefore || reader.creates != 0 || remote.reads != readsBefore {
					t.Fatal("already cached diagnostics contacted the restarted peer")
				}
			} else if reader.creates != 1 || remote.reads-readsBefore != reader.readerRuns {
				t.Fatal("reader not created once or original replacement pane received input")
			}
			if remote.runs != 1 {
				t.Fatal("recovery dispatched Pi again")
			}
			store.mu.Lock()
			after, _ := json.Marshal(store.records[original.ID])
			store.mu.Unlock()
			filesAfter, _ := json.Marshal([]any{remote.progress, remote.receipt, remote.stdout, remote.stderr, remote.completion})
			assetsAfter, _ := json.Marshal(assets.data)
			if !bytes.Equal(filesBefore, filesAfter) {
				t.Fatal("recovery changed original execution identity, receipt or source files")
			}
			if stage == "all_cached" && (!bytes.Equal(before, after) || !bytes.Equal(assetsBefore, assetsAfter)) {
				t.Fatal("already cached results changed on reread")
			}
			_, saved := reader.record()
			progress, _ := json.Marshal(saved.Batch.Progress)
			originalProgress, _ := json.Marshal(beforeDetails.Batch.Progress)
			if !bytes.Equal(progress, originalProgress) || saved.Batch.Receipt.Validate(saved.Batch.Progress) != nil {
				t.Fatal("reader replaced original persisted execution identity")
			}
		})
	}
}
