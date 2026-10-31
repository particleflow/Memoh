package application

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agent/turn"
	chatview "github.com/felinics/memoh/internal/agent/view"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

func TestBotTaskResultQueueVisibilityAndReplay(t *testing.T) {
	cmd := deliveryCommand()
	cmd.UserVisibleText = "legacy machine result"
	payload, err := encodeQueueCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{payload, []byte(`{"text":"legacy raw result","command":{"delegation_result_id":"one","Query":"raw query","UserVisibleText":"raw display"}}`)} {
		if text := QueuePayloadText(data); text != "" {
			t.Fatalf("result leaked into queue: %q", text)
		}
		if _, err := rewriteQueuePayloadText(data, cmd.UserID, "rewrite result"); !errors.Is(err, ErrQueueItemNotEditable) {
			t.Fatalf("internal input editable: %v", err)
		}
	}
	restored := decodeQueuePayload(payload).Command
	if restored == nil || restored.Query != cmd.Query || restored.DelegationResultID != cmd.DelegationResultID || len(restored.Attachments) != 1 {
		t.Fatalf("model input lost: %+v", restored)
	}
	if got := chatview.NewRequestUserTurn(*restored, "result-turn"); got != nil {
		t.Fatalf("live user bubble: %+v", got)
	}
	// Exactly the fields the deployed a0 consumer decodes. It has no command
	// to execute and no text to render, instead of silently downgrading origin.
	var legacy struct {
		Text    string                 `json:"text"`
		Command *turn.StartTurnCommand `json:"command"`
	}
	if err := json.Unmarshal(payload, &legacy); err != nil || legacy.Text != "" || legacy.Command != nil {
		t.Fatalf("a0 consumer could display or execute new internal input: %+v %v", legacy, err)
	}
}

