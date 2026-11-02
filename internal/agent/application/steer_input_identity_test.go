package application

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	sdk "github.com/felinics/twilight/sdk"

	contextfrag "github.com/felinics/memoh/internal/agent/context/fragment"
	historyfrag "github.com/felinics/memoh/internal/agent/context/history"
	"github.com/felinics/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/step"
	chatview "github.com/felinics/memoh/internal/agent/view"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/runtimefence"
)

// steerIdentityQueries resolves the account and channel identities a queued
// input recorded, the same way the production store does.
type steerIdentityQueries struct {
	dbstore.Queries
}

func (steerIdentityQueries) GetUserByID(_ context.Context, id pgtype.UUID) (sqlc.User, error) {
	return sqlc.User{ID: id}, nil
}

func (steerIdentityQueries) GetChannelIdentityByID(_ context.Context, id pgtype.UUID) (sqlc.ChannelIdentity, error) {
	return sqlc.ChannelIdentity{}, pgx.ErrNoRows
}

// steerIdentityPersister adds the turn-slot allocator the production store
// implements. Without it the coordinator cannot name a claimed steer, and the
// claim frame the client attributes would never be published.
type steerIdentityPersister struct {
	*recordingStepPersister
	slots int64
}

func (p *steerIdentityPersister) AllocateTurnSlot(context.Context, string) (messagepkg.TurnSlot, error) {
	p.slots++
	return messagepkg.TurnSlot{TurnID: "turn-steer", Position: p.slots}, nil
}

func newSteerIdentityPersister() *steerIdentityPersister {
	return &steerIdentityPersister{recordingStepPersister: &recordingStepPersister{recordingMessageService: &recordingMessageService{}}}
}

// newSteerIdentityService builds a service whose queue step transaction can run
// without PostgreSQL and whose identity lookups resolve the sender.
func newSteerIdentityService(t *testing.T) (*Service, sessionruntime.RunHandle) {
	t.Helper()
	service, handle := newDeferredSteerTestService(t)
	service.queries = steerIdentityQueries{}
	service.messageService = newSteerIdentityPersister()
	return service, handle
}

func newSteerIdentityCommitter(t *testing.T, service *Service, handle sessionruntime.RunHandle, req ChatRequest) *agentStepCommitter {
	t.Helper()
	ctx := runtimefence.WithContext(context.Background(), runtimefence.Fence{
		BotID: handle.BotID, SessionID: handle.SessionID, Token: handle.FencingToken,
	})
	req.BotID, req.ThreadID, req.RunID, req.RunHandle = handle.BotID, handle.SessionID, handle.RunID, handle
	req.QueueSteerEnabled = true
	req.UserMessagePersisted = true
	req.PersistedUserMessageID = "request-message-id"
	committer := service.newAgentStepCommitter(ctx, req, resolvedContext{runConfig: native.RunConfig{ContextLifecycle: contextfrag.NewLifecycleHolder()}})
	if committer == nil {
		t.Fatal("missing agent step committer")
	}
	return committer
}

// stepEnd publishes the marker the native loop emits before its commit
// barrier. A claimed steer is anchored after that step, and the anchor waits
// for the live projection to consume it, so a commit without the marker would
// block exactly as it does in production.
func stepEnd(t *testing.T, service *Service, handle sessionruntime.RunHandle, index int) {
	t.Helper()
	if _, err := service.sessionManager.HandleAgentEvent(context.Background(), handle, native.StreamEvent{
		Type: native.EventStepEnd, StepNumber: index,
	}); err != nil {
		t.Fatalf("emit step end %d: %v", index, err)
	}
}

// claimFrames drains the runtime delta frames published for one claimed steer
// and returns the user turns they upserted. A frame that carries the steer
// without an attributed user turn is the shape the client renders as a
// foreign, unknown author, so the assertion is on the turns, not the delta.
func claimFrames(t *testing.T, subscription sessionruntime.Subscription, text string) []chatview.UITurn {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-subscription.C:
			if event.Type != sessionruntime.EventRuntimeDelta || event.Delta == nil {
				continue
			}
			if len(event.Delta.SteerTurnUpserts) == 0 {
				continue
			}
			turns := make([]chatview.UITurn, 0, len(event.Delta.UserTurnUpserts))
			for _, turn := range event.Delta.UserTurnUpserts {
				if turn.Text == text {
					turns = append(turns, turn)
				}
			}
			return turns
		case <-deadline:
			return nil
		}
	}
}

