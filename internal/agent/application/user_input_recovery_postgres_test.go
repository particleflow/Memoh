package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/agent/decision"
	userinput "github.com/felinics/memoh/internal/agent/decision/input"
	"github.com/felinics/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/agent/toolexec"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/runtimefence"
)

type acceptedInputRecoveryFixture struct {
	service  *Service
	pool     *pgxpool.Pool
	store    *postgresstore.Queries
	input    UserInputRecoveryInput
	requests *capturedModelRequests
	original sqlc.SessionRun
	decision userinput.Request
}

func newAcceptedInputRecoveryFixture(t *testing.T) acceptedInputRecoveryFixture {
	t.Helper()
	ctx := t.Context()
	pool := openTurnAdmissionPostgres(t, ctx)
	botID, sessionID := createTurnAdmissionFixture(t, ctx, pool)
	store := postgresstore.NewQueriesWithPool(pool, sqlc.New(pool))
	messages := messagepkg.NewService(nil, store)
	user, err := messages.Persist(ctx, messagepkg.PersistInput{BotID: botID, SessionID: sessionID, Role: "user", Content: []byte(`{"role":"user","content":"SYNTHETIC_ORIGINAL_INPUT"}`)})
	if err != nil {
		t.Fatal(err)
	}
	previous, err := messages.Persist(ctx, messagepkg.PersistInput{BotID: botID, SessionID: sessionID, Role: "assistant", Content: []byte(`{"role":"assistant","content":"SYNTHETIC_PRIOR_FAILURE"}`), TurnRequestMessageID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	source, err := messages.GetVisibleTurnByMessage(ctx, sessionID, previous.ID)
	if err != nil {
		t.Fatal(err)
	}
	modelFixture := newDirectLifecycleFixture(t, directLifecycleModelSuccess)
	service := modelFixture.service
	fake := service.queries.(*directLifecycleQueries)
	providerID, modelID := uuid.NewString(), uuid.NewString()
	fake.provider.ID, fake.modelID = db.ParseUUIDOrEmpty(providerID), db.ParseUUIDOrEmpty(modelID)
	for key, model := range fake.models {
		model.ID, model.ProviderID = fake.modelID, fake.provider.ID
		fake.models[key] = model
	}
	if _, err := pool.Exec(ctx, `INSERT INTO providers(id,name,client_type,enable,config) VALUES($1,$2,'openai-completions',true,$3)`, providerID, "synthetic-recovery-"+providerID, fake.provider.Config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM providers WHERE id=$1`, providerID) })
	if _, err := pool.Exec(ctx, `INSERT INTO models(id,model_id,provider_id) VALUES($1,'synthetic-recovery',$2)`, modelID, providerID); err != nil {
		t.Fatal(err)
	}
	service.queries, service.messageService = store, messages
	service.userInput = userinput.NewService(nil, store)
	manager := sessionruntime.NewManager(sessionruntime.NewMemoryBackend(), sessionruntime.Options{OwnerID: "accepted-answer-test", Ledger: ledger.NewPostgres(sqlc.New(pool), pool), Fence: runtimefence.NewActivator(store)})
	service.SetSessionRuntime(manager)
	t.Cleanup(func() { _ = manager.CloseContext(context.Background()) })
	payload, _ := json.Marshal(map[string]any{"kind": "retry_message", "turn_id": source.ID, "resume": resumeContext{Version: 1, Query: "SYNTHETIC_ORIGINAL_INPUT", ChatID: botID, CurrentChannel: "local", ReplyTarget: botID, ConversationType: "private", Model: modelID}})
	admission, err := manager.Admit(ctx, sessionruntime.AdmitInput{BotID: botID, SessionID: sessionID, InvocationID: "original-retry", Payload: payload, Execution: sessionruntime.Execution{Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
		return sessionruntime.RunAdmissionView{}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	owner := runtimefence.WithContext(ctx, runtimefence.Fence{BotID: botID, SessionID: sessionID, Token: admission.Handle.FencingToken})
	input, err := service.userInput.CreatePending(owner, userinput.CreatePendingInput{BotID: botID, SessionID: sessionID, ToolCallID: "synthetic-original-ask", Input: map[string]any{"questions": []any{map[string]any{"text": "Continue the synthetic request?", "kind": "text"}}}})
	if err != nil {
		t.Fatal(err)
	}
	// Preserve real hidden step rows as older code left them. No real tool has
	// run: these are synthetic persisted facts the model must not replay.
	prefix := []sdk.Message{
		{Role: sdk.MessageRoleAssistant, Content: []sdk.MessagePart{sdk.ToolCallPart{ToolCallID: "already-done", ToolName: "synthetic_side_effect", Input: toolexec.ArgumentsFromValue(map[string]any{})}}},
		sdk.ToolMessage(sdk.ToolResultPart{ToolCallID: "already-done", ToolName: "synthetic_side_effect", Result: toolexec.OutputFromValue(map[string]any{"completed": true})}),
		{Role: sdk.MessageRoleAssistant, Content: []sdk.MessagePart{sdk.ToolCallPart{ToolCallID: input.ToolCallID, ToolName: "ask_user", Input: toolexec.ArgumentsFromValue(input.Input)}}},
	}
	for _, message := range prefix {
		content, _ := json.Marshal(message)
		if _, err := messages.Persist(owner, messagepkg.PersistInput{BotID: botID, SessionID: sessionID, RunID: admission.RunID, Role: string(message.Role), Content: content, SkipHistoryTurn: true, SessionMode: "chat", RuntimeType: "model"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.HandleAgentEvent(owner, admission.Handle, native.StreamEvent{Type: native.EventUserInputRequest, UserInputID: input.ID, ToolCallID: input.ToolCallID, Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.userInput.Submit(decision.WithNativeContinuation(owner), userinput.SubmitInput{RequestID: input.ID, Answers: []userinput.QuestionAnswer{{QuestionID: input.UIPayload.Questions[0].ID, Text: "SYNTHETIC_ACCEPTED_ANSWER"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.continuationTurnRequestMessageID(owner, sessionID, admission.Handle); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("fixture did not reproduce the original NoRows boundary: %v", err)
	}
	if _, err := manager.FinishRunWithErrorCode(owner, admission.Handle, sessionruntime.RunStatusErrored, "runtime_run_failed"); err != nil {
		t.Fatal(err)
	}
	original, err := store.GetSessionRun(ctx, db.ParseUUIDOrEmpty(admission.RunID))
	if err != nil {
		t.Fatal(err)
	}
	var actor string
	if err := pool.QueryRow(ctx, `SELECT created_by_user_id::text FROM bot_sessions WHERE id=$1`, sessionID).Scan(&actor); err != nil {
		// The shared fixture leaves the creator unset; use its synthetic owner.
		if err := pool.QueryRow(ctx, `SELECT owner_user_id::text FROM bots WHERE id=$1`, botID).Scan(&actor); err != nil {
			t.Fatal(err)
		}
	}
	return acceptedInputRecoveryFixture{service: service, pool: pool, store: store, requests: modelFixture.requests, original: original, decision: input, input: UserInputRecoveryInput{BotID: botID, SessionID: sessionID, OriginalRunID: admission.RunID, DecisionID: input.ID, ActorID: actor, Reauthorize: func(context.Context) error { return nil }}}
}

func TestPostgresAcceptedUserInputRecoveryResumesOnceWithoutReplayingTurn(t *testing.T) {
	f := newAcceptedInputRecoveryFixture(t)
	originalDecision, err := f.store.GetUserInputRequest(t.Context(), db.ParseUUIDOrEmpty(f.input.DecisionID))
	if err != nil {
		t.Fatal(err)
	}
	const callers = 4
	results := make(chan UserInputRecoveryResult, callers)
	errorsCh := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			result, err := f.service.RecoverAcceptedUserInput(t.Context(), f.input)
			results <- result
			errorsCh <- err
		})
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	var recoveryID string
	started := 0
	for result := range results {
		if recoveryID == "" {
			recoveryID = result.RecoveryRunID
		}
		if result.RecoveryRunID != recoveryID || result.OriginalRunID != f.input.OriginalRunID || result.DecisionID != f.input.DecisionID {
			t.Fatalf("recovery identity changed: %+v", result)
		}
		if result.Started {
			started++
		}
	}
	if started != 1 || recoveryID == f.input.OriginalRunID {
		t.Fatalf("started=%d recovery=%q", started, recoveryID)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		run, err := f.store.GetSessionRun(t.Context(), db.ParseUUIDOrEmpty(recoveryID))
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "completed" {
			break
		}
		if run.State == "failed" {
			t.Fatalf("recovery failed: code=%s requests=%d", run.ErrorCode.String, len(f.requests.all()))
		}
		select {
		case <-deadline.C:
			t.Fatalf("recovery did not settle: state=%s requests=%d", run.State, len(f.requests.all()))
		case <-tick.C:
		}
	}
	requests := f.requests.all()
	if len(requests) != 1 || !strings.Contains(string(requests[0]), "SYNTHETIC_ACCEPTED_ANSWER") || !strings.Contains(string(requests[0]), "already-done") {
		t.Fatalf("accepted context not resumed once: requests=%d", len(requests))
	}
	original, _ := f.store.GetSessionRun(t.Context(), f.original.RunID)
	if original.State != "failed" || original.ErrorCode != f.original.ErrorCode || string(original.InputJson) != string(f.original.InputJson) || original.UpdatedAt != f.original.UpdatedAt {
		t.Fatal("original failed run was rewritten")
	}
	decision, _ := f.service.userInput.Get(t.Context(), f.input.DecisionID)
	if decision.Status != userinput.StatusSubmitted {
		t.Fatal("accepted decision was resubmitted or changed")
	}
	retainedDecision, _ := f.store.GetUserInputRequest(t.Context(), originalDecision.ID)
	if retainedDecision.RespondedAt != originalDecision.RespondedAt || string(retainedDecision.ResultJson) != string(originalDecision.ResultJson) || retainedDecision.ResponseControlID != originalDecision.ResponseControlID {
		t.Fatal("recovery rewrote the accepted response receipt")
	}
	recoveryRow, _ := f.store.GetSessionRun(t.Context(), db.ParseUUIDOrEmpty(recoveryID))
	anchor, err := f.service.continuationTurnRequestMessageID(t.Context(), f.input.SessionID, sessionruntime.RunHandle{BotID: f.input.BotID, SessionID: f.input.SessionID, RunID: recoveryID, TurnID: recoveryRow.TurnID.String(), FencingToken: recoveryRow.FencingToken})
	if err != nil || anchor == "" {
		t.Fatalf("a later decision in this recovery lost its exact original anchor: %q %v", anchor, err)
	}
	var answerRows int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM bot_history_messages WHERE session_id=$1 AND role='tool' AND metadata->>'recovered_user_input_id'=$2`, f.input.SessionID, f.input.DecisionID).Scan(&answerRows); err != nil || answerRows != 1 {
		t.Fatalf("accepted answer persisted %d times: %v", answerRows, err)
	}
	if result, err := f.service.RecoverAcceptedUserInput(t.Context(), f.input); err != nil || result.Started || !result.Replayed || result.RecoveryRunID != recoveryID || len(f.requests.all()) != 1 {
		t.Fatalf("terminal replay restarted execution: %+v %v", result, err)
	}
}

func TestPostgresAcceptedUserInputRecoveryRetainsOriginalInputOutsideRecentWindow(t *testing.T) {
	f := newAcceptedInputRecoveryFixture(t)
	if _, err := f.pool.Exec(t.Context(), `UPDATE bot_history_messages SET created_at=created_at-interval '2 days' WHERE session_id=$1`, f.input.SessionID); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.RecoverAcceptedUserInput(t.Context(), f.input)
	if err != nil || !result.Started {
		t.Fatalf("historical recovery not admitted: %+v %v", result, err)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		run, err := f.store.GetSessionRun(t.Context(), db.ParseUUIDOrEmpty(result.RecoveryRunID))
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "completed" {
			break
		}
		if run.State == "failed" {
			t.Fatalf("historical recovery failed: %s", run.ErrorCode.String)
		}
		select {
		case <-deadline.C:
			t.Fatal("historical recovery did not settle")
		case <-tick.C:
		}
	}
	requests := f.requests.all()
	if len(requests) != 1 || strings.Count(string(requests[0]), "SYNTHETIC_ORIGINAL_INPUT") != 1 || !strings.Contains(string(requests[0]), "SYNTHETIC_ACCEPTED_ANSWER") || !strings.Contains(string(requests[0]), "already-done") {
		t.Fatalf("historical recovery lost or replayed original context: requests=%d", len(requests))
	}
}

func TestPostgresAcceptedUserInputRecoveryRejectsLostFence(t *testing.T) {
	f := newAcceptedInputRecoveryFixture(t)
	row, err := f.store.GetUserInputRequest(t.Context(), db.ParseUUIDOrEmpty(f.input.DecisionID))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := f.service.failedUserInputSnapshot(t.Context(), f.input, row)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := f.service.sessionManager.Admit(t.Context(), sessionruntime.AdmitInput{BotID: f.input.BotID, SessionID: f.input.SessionID, InvocationID: "stale-fence-attempt", Payload: []byte(`{}`), Execution: sessionruntime.Execution{Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
		return sessionruntime.RunAdmissionView{}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	stale := runtimefence.WithContext(t.Context(), runtimefence.Fence{BotID: f.input.BotID, SessionID: f.input.SessionID, Token: f.original.FencingToken})
	if err := f.service.reconcileAcceptedUserInput(stale, f.input, saved, admission); !errors.Is(err, runtimefence.ErrStale) {
		t.Fatalf("stale writer was admitted: %v", err)
	}
	if len(f.requests.all()) != 0 {
		t.Fatal("lost owner invoked a model")
	}
}

func TestPostgresAcceptedUserInputRecoveryRejectsUnsafeCheckpoints(t *testing.T) {
	for _, scenario := range []string{"revoked", "cross_bot", "cross_session", "changed_decision", "executing_checkpoint", "post_answer_output", "existing_answer_result", "foreign_tail", "newer_run", "invocation_collision", "changed_source_turn", "cas_changed", "revoked_after_admission"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAcceptedInputRecoveryFixture(t)
			ctx := t.Context()
			switch scenario {
			case "revoked":
				f.input.Reauthorize = func(context.Context) error { return userinput.ErrForbidden }
			case "cross_bot":
				f.input.BotID = uuid.NewString()
			case "cross_session":
				f.input.SessionID = uuid.NewString()
			case "changed_decision":
				_, err := f.pool.Exec(ctx, `UPDATE user_input_requests SET status='canceled' WHERE id=$1`, f.input.DecisionID)
				if err != nil {
					t.Fatal(err)
				}
			case "executing_checkpoint":
				_, err := f.pool.Exec(ctx, `UPDATE session_runs SET input_json=jsonb_set(input_json,ARRAY['decision_continuations',$2,'phase'],'"executing"') WHERE run_id=$1`, f.input.OriginalRunID, f.input.DecisionID)
				if err != nil {
					t.Fatal(err)
				}
			case "post_answer_output", "existing_answer_result", "foreign_tail":
				message := sdk.AssistantMessage("synthetic later output")
				runID := f.input.OriginalRunID
				if scenario == "existing_answer_result" {
					message = sdk.ToolMessage(sdk.ToolResultPart{ToolCallID: f.decision.ToolCallID, ToolName: "ask_user", Result: toolexec.OutputFromValue(map[string]any{"already_saved": true})})
				}
				if scenario == "foreign_tail" {
					runID = uuid.NewString()
				}
				content, _ := json.Marshal(message)
				if _, err := f.service.messageService.Persist(ctx, messagepkg.PersistInput{BotID: f.input.BotID, SessionID: f.input.SessionID, RunID: runID, Role: string(message.Role), Content: content, SkipHistoryTurn: true, RuntimeType: "model"}); err != nil {
					t.Fatal(err)
				}
			case "newer_run", "invocation_collision":
				invocation := "newer-user-input"
				if scenario == "invocation_collision" {
					invocation = acceptedUserInputRecoveryKind + ":" + f.input.DecisionID
				}
				admission, err := f.service.sessionManager.Admit(ctx, sessionruntime.AdmitInput{BotID: f.input.BotID, SessionID: f.input.SessionID, InvocationID: invocation, Payload: []byte(`{"kind":"message"}`), Execution: sessionruntime.Execution{Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
					return sessionruntime.RunAdmissionView{}, nil
				}}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.service.sessionManager.FinishRun(ctx, admission.Handle, sessionruntime.RunStatusCompleted); err != nil {
					t.Fatal(err)
				}
			case "changed_source_turn":
				if _, err := f.service.messageService.Persist(ctx, messagepkg.PersistInput{BotID: f.input.BotID, SessionID: f.input.SessionID, Role: "user", Content: []byte(`{"role":"user","content":"different later input"}`)}); err != nil {
					t.Fatal(err)
				}
			case "cas_changed", "revoked_after_admission":
				var checks atomic.Int32
				f.input.Reauthorize = func(ctx context.Context) error {
					if checks.Add(1) != 2 {
						return nil
					}
					if scenario == "revoked_after_admission" {
						return userinput.ErrForbidden
					}
					_, err := f.pool.Exec(ctx, `UPDATE session_runs SET updated_at=updated_at+interval '1 millisecond' WHERE run_id=$1`, f.input.OriginalRunID)
					return err
				}
			}
			if result, err := f.service.RecoverAcceptedUserInput(ctx, f.input); err == nil || result.Started {
				t.Fatalf("unsafe checkpoint started: %+v %v", result, err)
			}
			if len(f.requests.all()) != 0 {
				t.Fatal("unsafe recovery invoked the upstream")
			}
			var repaired int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM bot_history_messages WHERE session_id=$1 AND metadata->>'recovered_user_input_id'=$2`, f.original.SessionID.String(), f.input.DecisionID).Scan(&repaired); err != nil || repaired != 0 {
				t.Fatalf("unsafe recovery changed answer history: count=%d error=%v", repaired, err)
			}
			original, _ := f.store.GetSessionRun(ctx, f.original.RunID)
			if original.State != "failed" || original.ErrorCode != f.original.ErrorCode {
				t.Fatal("unsafe recovery changed original terminal state")
			}
		})
	}
}

type commitUnknownRecoveryStore struct{ *postgresstore.Queries }

func (q *commitUnknownRecoveryStore) InTx(ctx context.Context, fn func(dbstore.Queries) error) error {
	if err := q.Queries.InTx(ctx, fn); err != nil {
		return err
	}
	return db.ErrCommitOutcomeUnknown
}

func TestPostgresAcceptedUserInputRecoveryUnknownCommitDoesNotStartOrRetryModel(t *testing.T) {
	f := newAcceptedInputRecoveryFixture(t)
	f.service.queries = &commitUnknownRecoveryStore{Queries: f.store}
	result, err := f.service.RecoverAcceptedUserInput(t.Context(), f.input)
	if !errors.Is(err, db.ErrCommitOutcomeUnknown) || result.Started {
		t.Fatalf("uncertain commit was treated as accepted execution: %+v %v", result, err)
	}
	replay, err := f.service.RecoverAcceptedUserInput(t.Context(), f.input)
	if err != nil || replay.Started || !replay.Replayed || replay.RecoveryRunID != result.RecoveryRunID || len(f.requests.all()) != 0 {
		t.Fatalf("uncertain outcome was replayed: %+v %v requests=%d", replay, err, len(f.requests.all()))
	}
}