func TestBotTaskResultTwoQueuedResultsKeepIdentityAndHostInput(t *testing.T) {
	runs := sessionledger.New()
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	received := make(chan ChatRequest, 8)
	stream := testChatStreamerFunc(func(ctx context.Context, req ChatRequest) (<-chan StreamChunk, <-chan error) {
		received <- req
		chunks, errs := make(chan StreamChunk), make(chan error)
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
	s, manager := deliveryApplication(t, runs, stream)
	s.SetDelegationScope(func(ctx context.Context, req ChatRequest) (context.Context, ChatRequest, error) {
		if req.DelegationResultID != "" {
			req.ModelQuery = "host verified " + req.DelegationResultID
		}
		return ctx, req, nil
	})
	initial := deliveryCommand()
	initial.Query = "busy"
	initial.DelegationResultID = ""
	initial.IdempotencyKey = "initial"
	h, err := s.StartTurn(t.Context(), initial)
	if err != nil {
		t.Fatal(err)
	}
	<-received
	for _, id := range []string{"one", "two"} {
		cmd := deliveryCommand()
		cmd.DelegationResultID = id
		cmd.Query = "result " + id
		for range 2 {
			if err := s.EnqueueAuthorizedFollowUp(t.Context(), "result:"+id, cmd); err != nil {
				t.Fatal(err)
			}
		}
	}
	queues, err := s.ListSessionQueues(t.Context(), "bot", "session")
	if err != nil || len(queues.FollowUp) != 2 {
		t.Fatalf("result dedup failed: %+v %v", queues, err)
	}
	for _, item := range queues.FollowUp {
		if QueuePayloadText(item.Payload) != "" {
			t.Fatal("queued result leaked")
		}
	}
	releaseOnce.Do(func() { close(release) })
	drainDeferredTurn(h)
	seen := map[string]bool{}
	for range 2 {
		select {
		case req := <-received:
			if seen[req.DelegationResultID] || req.Query != "result "+req.DelegationResultID || req.ModelQuery != "host verified "+req.DelegationResultID {
				t.Fatalf("mixed result identity or discarded host input: %+v", req)
			}
			seen[req.DelegationResultID] = true
		case <-time.After(3 * time.Second):
			t.Fatal("result not consumed")
		}
	}
	awaitDeliveryIdle(t, manager)
	for _, id := range []string{"one", "two"} {
		cmd := deliveryCommand()
		cmd.DelegationResultID = id
		cmd.Query = "result " + id
		if err := s.EnqueueAuthorizedFollowUp(t.Context(), "result:"+id, cmd); err != nil {
			t.Fatal(err)
		}
	}
	runs.Mu.Lock()
	count := len(runs.Runs)
	runs.Mu.Unlock()
	if count != 3 || len(received) != 0 || !seen["one"] || !seen["two"] {
		t.Fatalf("duplicate or missing result: runs=%d received=%d seen=%v", count, len(received), seen)
	}
}

func TestBotTaskResultResumeRetainsOriginAndRejectsRevocationBeforeAdmission(t *testing.T) {
	q := &resumeSaveQueries{}
	s := &Service{queries: q, resumeSecret: "synthetic-resume"}
	ctx := runtimefence.WithContext(t.Context(), runtimefence.Fence{BotID: "bot", SessionID: "session", Token: 7})
	req := ChatRequest{RunID: uuid.NewString(), ChatID: "chat", UserID: "user", Query: "machine input", ModelQuery: "derived instructions", RouteID: "route", CurrentChannel: "web", ReplyTarget: "bot", DelegationResultID: "one"}
	if err := s.recordRunResumeContext(ctx, req); err != nil {
		t.Fatal(err)
	}
	var data resumeContext
	if err := json.Unmarshal(q.saved.ResumeContext, &data); err != nil {
		t.Fatal(err)
	}
	if data.Version != 3 || data.Query != req.Query || data.DelegationResultID != "one" || data.RouteID != "route" || !data.resumable() {
		t.Fatalf("lost resume origin: %+v", data)
	}
	data.DelegationResultID = ""
	if data.resumable() {
		t.Fatal("originless v3 was resumable")
	}
	data.DelegationResultID = "one"
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "authorized", true: "revoked"}[revoke], func(t *testing.T) {
			runner := &fakeRunner{chunks: []string{`{"type":"agent_end"}`}}
			app, admitter := newAdmittedTurnTestService(runner)
			app.logger = slog.New(slog.DiscardHandler)
			const team = "00000000-0000-0000-0000-000000000001"
			app.SetAllowedTeam(team)
			denied := errors.New("revoked")
			app.SetDelegationScope(func(ctx context.Context, got ChatRequest) (context.Context, ChatRequest, error) {
				if got.DelegationResultID != "one" || got.RouteID != "route" || got.UserID != "user" {
					t.Errorf("resume scope lost: %+v", got)
				}
				if revoke {
					return ctx, got, denied
				}
				return ctx, got, nil
			})
			raw, _ := json.Marshal(map[string]any{"resume": data})
			row := sqlc.SessionRun{TeamID: db.ParseUUIDOrEmpty(team), BotID: db.ParseUUIDOrEmpty(uuid.NewString()), SessionID: db.ParseUUIDOrEmpty(uuid.NewString()), RunID: db.ParseUUIDOrEmpty(uuid.NewString()), InputJson: raw}
			done, err := app.resumeInterruptedSession(t.Context(), row)
			if revoke {
				if !errors.Is(err, denied) || done != nil || len(admitter.admitted()) != 0 {
					t.Fatalf("revoked result admitted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("resume did not finish")
			}
			if runner.gotReq.DelegationResultID != "one" || runner.gotReq.RouteID != "route" || !runner.gotReq.UserMessagePersisted {
				t.Fatalf("resumed as ordinary input: %+v", runner.gotReq)
			}
		})
	}
}

func TestBotTaskResultCannotBeRetriedOrEditedAsUserInput(t *testing.T) {
	messages := &replacementOperationMessageService{latest: messagepkg.HistoryTurn{ID: "result-turn", RequestMessageID: "internal", AssistantMessageID: "reply"}, requestMetadata: map[string]any{"message_source": "internal_feedback", "internal_input_kind": "bot_task_result"}}
	s := &Service{messageService: messages}
	if _, err := s.PrepareRetryLatestTurnOperation(t.Context(), "session", "result-turn"); err == nil {
		t.Fatal("internal result retry accepted")
	}
	if _, err := s.PrepareEditLatestTurnOperation(t.Context(), "session", "result-turn"); err == nil {
		t.Fatal("internal result edit accepted")
	}
}

func TestBotTaskResultHistoryRetainsInputAndOnlyRendersExtractedReply(t *testing.T) {
	s := &Service{logger: slog.New(slog.DiscardHandler)}
	pos := int64(5)
	req := ChatRequest{BotID: "bot", ThreadID: "session", Query: "Delegation one execution state: failed.", UserVisibleText: "legacy visible raw", RawQuery: "legacy raw", DelegationResultID: "one", RunID: "run", TurnID: "result-turn", TurnPosition: &pos}
	inputs, err := s.buildPersistInputs(context.Background(), req, prependTurnUserMessage(req, []ModelMessage{{Role: "assistant", Content: newTextContent("这次处理失败了，我已保留原任务。")}}), "", storeRoundOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 || inputs[0].TurnID != req.TurnID || inputs[0].RunID != req.RunID || messagepkg.IsInternalFeedback(inputs[0].Metadata) || inputs[0].Metadata["message_source"] != "internal_feedback" || inputs[0].DisplayText != "" {
		t.Fatalf("result lost own turn or leaked display: %+v", inputs)
	}
	if persistedTextContent(t, inputs[0].Content) != req.Query {
		t.Fatal("internal model history erased")
	}
	rows := make([]messagepkg.Message, 0, len(inputs))
	for _, input := range inputs {
		meta, _ := json.Marshal(input.Metadata)
		rows = append(rows, messagepkg.Message{Role: input.Role, Content: input.Content, RawMetadata: meta, TurnID: req.TurnID, TurnPosition: &pos})
	}
	got := chatview.ConvertMessagesToUITurns(rows)
	if len(got) != 1 || got[0].Role != "assistant" || len(got[0].Messages) != 1 || got[0].Messages[0].Content != "这次处理失败了，我已保留原任务。" {
		t.Fatalf("history rendered machine input or lost response: %+v", got)
	}
	// Plain user text with a machine-looking body remains user text. Visibility
	// follows host provenance, never text matching or a model-selected kind.
	if got := chatview.NewRequestUserTurn(turn.StartTurnCommand{Query: req.Query}, "user-turn"); got == nil || got.Text != req.Query {
		t.Fatal("ordinary user speech was filtered")
	}
}

func TestBotTaskResultEarlyPersistenceUsesSameVisibility(t *testing.T) {
	messages := &recordingMessageService{}
	s := &Service{messageService: messages, logger: slog.New(slog.DiscardHandler)}
	req := ChatRequest{BotID: "bot", ThreadID: "session", Query: "raw result", RawQuery: "raw", UserVisibleText: "visible", DelegationResultID: "one"}
	if _, err := s.persistUserTurn(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if len(messages.persisted) != 1 || messages.persisted[0].DisplayText != "" || messages.persisted[0].Metadata["message_source"] != "internal_feedback" || persistedTextContent(t, messages.persisted[0].Content) != req.Query {
		t.Fatalf("early input diverges: %+v", messages.persisted)
	}
}
