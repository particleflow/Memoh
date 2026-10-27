package application

import (
	"context"
	"log/slog"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/decision"
	userinput "github.com/felinics/memoh/internal/agent/decision/input"
	"github.com/felinics/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/agent/step"
	"github.com/felinics/memoh/internal/agent/toolexec"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/runtimefence"
)

// Exercise the actual fenced step writer, replacement transaction and native
// decision store. No model or external tool is invoked: the accepted answer is
// persisted at the continuation boundary, before any new business execution.
func TestPostgresDeferredReplacementKeepsDecisionContinuationAddressable(t *testing.T) {
	for _, reason := range []string{"retry", "edit"} {
		t.Run(reason, func(t *testing.T) {
			ctx := t.Context()
			pool := openTurnAdmissionPostgres(t, ctx)
			botID, sessionID := createTurnAdmissionFixture(t, ctx, pool)
			store := postgresstore.NewQueriesWithPool(pool, sqlc.New(pool))
			messages := messagepkg.NewService(nil, store)
			user, err := messages.Persist(ctx, messagepkg.PersistInput{BotID: botID, SessionID: sessionID, Role: "user", Content: []byte(`{"role":"user","content":"synthetic original input"}`)})
			if err != nil {
				t.Fatal(err)
			}
			oldAssistant, err := messages.Persist(ctx, messagepkg.PersistInput{BotID: botID, SessionID: sessionID, Role: "assistant", Content: []byte(`{"role":"assistant","content":"synthetic prior answer"}`), TurnRequestMessageID: user.ID})
			if err != nil {
				t.Fatal(err)
			}
			oldTurn, err := messages.GetVisibleTurnByMessage(ctx, sessionID, oldAssistant.ID)
			if err != nil {
				t.Fatal(err)
			}
			manager := sessionruntime.NewManager(sessionruntime.NewMemoryBackend(), sessionruntime.Options{OwnerID: "decision-replacement-test", Ledger: ledger.NewPostgres(sqlc.New(pool), pool), Fence: runtimefence.NewActivator(store)})
			t.Cleanup(func() { _ = manager.CloseContext(context.Background()) })
			admission, err := manager.Admit(ctx, sessionruntime.AdmitInput{BotID: botID, SessionID: sessionID, InvocationID: reason, Payload: []byte(`{}`), Execution: sessionruntime.Execution{Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
				return sessionruntime.RunAdmissionView{}, nil
			}}})
			if err != nil {
				t.Fatal(err)
			}
			owner := runtimefence.WithContext(ctx, runtimefence.Fence{BotID: botID, SessionID: sessionID, Token: admission.Handle.FencingToken})
			inputs := userinput.NewService(nil, store)
			pending, err := inputs.CreatePending(owner, userinput.CreatePendingInput{BotID: botID, SessionID: sessionID, ToolCallID: "synthetic-ask", Input: map[string]any{"questions": []any{map[string]any{"text": "Continue?", "kind": "text"}}}})
			if err != nil {
				t.Fatal(err)
			}
			service := &Service{logger: slog.New(slog.DiscardHandler), messageService: messages, queries: store, userInput: inputs}
			service.SetSessionRuntime(manager)
			replacement := &messagepkg.TurnReplacement{OldTurnID: oldTurn.ID, ReplacementTurnID: admission.TurnID, ReplacementTurnPosition: &admission.TurnPosition, Reason: reason}
			req := ChatRequest{BotID: botID, ThreadID: sessionID, RunID: admission.RunID, RunHandle: admission.Handle, TurnID: admission.TurnID, TurnPosition: &admission.TurnPosition, Query: "synthetic replacement input", SkipHistoryTurn: true, TurnReplacement: replacement}
			if reason == "retry" {
				replacement.RequestMessageID = user.ID
				req.UserMessagePersisted, req.ReusePersistedUserMessage, req.PersistedUserMessageID = true, true, user.ID
			}
			committer := service.newAgentStepCommitter(owner, req, resolvedContext{})
			if committer == nil {
				t.Fatal("missing real step committer")
			}
			if _, err := manager.HandleAgentEvent(owner, admission.Handle, native.StreamEvent{Type: native.EventUserInputRequest, UserInputID: pending.ID, ToolCallID: pending.ToolCallID, Status: "pending"}); err != nil {
				t.Fatal(err)
			}
			if _, err := committer.commit(owner, 0, &step.Record{Messages: []sdk.Message{{Role: sdk.MessageRoleAssistant, Content: []sdk.MessagePart{sdk.ToolCallPart{ToolCallID: pending.ToolCallID, ToolName: "ask_user", Input: toolexec.ArgumentsFromValue(pending.Input)}}}}, Deferred: &toolexec.ToolApprovalResult{ApprovalID: pending.ID}}); err != nil {
				t.Fatal(err)
			}
			requestID, err := service.continuationTurnRequestMessageID(owner, sessionID, admission.Handle)
			if err != nil || requestID == "" {
				t.Fatalf("accepted-answer history anchor unavailable: %q %v", requestID, err)
			}
			answer, err := inputs.Submit(decision.WithNativeContinuation(owner), userinput.SubmitInput{RequestID: pending.ID, Answers: []userinput.QuestionAnswer{{QuestionID: pending.UIPayload.Questions[0].ID, Text: "synthetic accepted answer"}}})
			if err != nil || answer.ID != pending.ID || answer.Status != userinput.StatusSubmitted {
				t.Fatalf("original decision was not accepted: %+v %v", answer, err)
			}
			if err := service.storeRoundWithOptions(owner, ChatRequest{BotID: botID, ThreadID: sessionID, RunID: admission.RunID, UserMessagePersisted: true, PersistedUserMessageID: requestID, SkipMemoryExtraction: true}, sdkMessagesToModelMessages([]sdk.Message{sdk.ToolMessage(sdk.ToolResultPart{ToolCallID: pending.ToolCallID, ToolName: pending.ToolName, Result: toolexec.OutputFromValue(answer.Result)})}), "", storeRoundOptions{AllowPendingToolCalls: true, RequireCompletePersist: true}); err != nil {
				t.Fatal(err)
			}
			var askResults, oldRows int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM bot_history_messages WHERE run_id=$1 AND turn_id=$2 AND role='tool' AND turn_visible`, admission.RunID, admission.TurnID).Scan(&askResults); err != nil || askResults != 1 {
				t.Fatalf("accepted tool result count=%d error=%v", askResults, err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM bot_history_messages WHERE id=$1`, oldAssistant.ID).Scan(&oldRows); err != nil || oldRows != 1 {
				t.Fatalf("original history deleted: count=%d error=%v", oldRows, err)
			}
		})
	}
}
