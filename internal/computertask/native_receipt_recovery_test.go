package computertask

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
)

// The terminal is a presentation channel. The immutable receipt must still
// match the original operation, grant, target and invocation in every case.
func TestNativeReceiptRecoveryUsesOriginalFileAfterMalformedTerminalFrame(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	first, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "synthetic receipt recovery"})
	if err != nil {
		t.Fatal(err)
	}
	service.relay.SetNativeCaller(func(ctx context.Context, scope agenvo.Scope, target agenvo.Target, method string, params map[string]any) (agenvo.Receipt, error) {
		r, err := f.invoke(ctx, scope, target, method, params)
		if err == nil && method == "pane.read" && params["paneId"] == f.batch.progress.PaneID {
			r.Result, _ = json.Marshal(map[string]string{"session": target.Session, "output": "MEMOH_BATCH_" + f.batch.progress.Generation + ":invalid-base64!:END_BATCH_" + f.batch.progress.Generation + "\r\n"})
		}
		return r, err
	})
	original := f.store.records[first.ID]
	var got Result
	for range 5 {
		got, err = service.observe(ctx, f.binding, first.ID, true)
		if err != nil {
			t.Fatalf("valid immutable receipt was not checked: state=%s control_reads=%d: %v", got.State, f.batch.reads, err)
		}
		if got.PiResult != nil {
			break
		}
	}
	saved := f.store.records[first.ID]
	if got.State != "completed" || !got.ProcessStopped || got.PiResult == nil || got.PiResult.Outcome != "succeeded" || f.batch.reads == 0 || f.batch.runs != 1 || saved.RunID != original.RunID || saved.TargetID != original.TargetID || saved.TargetFingerprint != original.TargetFingerprint {
		t.Fatalf("file recovery fabricated completion or replaced the original identity: %+v", got)
	}
}

func TestNativeReceiptRecoveryDoesNotTrustMalformedTerminalOrForeignFile(t *testing.T) {
	for _, scenario := range []string{"missing-file", "corrupt-file", "other-attempt", "other-generation", "other-request", "other-session", "other-workspace", "other-pane", "other-terminal", "revoked", "revoked-during-file-read"} {
		t.Run(scenario, func(t *testing.T) {
			f, service, ctx := newNativeTaskFixture(t)
			first, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "synthetic denied receipt recovery"})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing-file":
				f.batch.missing = true
			case "corrupt-file":
				f.batch.corrupt = true
			case "other-attempt":
				f.batch.receipt.Attempt = strings.Repeat("a", 32)
			case "other-generation":
				f.batch.receipt.Generation = strings.Repeat("a", 32)
			case "other-request":
				f.batch.receipt.RequestHash = strings.Repeat("a", 64)
			case "other-session":
				f.batch.receipt.PiSessionID = strings.Repeat("a", 64)
			case "other-workspace":
				f.batch.receipt.WorkspaceID = "foreign-workspace"
			case "other-pane":
				f.batch.receipt.PaneID = "foreign-pane"
			case "other-terminal":
				f.batch.receipt.TerminalID = "foreign-terminal"
			case "revoked":
				f.denied = true
			case "revoked-during-file-read":
				f.batch.onPage = func() { f.denied = true }
			}
			service.relay.SetNativeCaller(func(ctx context.Context, scope agenvo.Scope, target agenvo.Target, method string, params map[string]any) (agenvo.Receipt, error) {
				r, err := f.invoke(ctx, scope, target, method, params)
				if err == nil && method == "pane.read" && params["paneId"] == f.batch.progress.PaneID {
					r.Result, _ = json.Marshal(map[string]string{"session": target.Session, "output": "MEMOH_BATCH_" + f.batch.progress.Generation + ":invalid-base64!:END_BATCH_" + f.batch.progress.Generation + "\r\n"})
				}
				return r, err
			})
			before := f.store.records[first.ID]
			got, err := service.observe(ctx, f.binding, first.ID, true)
			after := f.store.records[first.ID]
			if err == nil || got.ProcessStopped || got.PiResult != nil || f.batch.runs != 1 || after.Revision != before.Revision || string(after.Details) != string(before.Details) {
				t.Fatalf("unverified receipt released the original operation: %+v %v", got, err)
			}
		})
	}
}
