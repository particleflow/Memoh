package computertask

import (
	"encoding/json"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

// TestBatchTargetChangeWithProvenNoInvocationDispatchesFirst reproduces
// incident E3's stranded conversation: the latest prior Pi operation completed
// with a valid never-started receipt, then the owner changed the target profile
// (the explicit agent PATH fix). The next submission must dispatch as a first
// invocation instead of stranding the conversation at pi_continuity_unconfirmed.
func TestBatchTargetChangeWithProvenNoInvocationDispatchesFirst(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	first := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic first"}, false))
	f.receipt.Started = false
	f.receipt.ExitCode = 125
	f.receipt.Preflight = "preflight_pi_missing"
	f.receipt.History = &agenvo.BatchHistoryReport{Before: "not_checked", After: "not_started"}
	f.receipt.Completion = nil
	notStarted := batchResultUntilReady(t, c, first.ID)
	if notStarted.PiResult == nil || notStarted.PiResult.Outcome != "not_started" || notStarted.PiResult.Preflight != "preflight_pi_missing" {
		t.Fatalf("proven no-invocation receipt not surfaced: %+v", notStarted.PiResult)
	}
	// The owner fixes the launcher PATH; the whole-profile target fingerprint
	// changes even though the Pi session never saw an invocation.
	prior := store.records[first.ID]
	prior.TargetFingerprint = "changed-by-agent-path"
	store.records[first.ID] = prior

	// Reconstruct the service from the same durable records, with a new turn.
	f.runs = 0
	service := NewService(store, store, source.service.relay, nil, source.service.authorize)
	binding := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "run-b"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.submit(ctx, binding, Submit{Kind: "herdr_pi_task", Task: "synthetic followup"})
	if err != nil {
		t.Fatal(err)
	}
	if next.State == "not_started" && next.Phase == "pi_continuity_unconfirmed" {
		t.Fatalf("conversation stranded after target change: %+v", next)
	}
	if f.runs != 1 {
		t.Fatalf("expected one dispatch, got %d", f.runs)
	}
	var d Details
	if err := json.Unmarshal(store.records[next.ID].Details, &d); err != nil || d.Batch == nil {
		t.Fatalf("missing batch details: %v", err)
	}
	if d.Batch.Progress.History == nil || d.Batch.Progress.History.Mode != "first" {
		t.Fatalf("proven no-invocation prior must dispatch as first, got %+v", d.Batch.Progress.History)
	}
}

// TestBatchAdmissionBlockedPriorDoesNotStrandNewRequests covers the production
// sequence after the agent PATH config fix: one request was rejected at the
// continuity gate (no receipt, no native mutation, state not_started — the
// persisted shape of operation 31e8c398), and the user's next real request on
// the same default work_dir must still dispatch.
func TestBatchAdmissionBlockedPriorDoesNotStrandNewRequests(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	first := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic first"}, false))
	batchResultUntilReady(t, c, first.ID)
	prior := store.records[first.ID]
	var d Details
	if err := json.Unmarshal(prior.Details, &d); err != nil {
		t.Fatal(err)
	}
	// Rewrite the latest prior into the admission-blocked shape: matching
	// fingerprint, no receipt, nothing ever dispatched.
	d.Phase = "pi_continuity_unconfirmed"
	d.Batch.Receipt = nil
	d.Batch.Result = nil
	d.Batch.Files = nil
	d.Batch.Progress.Phase = ""
	d.Batch.Progress.WorkspaceID, d.Batch.Progress.PaneID, d.Batch.Progress.RequestID = "", "", ""
	prior.Details, _ = json.Marshal(d)
	prior.State = "not_started"
	store.records[first.ID] = prior
	f.runs = 0
	service := NewService(store, store, source.service.relay, nil, source.service.authorize)
	binding := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "run-b"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.submit(ctx, binding, Submit{Kind: "herdr_pi_task", Task: "synthetic followup"})
	if err != nil {
		t.Fatal(err)
	}
	if next.State == "not_started" && next.Phase == "pi_continuity_unconfirmed" {
		t.Fatalf("admission-blocked prior stranded a new request: %+v", next)
	}
	if f.runs != 1 {
		t.Fatalf("expected one dispatch, got %d", f.runs)
	}
	var d2 Details
	if err := json.Unmarshal(store.records[next.ID].Details, &d2); err != nil || d2.Batch == nil || d2.Batch.Progress.History == nil || d2.Batch.Progress.History.Mode != "first" {
		t.Fatalf("admission-blocked prior must dispatch as first: %+v %v", d2.Batch, err)
	}
}

// TestResumeCompletedNotStartedReportsUnresumable pins the incident E3 resume
// no-op: a completed operation whose Pi never started is not re-executed, and
// the receipt says so explicitly instead of reading like a retry.
func TestResumeCompletedNotStartedReportsUnresumable(t *testing.T) {
	source, _, f, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	first := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic first"}, false))
	f.receipt.Started = false
	f.receipt.ExitCode = 125
	f.receipt.Preflight = "preflight_pi_missing"
	f.receipt.History = &agenvo.BatchHistoryReport{Before: "not_checked", After: "not_started"}
	f.receipt.Completion = nil
	batchResultUntilReady(t, c, first.ID)
	f.runs = 0
	resumed := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic first", "resume_operation_id": first.ID}, false))
	if resumed.ID != first.ID || resumed.Resumable == nil || *resumed.Resumable || resumed.ResumeReason != "completed_pi_not_started" {
		t.Fatalf("resume of a completed never-started batch not annotated: %+v", resumed)
	}
	if resumed.PiResult == nil || resumed.PiResult.Preflight != "preflight_pi_missing" {
		t.Fatalf("resume receipt lost the preflight cause: %+v", resumed.PiResult)
	}
	if f.runs != 0 {
		t.Fatal("no-op resume re-dispatched Pi")
	}
}
