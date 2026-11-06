package application

import (
	"context"
	"errors"
	"testing"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

type nilQueries struct{ dbstore.Queries }

type deferredReplacementPersister struct {
	*recordingStepPersister
	finalizations int
	requestID     string
	assistantID   string
	finalizeErr   error
}

func (p *deferredReplacementPersister) FinalizeAgentReplacement(_ context.Context, _ string, _ messagepkg.TurnReplacement, requestID, assistantID string) error {
	p.finalizations++
	p.requestID, p.assistantID = requestID, assistantID
	return p.finalizeErr
}

func TestDeferredReplacementFinalizationFailureDoesNotClaimSteer(t *testing.T) {
	service, handle := newDeferredSteerTestService(t)
	failure := errors.New("synthetic replacement write rejected")
	persister := &deferredReplacementPersister{recordingStepPersister: service.messageService.(*recordingStepPersister), finalizeErr: failure}
	service.messageService = persister
	item, err := service.EnqueueSteer(t.Context(), testQueueInput(handle.BotID, handle.SessionID, "replacement-failure-steer", "later input"))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := newQueueStepCoordinator(service, ChatRequest{
		BotID: handle.BotID, ThreadID: handle.SessionID, RunID: handle.RunID,
		RunHandle: handle, QueueSteerEnabled: true,
		TurnReplacement: &messagepkg.TurnReplacement{RequestMessageID: "original-user"},
	})
	outcome, err := coordinator.commit(t.Context(), queueStepDeferredDecision, messagepkg.AgentStep{RunID: handle.RunID}, []messagepkg.Message{{ID: "ask-assistant", Role: "assistant"}})
	if !errors.Is(err, failure) || !outcome.historyCommitted || outcome.replacementFinalized || outcome.claimedSteer != nil || persister.finalizations != 1 {
		t.Fatalf("failed selection was treated as ready: %+v error=%v finalizations=%d", outcome, err, persister.finalizations)
	}
	pending, _, err := service.sessionManager.PendingQueues(t.Context(), sessionruntime.Key{BotID: handle.BotID, SessionID: handle.SessionID}, 0)
	if err != nil || len(pending) != 1 || pending[0].ID != item.ID || pending[0].Status != sessionruntime.QueueAccepted {
		t.Fatalf("failed selection changed the queue: %+v %v", pending, err)
	}
}

// A retry/edit persists its intermediate rows outside visible history. Parking
// on ask_user must select that exact replacement before the original stream
// releases the continuation barrier; otherwise the accepted answer cannot find
// its admitted turn. This boundary must still leave queued steers untouched.
func TestDeferredReplacementSelectsVisibleTurnBeforeAnswer(t *testing.T) {
	service, handle := newDeferredSteerTestService(t)
	persister := &deferredReplacementPersister{recordingStepPersister: service.messageService.(*recordingStepPersister)}
	service.messageService = persister
	item, err := service.EnqueueSteer(t.Context(), testQueueInput(handle.BotID, handle.SessionID, "replacement-steer", "later input"))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := newQueueStepCoordinator(service, ChatRequest{
		BotID: handle.BotID, ThreadID: handle.SessionID, RunID: handle.RunID,
		RunHandle: handle, QueueSteerEnabled: true,
		TurnReplacement: &messagepkg.TurnReplacement{RequestMessageID: "original-user"},
	})
	outcome, err := coordinator.commit(t.Context(), queueStepDeferredDecision, messagepkg.AgentStep{RunID: handle.RunID}, []messagepkg.Message{{ID: "ask-assistant", Role: "assistant"}})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.replacementFinalized || persister.finalizations != 1 || persister.requestID != "original-user" || persister.assistantID != "ask-assistant" {
		t.Fatalf("parked replacement was not selected: outcome=%+v finalizations=%d request=%q assistant=%q", outcome, persister.finalizations, persister.requestID, persister.assistantID)
	}
	if outcome.claimedSteer != nil {
		t.Fatal("deferred replacement claimed a steer")
	}
	pending, _, err := service.sessionManager.PendingQueues(t.Context(), sessionruntime.Key{BotID: handle.BotID, SessionID: handle.SessionID}, 0)
	if err != nil || len(pending) != 1 || pending[0].ID != item.ID || pending[0].Status != sessionruntime.QueueAccepted {
		t.Fatalf("deferred replacement changed the queue: %+v %v", pending, err)
	}
}

type queueTestFence struct{}

func (queueTestFence) Activate(context.Context, string, string, int64) error { return nil }

// newDeferredSteerTestService builds a Service whose queue step transaction
// can run without PostgreSQL: steps carry no messages, so history persistence
// is skipped, and queue state lives in a memory backend with one active run.
func newDeferredSteerTestService(t *testing.T, backends ...sessionruntime.Backend) (*Service, sessionruntime.RunHandle) {
	t.Helper()
	var backend sessionruntime.Backend = sessionruntime.NewMemoryBackend()
	if len(backends) > 0 {
		backend = backends[0]
	}
	manager := sessionruntime.NewManager(backend, sessionruntime.Options{OwnerID: "owner-1", Ledger: sessionledger.New(), Fence: queueTestFence{}})
	t.Cleanup(func() { _ = manager.Close() })
	admitted, err := manager.Admit(context.Background(), sessionruntime.AdmitInput{
		BotID: "bot", SessionID: "session", InvocationID: "initial", Payload: []byte(`{}`),
		Execution: sessionruntime.Execution{Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
			return sessionruntime.RunAdmissionView{}, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	handle := admitted.Handle
	if err := manager.EnableSteer(context.Background(), handle); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		sessionManager: manager,
		messageService: &recordingStepPersister{recordingMessageService: &recordingMessageService{}},
		queries:        nilQueries{},
	}
	return service, handle
}

// A deferred step parks the loop, so its commit must not claim a steer: a
// claim would sit unapplied across the decision and across any owner change
// while the run waits. The continuation's first committed step claims it
// instead, and the next step applies it once.
func TestDeferredStepDoesNotClaimSteerAndContinuationDeliversIt(t *testing.T) {
	service, handle := newDeferredSteerTestService(t)
	ctx := context.Background()
	key := sessionruntime.Key{BotID: handle.BotID, SessionID: handle.SessionID}
	item, err := service.EnqueueSteer(ctx, testQueueInput(handle.BotID, handle.SessionID, "invoke-1", "steer me"))
	if err != nil {
		t.Fatal(err)
	}

	// Original run: the deferred step commits without touching the queue.
	original := newQueueStepCoordinator(service, ChatRequest{
		BotID: handle.BotID, ThreadID: handle.SessionID, RunID: handle.RunID,
		RunHandle: handle, QueueSteerEnabled: true,
	})
	if original == nil {
		t.Fatal("queue step transaction unavailable")
	}
	outcome, err := original.commit(ctx, queueStepDeferredDecision, messagepkg.AgentStep{RunID: handle.RunID}, nil)
	if err != nil {
		t.Fatalf("deferred commit: %v", err)
	}
	if outcome.claimedSteer != nil || outcome.appliedSteerItemID != "" {
		t.Fatalf("deferred step touched the steer queue: %#v", outcome)
	}

	if steers, _, err := service.sessionManager.PendingQueues(ctx, key, 0); err != nil || len(steers) != 1 || steers[0].Status != sessionruntime.QueueAccepted {
		t.Fatalf("steer should stay accepted across the park: %#v, %v", steers, err)
	}

	// Continuation after the decision uses a fresh coordinator with the same run.
	continuation := newQueueStepCoordinator(service, ChatRequest{
		BotID: handle.BotID, ThreadID: handle.SessionID, RunID: handle.RunID,
		RunHandle: handle, QueueSteerEnabled: true, UserMessagePersisted: true,
	})
	if continuation == nil {
		t.Fatal("continuation queue step transaction unavailable")
	}

	// Step N+1: the model call that consumed the approved tool result. Its
	// commit claims the steer for step N+2.
	outcome, err = continuation.commit(ctx, queueStepToolLoop, messagepkg.AgentStep{RunID: handle.RunID}, nil)
	if err != nil {
		t.Fatalf("continuation commit: %v", err)
	}
	if outcome.appliedSteerItemID != "" {
		t.Fatalf("continuation applied a steer it never delivered: %#v", outcome)
	}
	if outcome.claimedSteer == nil || outcome.claimedSteer.ID != item.ID {
		t.Fatalf("continuation did not claim the steer: %#v", outcome)
	}
	if got := QueuePayloadText(outcome.claimedSteer.Payload); got != "steer me" {
		t.Fatalf("claimed payload = %q", got)
	}

	steers, _, err := service.sessionManager.PendingQueues(ctx, key, 0)
	if err != nil || len(steers) != 0 {
		t.Fatalf("pending steers while claimed = %#v, %v", steers, err)
	}

	// Step N+2 saw the steer; its commit applies the claim exactly once.
	outcome, err = continuation.commit(ctx, queueStepFinal, messagepkg.AgentStep{RunID: handle.RunID}, nil)
	if err != nil {
		t.Fatalf("final commit: %v", err)
	}
	if outcome.appliedSteerItemID != string(item.ID) || outcome.claimedSteer != nil {
		t.Fatalf("final outcome = %#v", outcome)
	}
	if _, err := service.sessionManager.UpdateSteer(ctx, key, item.ID, []byte("x")); err == nil {
		t.Fatal("applied steer still mutable")
	}
	if _, err := service.EnqueueSteer(ctx, testQueueInput(handle.BotID, handle.SessionID, "invoke-2", "late")); !errors.Is(err, sessionruntime.ErrQueueNoActiveRun) {
		t.Fatalf("late steer after sealed final = %v", err)
	}
}

func TestFinalStepClaimsSteerForLoopContinuation(t *testing.T) {
	service, handle := newDeferredSteerTestService(t)
	ctx := context.Background()
	item, err := service.EnqueueSteer(ctx, testQueueInput(handle.BotID, handle.SessionID, "invoke-1", "change direction"))
	if err != nil {
		t.Fatal(err)
	}

	coordinator := newQueueStepCoordinator(service, ChatRequest{
		BotID: handle.BotID, ThreadID: handle.SessionID, RunID: handle.RunID,
		RunHandle: handle, QueueSteerEnabled: true,
	})
	if coordinator == nil {
		t.Fatal("queue step coordinator unavailable")
	}
	outcome, err := coordinator.commit(ctx, queueStepFinal, messagepkg.AgentStep{RunID: handle.RunID}, nil)
	if err != nil {
		t.Fatalf("final commit: %v", err)
	}
	if outcome.claimedSteer == nil || outcome.claimedSteer.ID != item.ID {
		t.Fatalf("final did not claim the steer: %#v", outcome)
	}
	if got := QueuePayloadText(outcome.claimedSteer.Payload); got != "change direction" {
		t.Fatalf("claimed payload = %q", got)
	}
	if _, err := service.EnqueueSteer(ctx, testQueueInput(handle.BotID, handle.SessionID, "invoke-2", "late")); err != nil {
		t.Fatalf("run should stay open after a claimed final steer: %v", err)
	}
}
