package computertask

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/computertarget"
	dbstore "github.com/felinics/memoh/internal/db/store"
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
	f.receipt.History = &computertarget.BatchHistoryReport{Before: "not_checked", After: "not_started"}
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

// seedBlockedOperation appends the admission-blocked shape of incident
// operation 31e8c398 above an existing record of the same Pi session: matching
// identity and fingerprint, no receipt, no native mutation, state not_started.
func seedBlockedOperation(t *testing.T, store *fixtureStore, below dbstore.ComputerOperationRecord) {
	t.Helper()
	var d Details
	if err := json.Unmarshal(below.Details, &d); err != nil || d.Batch == nil {
		t.Fatalf("seed source unreadable: %v", err)
	}
	d.Phase = "pi_continuity_unconfirmed"
	d.Batch.Receipt = nil
	d.Batch.Result = nil
	d.Batch.Files = nil
	d.InputPreparation = ""
	d.Batch.Progress.Phase = ""
	d.Batch.Progress.WorkspaceID, d.Batch.Progress.PaneID, d.Batch.Progress.RequestID = "", "", ""
	blocked := below
	blocked.ID = "blocked-prior-" + below.ID[:8]
	blocked.State = "not_started"
	blocked.CreatedAt = below.CreatedAt.Add(time.Minute)
	blocked.UpdatedAt = blocked.CreatedAt
	blocked.Details, _ = json.Marshal(d)
	store.records[blocked.ID] = blocked
}

// TestBatchAdmissionBlockedPriorDoesNotStrandNewRequests covers the production
// sequence after the agent PATH config fix over a Pi session that provably
// never started (the incident conversation's own history shape): an earlier
// never-started receipt, then one request rejected at the continuity gate (no
// receipt, no native mutation, state not_started — the persisted shape of
// operation 31e8c398), then the user's next real request on the same default
// work_dir, which must still dispatch.
func TestBatchAdmissionBlockedPriorDoesNotStrandNewRequests(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	first := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic first"}, false))
	f.receipt.Started = false
	f.receipt.ExitCode = 125
	f.receipt.Preflight = "preflight_pi_missing"
	f.receipt.History = &computertarget.BatchHistoryReport{Before: "not_checked", After: "not_started"}
	f.receipt.Completion = nil
	notStarted := batchResultUntilReady(t, c, first.ID)
	if notStarted.PiResult == nil || notStarted.PiResult.Outcome != "not_started" {
		t.Fatalf("earlier prior must be a proven no-invocation receipt: %+v", notStarted.PiResult)
	}
	seedBlockedOperation(t, store, store.records[first.ID])
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

// TestBatchOnceStartedSessionStaysUnconfirmedOverBlockedPrior is the review
// counterexample for the admission-blocked relaxation: an earlier operation of
// the same Pi session did start Pi (its history files are gone — a valid
// started receipt with after=missing), a later request was blocked at
// admission, and a new request must NOT dispatch as first: a proven start may
// have had external side effects regardless of lost history.
func TestBatchOnceStartedSessionStaysUnconfirmedOverBlockedPrior(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	first := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic first"}, false))
	f.receipt.Started = true
	f.receipt.History = &computertarget.BatchHistoryReport{Before: "new", After: "missing"}
	batchResultUntilReady(t, c, first.ID)
	seedBlockedOperation(t, store, store.records[first.ID])
	f.runs = 0
	service := NewService(store, store, source.service.relay, nil, source.service.authorize)
	binding := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "run-c"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.submit(ctx, binding, Submit{Kind: "herdr_pi_task", Task: "synthetic followup"})
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "not_started" || next.Phase != "pi_continuity_unconfirmed" {
		t.Fatalf("once-started Pi session redispatched over a blocked prior: %+v", next)
	}
	if f.runs != 0 {
		t.Fatalf("expected no dispatch, got %d", f.runs)
	}
}

// TestBatchOnceStartedSessionStaysUnconfirmedAfterTargetChange is the review
// counterexample for the target-change relaxation: the Pi session started once
// (earlier operation with a started receipt), the latest prior above it is a
// proven no-invocation receipt under an already-changed target fingerprint
// (the failed-resume shape). A new request must stay unconfirmed instead of
// dispatching as first.
func TestBatchOnceStartedSessionStaysUnconfirmedAfterTargetChange(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	first := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic first"}, false))
	// op1 keeps its default started receipt with valid history evidence.
	batchResultUntilReady(t, c, first.ID)
	// Seed the failed-resume prior surgically above the completed started op:
	// receipt proves no invocation, target fingerprint already changed. This
	// avoids the shared transport receipt so the started evidence below stays
	// byte-stable.
	started := store.records[first.ID]
	var d Details
	if err := json.Unmarshal(started.Details, &d); err != nil || d.Batch == nil || d.Batch.Receipt == nil || !d.Batch.Receipt.Started {
		t.Fatalf("started evidence unreadable: %v %+v", err, d.Batch)
	}
	failedResume := started
	failedResume.ID = "failed-resume-prior-" + started.ID[:8]
	failedResume.State = "completed"
	failedResume.CreatedAt = started.CreatedAt.Add(time.Minute)
	failedResume.UpdatedAt = failedResume.CreatedAt
	failedResume.TargetFingerprint = "changed-by-agent-path"
	d.Phase = "pi_not_started"
	d.Batch.Result = &computertarget.PiJSONResult{Outcome: "not_started", Preflight: "preflight_pi_missing"}
	d.Batch.Receipt.Started = false
	d.Batch.Receipt.ExitCode = 125
	d.Batch.Receipt.Preflight = "preflight_pi_missing"
	d.Batch.Receipt.Completion = nil
	d.Batch.Receipt.History = &computertarget.BatchHistoryReport{Before: "not_checked", After: "not_started"}
	d.Batch.Progress.History = &computertarget.BatchHistoryExpectation{Version: computertarget.BatchHistoryVersion, Mode: "first"}
	failedResume.Details, _ = json.Marshal(d)
	store.records[failedResume.ID] = failedResume
	f.runs = 0
	service := NewService(store, store, source.service.relay, nil, source.service.authorize)
	binding := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "run-d"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.submit(ctx, binding, Submit{Kind: "herdr_pi_task", Task: "synthetic third"})
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "not_started" || next.Phase != "pi_continuity_unconfirmed" {
		t.Fatalf("once-started Pi session redispatched after target change: %+v", next)
	}
	if f.runs != 0 {
		t.Fatalf("expected no dispatch, got %d", f.runs)
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
	f.receipt.History = &computertarget.BatchHistoryReport{Before: "not_checked", After: "not_started"}
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