// A mid-run steer is a real user input: it must be persisted with the sender
// the ingress recorded and an input source of kind=user, exactly like the
// turn's own request row. Before this it landed with a NULL sender and
// kind=unknown, which the client correctly rendered as a foreign, unknown
// author on the left.
func TestMidRunSteerPersistsItsRecordedSender(t *testing.T) {
	service, handle := newSteerIdentityService(t)
	userID := "6c1c1de1-6f0e-4a1b-9d6d-2f9b4a6c8e11"
	input := testQueueInput(handle.BotID, handle.SessionID, "steer-invocation", "later question")
	input.UserID, input.SourceChannelIdentityID = userID, userID
	input.Channel = "web"
	if _, err := service.EnqueueSteer(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	committer := newSteerIdentityCommitter(t, service, handle, ChatRequest{UserID: userID, CurrentChannel: "web"})

	// Subscribe before the claim so the frame the client renders first is the
	// one inspected, not the settled projection that replaces it later.
	subscription, err := service.sessionManager.Subscribe(t.Context(), handle.BotID, handle.SessionID)
	if err != nil {
		t.Fatalf("subscribe runtime: %v", err)
	}
	defer subscription.Close()

	stepEnd(t, service, handle, 0)
	if _, err := committer.commit(t.Context(), 0, &step.Record{Messages: []sdk.Message{sdk.AssistantMessage("first answer")}}); err != nil {
		t.Fatalf("commit step claiming the steer: %v", err)
	}
	claimedFrames := claimFrames(t, subscription, "later question")
	if len(claimedFrames) == 0 {
		t.Fatal("the claimed steer was never published as an attributed user turn")
	}
	for _, frame := range claimedFrames {
		if frame.SenderUserID != userID {
			t.Fatalf("claimed steer frame sender = %q, want %q", frame.SenderUserID, userID)
		}
		if frame.InputSource == nil || frame.InputSource.Kind != inputsource.User || frame.InputSource.UserID != userID {
			t.Fatalf("claimed steer frame source = %+v, want kind=user with the recorded user", frame.InputSource)
		}
	}

	if _, err := committer.persist(t.Context(), 1, &step.Record{Messages: []sdk.Message{
		sdk.UserMessage("later question"),
		sdk.AssistantMessage("second answer"),
	}}, stepSteered, ""); err != nil {
		t.Fatalf("commit steered step: %v", err)
	}

	store := service.messageService.(*steerIdentityPersister)
	if len(store.steps) != 2 {
		t.Fatalf("persisted steps = %d, want two", len(store.steps))
	}
	var steerRow *messagepkg.PersistInput
	for i := range store.steps[1].Messages {
		if store.steps[1].Messages[i].Role == "user" {
			steerRow = &store.steps[1].Messages[i]
			break
		}
	}
	if steerRow == nil {
		t.Fatal("steered step persisted no user row")
	}
	if steerRow.SenderUserID != userID || steerRow.SenderChannelIdentityID != "" {
		t.Fatalf("steer row sender = (%q, %q), want the recorded account and no channel identity", steerRow.SenderUserID, steerRow.SenderChannelIdentityID)
	}
	source := inputsource.FromMetadata(steerRow.Metadata, handle.BotID, handle.SessionID)
	if source.Kind != inputsource.User || source.UserID != userID || source.Channel != "web" {
		t.Fatalf("steer row input source = %+v, want kind=user with the recorded user and channel", source)
	}

	// The assistant rows of the same steered step belong to the turn's own
	// admitted input; they must not inherit the steer's fallback source.
	for _, row := range store.steps[1].Messages {
		if row.Role != "assistant" {
			continue
		}
		assistantSource := inputsource.FromMetadata(row.Metadata, handle.BotID, handle.SessionID)
		if assistantSource.Kind != inputsource.User {
			t.Fatalf("assistant row of the steered step lost the turn source: %+v", assistantSource)
		}
	}

	// The live projection learns the same identity when the input is claimed,
	// so the bubble is attributed before its row exists.
	snapshot, err := service.sessionManager.Snapshot(t.Context(), handle.BotID, handle.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var claimed *chatview.UITurn
	for i := range snapshot.CurrentRunView.UserTurns {
		if snapshot.CurrentRunView.UserTurns[i].Text == "later question" {
			claimed = &snapshot.CurrentRunView.UserTurns[i]
			break
		}
	}
	if claimed == nil {
		t.Fatalf("claimed steer turn missing from the run view: %+v", snapshot.CurrentRunView.UserTurns)
	}
	if claimed.SenderUserID != userID {
		t.Fatalf("claimed steer turn sender = %q, want %q", claimed.SenderUserID, userID)
	}
	if claimed.InputSource == nil || claimed.InputSource.Kind != inputsource.User || claimed.InputSource.UserID != userID {
		t.Fatalf("claimed steer turn source = %+v, want kind=user with the recorded user", claimed.InputSource)
	}
}

// A queue item without a recorded command cannot name its sender. It must stay
// unattributed rather than borrow the enclosing turn's actor.
func TestSteeredInputWithoutRecordedSenderStaysUnattributed(t *testing.T) {
	service, handle := newSteerIdentityService(t)
	if _, err := service.sessionManager.EnqueueSteer(t.Context(), sessionruntime.Key{BotID: handle.BotID, SessionID: handle.SessionID}, "item-legacy", "legacy-invocation", []byte(`{"text":"legacy input"}`)); err != nil {
		t.Fatal(err)
	}
	committer := newSteerIdentityCommitter(t, service, handle, ChatRequest{UserID: "6c1c1de1-6f0e-4a1b-9d6d-2f9b4a6c8e11", CurrentChannel: "web"})
	stepEnd(t, service, handle, 0)
	if _, err := committer.commit(t.Context(), 0, &step.Record{Messages: []sdk.Message{sdk.AssistantMessage("first answer")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := committer.persist(t.Context(), 1, &step.Record{Messages: []sdk.Message{
		sdk.UserMessage("legacy input"),
		sdk.AssistantMessage("second answer"),
	}}, stepSteered, ""); err != nil {
		t.Fatal(err)
	}
	store := service.messageService.(*steerIdentityPersister)
	for _, row := range store.steps[1].Messages {
		if row.Role != "user" {
			continue
		}
		if row.SenderUserID != "" || row.SenderChannelIdentityID != "" {
			t.Fatalf("legacy steer row borrowed a sender: %#v", row)
		}
		if source := inputsource.FromMetadata(row.Metadata, handle.BotID, handle.SessionID); source.Kind != inputsource.Unknown {
			t.Fatalf("legacy steer row source = %+v, want unknown", source)
		}
	}
}

// TestPersistedUserRowsNeverClaimUserSourceWithoutSender is the invariant the
// E7 regression slipped through: for an identity this service can resolve, any
// user row recorded as kind=user must name the sender that row belongs to. A
// row whose actor cannot be named must not claim the kind at all. The steered
// row is the regression's own shape and is checked through the real commit
// path below.
func TestPersistedUserRowsNeverClaimUserSourceWithoutSender(t *testing.T) {
	bot, session := "8f0f6f0e-4b7c-4f1c-9d61-3f2a1c0b7d21", "2d1a3c4b-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	user := "6c1c1de1-6f0e-4a1b-9d6d-2f9b4a6c8e11"
	cases := map[string]ChatRequest{
		"turn leading input":  {BotID: bot, ThreadID: session, UserID: user, CurrentChannel: "web", Query: "hello", TurnID: "turn-1"},
		"skill activation":    {BotID: bot, ThreadID: session, UserID: user, CurrentChannel: "web", UserMessageKind: UserMessageKindSkillActivation, UserVisibleText: "skill", TurnID: "turn-2"},
		"input with no actor": {BotID: bot, ThreadID: session, Query: "hello", TurnID: "turn-1"},
		"automation input":    {BotID: bot, ThreadID: session, UserID: user, SessionType: "schedule", Query: "tick"},
	}
	service := &Service{queries: steerIdentityQueries{}}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			inputs, err := service.buildPersistInputs(t.Context(), req, []ModelMessage{
				{Role: "user", Content: newTextContent("hello")},
				{Role: "assistant", Content: newTextContent("hi")},
			}, "", storeRoundOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range inputs {
				if row.Role != "user" || messagepkg.IsInternalFeedback(row.Metadata) {
					continue
				}
				source := inputsource.FromMetadata(row.Metadata, bot, session)
				if source.Kind != inputsource.User {
					continue
				}
				if row.SenderUserID == "" && row.SenderChannelIdentityID == "" {
					t.Fatalf("user row claims kind=user without a sender: %#v", row)
				}
				if source.UserID == "" && source.ChannelIdentityID == "" {
					t.Fatalf("user row source names no actor: %+v", source)
				}
			}
		})
	}

	service2, handle := newSteerIdentityService(t)
	input := testQueueInput(handle.BotID, handle.SessionID, "invariant-steer", "later question")
	input.UserID, input.SourceChannelIdentityID = user, user
	if _, err := service2.EnqueueSteer(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	committer := newSteerIdentityCommitter(t, service2, handle, ChatRequest{UserID: user, CurrentChannel: "web"})
	stepEnd(t, service2, handle, 0)
	if _, err := committer.commit(t.Context(), 0, &step.Record{Messages: []sdk.Message{sdk.AssistantMessage("first answer")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := committer.persist(t.Context(), 1, &step.Record{Messages: []sdk.Message{
		sdk.UserMessage("later question"), sdk.AssistantMessage("second answer"),
	}}, stepSteered, ""); err != nil {
		t.Fatal(err)
	}
	for _, row := range service2.messageService.(*steerIdentityPersister).steps[1].Messages {
		if row.Role != "user" || messagepkg.IsInternalFeedback(row.Metadata) {
			continue
		}
		source := inputsource.FromMetadata(row.Metadata, handle.BotID, handle.SessionID)
		if source.Kind != inputsource.User {
			continue
		}
		if row.SenderUserID == "" {
			t.Fatalf("steered user row claims kind=user without a sender: %#v", row)
		}
	}
}

// The persisted projection must agree with the live one: a steer whose sender
// is recorded projects a self-authored user turn, not an unknown one.
func TestSteeredRowProjectsSelfAuthoredTurn(t *testing.T) {
	bot, session := "8f0f6f0e-4b7c-4f1c-9d61-3f2a1c0b7d21", "2d1a3c4b-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	user := "6c1c1de1-6f0e-4a1b-9d6d-2f9b4a6c8e11"
	source := inputsource.Source{Kind: inputsource.User, BotID: bot, SessionID: session, UserID: user, Channel: "web"}
	row := messagepkg.Message{
		ID: "steer-row", BotID: bot, SessionID: session, Role: "user", TurnID: "turn-steer",
		SenderUserID: user, DisplayContent: "later question",
		Content:  mustStoredUserContent(t, "later question"),
		Metadata: map[string]any{inputsource.MetadataKey: source, "platform": "web"},
	}
	turns := chatview.ConvertMessagesToUITurns([]messagepkg.Message{row})
	if len(turns) != 1 {
		t.Fatalf("projected turns = %d, want one", len(turns))
	}
	if turns[0].InputSource == nil || turns[0].InputSource.Kind != inputsource.User || turns[0].SenderUserID != user {
		t.Fatalf("steer turn projected without its author: %+v", turns[0])
	}
}

func mustStoredUserContent(t *testing.T, text string) json.RawMessage {
	t.Helper()
	raw, err := historyfrag.MarshalStoredModelMessage(ModelMessage{Role: "user", Content: newTextContent(text)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
