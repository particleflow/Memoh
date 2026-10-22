package application

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

// This uses the real queue, admission manager and application StartTurn. Only
// the model stream and durable storage adapter are fixtures.
func deliveryApplication(t *testing.T, runs *sessionledger.Store, stream testChatStreamer) (*Service, *sessionruntime.Manager) {
	t.Helper()
	manager := sessionruntime.NewManager(sessionruntime.NewMemoryBackend(), sessionruntime.Options{Ledger: runs, Fence: queueTestFence{}})
	t.Cleanup(func() { _ = manager.Close() })
	service := &Service{sessionRuntime: manager, sessionManager: manager, turnHooks: &turnRuntimeHooks{streamChat: stream.StreamChat}}
	manager.SetTerminalObserver(func(ctx context.Context, terminal sessionruntime.TerminalRun) {
		service.startFollowUpAfterTerminal(ctx, terminal)
	})
	return service, manager
}

func awaitDeliveryIdle(t *testing.T, manager *sessionruntime.Manager) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot, err := manager.Snapshot(t.Context(), "bot", "session")
		if err == nil && snapshot.CurrentRunView != nil && !sessionruntime.IsActiveRunStatus(snapshot.CurrentRunView.Status) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not finish: %v %+v", err, snapshot.CurrentRunView)
		}
		time.Sleep(time.Millisecond)
	}
}

func deliveryCommand() turn.StartTurnCommand {
	return turn.StartTurnCommand{TeamID: "team", BotID: "bot", ThreadID: "session", UserID: "user", Mode: turn.ModeChat, Query: "specialist result", DelegationResultID: "delegation", Attachments: []turn.Attachment{{Type: "image", ContentHash: "original", Metadata: map[string]any{"delegation_url": "/scoped/original"}}}}
}

func TestAuthorizedDeliveryWakesIdleSupervisorAndSurvivesQueueRestart(t *testing.T) {
	runs := sessionledger.New()
	received := make(chan ChatRequest, 8)
	stream := testChatStreamerFunc(func(_ context.Context, req ChatRequest) (<-chan StreamChunk, <-chan error) {
		received <- req
		chunks := make(chan StreamChunk)
		errs := make(chan error)
		close(chunks)
		close(errs)
		return chunks, errs
	})
	service, manager := deliveryApplication(t, runs, stream)
	initial := deliveryCommand()
	initial.Query = "supervisor starts specialist"
	initial.IdempotencyKey = "original"
	initial.DelegationResultID = ""
	initial.Attachments = nil
	handle, err := service.StartTurn(t.Context(), initial)
	if err != nil {
		t.Fatal(err)
	}
	drainDeferredTurn(handle)
	awaitDeliveryIdle(t, manager)
	<-received
	// The specialist completes only after the supervisor has already finished.
	ctx, cancel := context.WithCancel(t.Context())
	cmd := deliveryCommand()
	if err := service.EnqueueAuthorizedFollowUp(ctx, "result:one", cmd); err != nil {
		t.Fatal(err)
	}
	cancel() // Delivery request finishing must not cancel the supervisor turn.
	got := <-received
	if got.DelegationResultID != cmd.DelegationResultID || got.Query != cmd.Query || !reflect.DeepEqual(got.Attachments, cmd.Attachments) {
		t.Fatalf("lost result/attachments: %+v", got)
	}
	awaitDeliveryIdle(t, manager)
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulated process/queue replacement retains only the durable run ledger.
	restarted, _ := deliveryApplication(t, runs, stream)
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failures <- restarted.EnqueueAuthorizedFollowUp(t.Context(), "result:one", cmd)
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	runs.Mu.Lock()
	count := len(runs.Runs)
	runs.Mu.Unlock()
	if count != 2 || len(received) != 0 {
		t.Fatalf("delivery retry re-executed: runs=%d calls=%d", count, len(received))
	}
	changed := cmd
	changed.Query = "different result"
	if err := restarted.EnqueueAuthorizedFollowUp(t.Context(), "result:one", changed); !errors.Is(err, sessionruntime.ErrInvocationConflict) {
		t.Fatalf("changed replay accepted: %v", err)
	}
}

func TestAuthorizedDeliveryBusyQueueRechecksRevocation(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "deliver", true: "revoked"}[revoke], func(t *testing.T) {
			runs := sessionledger.New()
			release := make(chan struct{})
			received := make(chan ChatRequest, 4)
			stream := testChatStreamerFunc(func(ctx context.Context, req ChatRequest) (<-chan StreamChunk, <-chan error) {
				received <- req
				chunks := make(chan StreamChunk)
				errs := make(chan error)
				go func() {
					defer close(chunks)
					defer close(errs)
					if req.Query == "busy" {
						select {
						case <-release:
						case <-ctx.Done():
						}
					}
				}()
				return chunks, errs
			})
			service, manager := deliveryApplication(t, runs, stream)
			denied := errors.New("revoked")
			var revoked atomic.Bool
			service.SetDelegationScope(func(ctx context.Context, req ChatRequest) (context.Context, ChatRequest, error) {
				if req.DelegationResultID != "" && revoked.Load() {
					return ctx, req, denied
				}
				return ctx, req, nil
			})
			cmd := deliveryCommand()
			initial := cmd
			initial.Query = "busy"
			initial.DelegationResultID = ""
			initial.IdempotencyKey = "busy"
			handle, err := service.StartTurn(t.Context(), initial)
			if err != nil {
				t.Fatal(err)
			}
			<-received
			for range 8 {
				if err := service.EnqueueAuthorizedFollowUp(t.Context(), "result:busy", cmd); err != nil {
					t.Fatal(err)
				}
			}
			queues, err := service.ListSessionQueues(t.Context(), "bot", "session")
			if err != nil || len(queues.FollowUp) != 1 {
				t.Fatalf("not one queued result: %+v %v", queues, err)
			}
			revoked.Store(revoke)
			close(release)
			drainDeferredTurn(handle)
			if !revoke {
				select {
				case got := <-received:
					if !reflect.DeepEqual(got.Attachments, cmd.Attachments) {
						t.Fatal("lost queued attachment")
					}
				case <-time.After(3 * time.Second):
					t.Fatal("result not delivered")
				}
				awaitDeliveryIdle(t, manager)
			} else {
				deadline := time.Now().Add(3 * time.Second)
				for {
					_, busy := service.followUpStarts.Load(sessionruntime.Key{BotID: "bot", SessionID: "session"}.String())
					if !busy {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("follow-up did not settle")
					}
					time.Sleep(time.Millisecond)
				}
				if len(received) != 0 {
					t.Fatal("revoked result reached model")
				}
				if err := service.EnqueueAuthorizedFollowUp(t.Context(), "result:busy", cmd); !errors.Is(err, denied) {
					t.Fatalf("idle retry bypassed revocation: %v", err)
				}
			}
		})
	}
}

// A concurrent inbound user turn may win precisely between the queue's idle
// answer and result admission. It must leave one result queued for that turn.
type deliveryRaceBackend struct {
	*sessionruntime.MemoryBackend
	once         sync.Once
	beforeReturn func()
}

func (b *deliveryRaceBackend) EnqueueFollowUp(ctx context.Context, key sessionruntime.Key, itemID, invocationID string, payload []byte) (sessionruntime.FollowUpItem, error) {
	item, err := b.MemoryBackend.EnqueueFollowUp(ctx, key, itemID, invocationID, payload)
	if errors.Is(err, sessionruntime.ErrQueueNoActiveRun) {
		b.once.Do(b.beforeReturn)
	}
	return item, err
}

func TestAuthorizedDeliveryIdleToBusyRaceQueuesSameInvocation(t *testing.T) {
	runs := sessionledger.New()
	backend := &deliveryRaceBackend{MemoryBackend: sessionruntime.NewMemoryBackend()}
	manager := sessionruntime.NewManager(backend, sessionruntime.Options{Ledger: runs, Fence: queueTestFence{}})
	t.Cleanup(func() { _ = manager.Close() })
	received := make(chan ChatRequest, 2)
	service := &Service{sessionRuntime: manager, sessionManager: manager, turnHooks: &turnRuntimeHooks{streamChat: func(_ context.Context, req ChatRequest) (<-chan StreamChunk, <-chan error) {
		received <- req
		ch := make(chan StreamChunk)
		errs := make(chan error)
		close(ch)
		close(errs)
		return ch, errs
	}}}
	manager.SetTerminalObserver(func(ctx context.Context, terminal sessionruntime.TerminalRun) {
		service.startFollowUpAfterTerminal(ctx, terminal)
	})
	var competing sessionruntime.Admission
	backend.beforeReturn = func() {
		var err error
		competing, err = manager.Admit(t.Context(), sessionruntime.AdmitInput{BotID: "bot", SessionID: "session", InvocationID: "competing", Payload: []byte(`{}`), Execution: sessionruntime.Execution{Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
			return sessionruntime.RunAdmissionView{}, nil
		}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	cmd := deliveryCommand()
	if err := service.EnqueueAuthorizedFollowUp(t.Context(), "result:race", cmd); err != nil {
		t.Fatal(err)
	}
	queues, err := service.ListSessionQueues(t.Context(), "bot", "session")
	if err != nil || len(queues.FollowUp) != 1 || len(received) != 0 {
		t.Fatalf("race result %+v %v", queues, err)
	}
	if _, err := manager.FinishRunWithErrorCode(t.Context(), competing.Handle, sessionruntime.RunStatusCompleted, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if !reflect.DeepEqual(got.Attachments, cmd.Attachments) {
			t.Fatal("race lost attachment")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("race stranded result")
	}
	awaitDeliveryIdle(t, manager)
	if err := service.EnqueueAuthorizedFollowUp(t.Context(), "result:race", cmd); err != nil {
		t.Fatal(err)
	}
	if len(received) != 0 {
		t.Fatal("race retry executed twice")
	}
}
