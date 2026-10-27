package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/agent/decision"
	userinput "github.com/felinics/memoh/internal/agent/decision/input"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/sessionmode"
	"github.com/felinics/memoh/internal/agent/toolexec"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/runtimefence"
)

var ErrUserInputRecoveryUnavailable = errors.New("accepted user input cannot be recovered from this checkpoint")

const acceptedUserInputRecoveryKind = "accepted_user_input_recovery"

type UserInputRecoveryInput struct {
	BotID, SessionID, OriginalRunID, DecisionID, ActorID string
	ChatToken                                            string
	// Reauthorize rechecks the existing account/Bot/session gate. It is a
	// server-owned callback, never serialized or accepted from request JSON.
	Reauthorize func(context.Context) error
}

type UserInputRecoveryResult struct {
	OriginalRunID string `json:"original_run_id"`
	DecisionID    string `json:"decision_id"`
	RecoveryRunID string `json:"recovery_run_id"`
	InvocationID  string `json:"invocation_id"`
	State         string `json:"state"`
	Started       bool   `json:"started"`
	Replayed      bool   `json:"replayed"`
}

type acceptedUserInputRecovery struct {
	Kind             string `json:"kind"`
	OriginalRunID    string `json:"original_run_id"`
	DecisionID       string `json:"decision_id"`
	RequestMessageID string `json:"request_message_id"`
}

type failedUserInputRecoveryQueries interface {
	GetSessionRunByInvocation(context.Context, sqlc.GetSessionRunByInvocationParams) (sqlc.SessionRun, error)
	LockFailedUserInputRecovery(context.Context, sqlc.LockFailedUserInputRecoveryParams) (pgtype.UUID, error)
	ListFailedUserInputRecoveryMessages(context.Context, sqlc.ListFailedUserInputRecoveryMessagesParams) ([]sqlc.BotHistoryMessage, error)
	FailedUserInputRecoveryTailIsExclusive(context.Context, sqlc.FailedUserInputRecoveryTailIsExclusiveParams) (bool, error)
	CreateMessageInHistoryTurnByRequestAndBind(context.Context, sqlc.CreateMessageInHistoryTurnByRequestAndBindParams) (sqlc.CreateMessageInHistoryTurnByRequestAndBindRow, error)
}

type failedUserInputSnapshot struct {
	run      sqlc.SessionRun
	decision sqlc.UserInputRequest
	resume   resumeContext
	source   messagepkg.HistoryTurn
}

// RecoverAcceptedUserInput is an explicit user action. The old failed run and
// submitted decision remain facts; normal admission gives the continuation a
// new, durably idempotent run. It never resubmits the answer or retries a turn.
func (s *Service) RecoverAcceptedUserInput(ctx context.Context, in UserInputRecoveryInput) (UserInputRecoveryResult, error) {
	if in.Reauthorize == nil || s == nil || s.sessionRuntime == nil || s.userInput == nil || s.agent == nil {
		return UserInputRecoveryResult{}, ErrUserInputRecoveryUnavailable
	}
	if err := in.Reauthorize(ctx); err != nil {
		return UserInputRecoveryResult{}, err
	}
	store, ok := s.queries.(failedUserInputRecoveryQueries)
	if !ok {
		return UserInputRecoveryResult{}, ErrUserInputRecoveryUnavailable
	}
	for _, id := range []string{in.BotID, in.SessionID, in.OriginalRunID, in.DecisionID, in.ActorID} {
		if _, err := db.ParseUUID(id); err != nil {
			return UserInputRecoveryResult{}, ErrUserInputRecoveryUnavailable
		}
	}
	decisionRow, err := s.queries.GetUserInputRequest(ctx, db.ParseUUIDOrEmpty(in.DecisionID))
	if err != nil {
		return UserInputRecoveryResult{}, err
	}
	if decisionRow.BotID.String() != in.BotID || decisionRow.SessionID.String() != in.SessionID || decisionRow.RunID.String() != in.OriginalRunID || decisionRow.Status != userinput.StatusSubmitted {
		return UserInputRecoveryResult{}, ErrUserInputRecoveryUnavailable
	}
	invocationID := acceptedUserInputRecoveryKind + ":" + in.DecisionID
	result := UserInputRecoveryResult{OriginalRunID: in.OriginalRunID, DecisionID: in.DecisionID, InvocationID: invocationID}
	if replay, err := store.GetSessionRunByInvocation(ctx, sqlc.GetSessionRunByInvocationParams{SessionID: decisionRow.SessionID, InvocationID: invocationID}); err == nil {
		var intent acceptedUserInputRecovery
		if replay.BotID.String() != in.BotID || json.Unmarshal(replay.InputJson, &intent) != nil ||
			intent.Kind != acceptedUserInputRecoveryKind || intent.OriginalRunID != in.OriginalRunID || intent.DecisionID != in.DecisionID || intent.RequestMessageID == "" {
			return result, ErrUserInputRecoveryUnavailable
		}
		result.RecoveryRunID, result.State, result.Replayed = replay.RunID.String(), replay.State, true
		return result, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	snapshot, err := s.failedUserInputSnapshot(ctx, in, decisionRow)
	if err != nil {
		return result, err
	}
	// No resume payload is installed until history reconciliation commits. A
	// crash before that boundary must not restart the original query on boot.
	payload, err := json.Marshal(acceptedUserInputRecovery{Kind: acceptedUserInputRecoveryKind, OriginalRunID: in.OriginalRunID, DecisionID: in.DecisionID, RequestMessageID: snapshot.source.RequestMessageID})
	if err != nil {
		return result, err
	}
	endActive, err := s.activeTurns.begin()
	if err != nil {
		return result, err
	}
	runBase, cancelCause := context.WithCancelCause(context.WithoutCancel(ctx))
	cancel := func() { cancelCause(context.Canceled) }
	admission, err := s.sessionRuntime.Admit(runBase, sessionruntime.AdmitInput{BotID: in.BotID, SessionID: in.SessionID, InvocationID: invocationID, Payload: payload, Execution: sessionruntime.Execution{Cancel: cancel, OwnershipCancel: cancelCause, Admission: func(context.Context, sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
		return sessionruntime.RunAdmissionView{}, nil
	}}})
	if err != nil || !admission.Started {
		cancel()
		endActive()
		if err == nil {
			result.RecoveryRunID, result.State, result.Replayed = admission.RunID, string(admission.State), true
		}
		return result, err
	}
	runCtx := s.withAdmissionRuntimeFence(runBase, admission)
	finish := s.turnRunFinisher(runCtx, admission, sessionmode.Chat)
	result.RecoveryRunID, result.State = admission.RunID, string(admission.State)
	if err := s.reconcileAcceptedUserInput(runCtx, in, snapshot, admission); err != nil {
		finish(RunOutcome{Status: sessionruntime.RunStatusErrored, Cause: err})
		cancel()
		endActive()
		return result, err
	}
	result.Started = true
	go func() {
		defer cancel()
		defer endActive()
		err := s.runRecoveredUserInput(runCtx, in, snapshot, admission)
		finish(RunOutcome{Cause: err})
	}()
	return result, nil
}

func (s *Service) failedUserInputSnapshot(ctx context.Context, in UserInputRecoveryInput, decisionRow sqlc.UserInputRequest) (failedUserInputSnapshot, error) {
	run, err := s.queries.GetSessionRun(ctx, decisionRow.RunID)
	if err != nil {
		return failedUserInputSnapshot{}, err
	}
	var saved struct {
		Kind                  string                                     `json:"kind"`
		TurnID                string                                     `json:"turn_id"`
		Resume                *resumeContext                             `json:"resume"`
		DecisionContinuations map[string]decision.ContinuationCheckpoint `json:"decision_continuations"`
	}
	if err := json.Unmarshal(run.InputJson, &saved); err != nil {
		return failedUserInputSnapshot{}, ErrUserInputRecoveryUnavailable
	}
	checkpoint := saved.DecisionContinuations[in.DecisionID]
	if run.BotID.String() != in.BotID || run.SessionID.String() != in.SessionID || run.State != "failed" || run.ErrorCode.String != "runtime_run_failed" ||
		saved.Kind != "retry_message" || saved.Resume == nil || saved.Resume.Version != 1 || strings.TrimSpace(saved.Resume.Query) == "" ||
		len(saved.DecisionContinuations) != 1 || checkpoint.Kind != runtimefence.DecisionUserInput || checkpoint.Phase != "accepted" ||
		!decisionRow.RuntimeFencingToken.Valid || decisionRow.RuntimeFencingToken.Int64 != run.FencingToken || run.AbortRequestedAt.Valid {
		return failedUserInputSnapshot{}, ErrUserInputRecoveryUnavailable
	}
	if _, err := db.ParseUUID(saved.TurnID); err != nil {
		return failedUserInputSnapshot{}, ErrUserInputRecoveryUnavailable
	}
	source, err := s.queries.GetHistoryTurnByID(ctx, sqlc.GetHistoryTurnByIDParams{SessionID: run.SessionID, OldTurnID: db.ParseUUIDOrEmpty(saved.TurnID)})
	if err != nil {
		return failedUserInputSnapshot{}, err
	}
	if source.BotID != run.BotID || !source.RequestMessageID.Valid {
		return failedUserInputSnapshot{}, ErrUserInputRecoveryUnavailable
	}
	return failedUserInputSnapshot{run: run, decision: decisionRow, resume: *saved.Resume, source: messagepkg.HistoryTurn{ID: saved.TurnID, RequestMessageID: source.RequestMessageID.String()}}, nil
}

func (s *Service) reconcileAcceptedUserInput(ctx context.Context, in UserInputRecoveryInput, saved failedUserInputSnapshot, admission sessionruntime.Admission) error {
	return runtimefence.InTransaction(ctx, s.queries, in.BotID, in.SessionID, func(q dbstore.Queries) error {
		if err := in.Reauthorize(ctx); err != nil {
			return err
		}
		if _, err := q.LockSessionForCommitReconciliation(ctx, sqlc.LockSessionForCommitReconciliationParams{SessionID: saved.run.SessionID, BotID: saved.run.BotID}); err != nil {
			return err
		}
		store, ok := q.(failedUserInputRecoveryQueries)
		if !ok {
			return ErrUserInputRecoveryUnavailable
		}
		_, err := store.LockFailedUserInputRecovery(ctx, sqlc.LockFailedUserInputRecoveryParams{OriginalRunID: saved.run.RunID, BotID: saved.run.BotID, SessionID: saved.run.SessionID, OriginalFencingToken: saved.run.FencingToken, OriginalUpdatedAt: saved.run.UpdatedAt, SourceTurnID: saved.source.ID, DecisionID: saved.decision.ID, RecoveryRunID: db.ParseUUIDOrEmpty(admission.RunID), MaxHistoryBytes: s.historyLoadMaxBytes(0)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserInputRecoveryUnavailable
		} else if err != nil {
			return err
		}
		prefix, err := store.ListFailedUserInputRecoveryMessages(ctx, sqlc.ListFailedUserInputRecoveryMessagesParams{BotID: saved.run.BotID, SessionID: saved.run.SessionID, OriginalRunID: saved.run.RunID})
		if err != nil {
			return err
		}
		if len(prefix) == 0 || prefix[0].Role != "assistant" {
			return ErrUserInputRecoveryUnavailable
		}
		exclusive, err := store.FailedUserInputRecoveryTailIsExclusive(ctx, sqlc.FailedUserInputRecoveryTailIsExclusiveParams{AssistantID: prefix[0].ID, SessionID: saved.run.SessionID, OriginalRunID: saved.run.RunID})
		if err != nil {
			return err
		}
		if !exclusive {
			return ErrUserInputRecoveryUnavailable
		}
		if _, err := q.ReplaceHistoryTurn(ctx, sqlc.ReplaceHistoryTurnParams{OldTurnID: db.ParseUUIDOrEmpty(saved.source.ID), SessionID: saved.run.SessionID, ReplacementTurnID: saved.run.TurnID, ReplacementPosition: saved.run.TurnPosition, RequestMessageID: db.ParseUUIDOrEmpty(saved.source.RequestMessageID), AssistantMessageID: prefix[0].ID, SupersededAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}, SupersededReason: pgtype.Text{String: "accepted_user_input_recovery", Valid: true}}); err != nil {
			return err
		}
		var answer any
		if err := json.Unmarshal(saved.decision.ResultJson, &answer); err != nil {
			return err
		}
		content, err := json.Marshal(sdk.ToolMessage(sdk.ToolResultPart{ToolCallID: saved.decision.ToolCallID, ToolName: saved.decision.ToolName, Result: toolexec.OutputFromValue(answer)}))
		if err != nil {
			return err
		}
		metadata, _ := json.Marshal(map[string]string{"recovered_user_input_id": in.DecisionID, "original_run_id": in.OriginalRunID})
		if _, err := store.CreateMessageInHistoryTurnByRequestAndBind(ctx, sqlc.CreateMessageInHistoryTurnByRequestAndBindParams{Role: "tool", SessionID: saved.run.SessionID, BotID: saved.run.BotID, RequestMessageID: db.ParseUUIDOrEmpty(saved.source.RequestMessageID), RunID: db.ParseUUIDOrEmpty(admission.RunID), Content: content, Metadata: metadata, SessionMode: sessionmode.Chat, RuntimeType: "model"}); err != nil {
			return err
		}
		resume := saved.resume
		resume.UserID, resume.ChannelIdentityID, resume.DeadlineAt = in.ActorID, in.ActorID, nil
		resume.TokenClaims = nil
		resume.ChatClaims, err = s.resumeClaims(in.ChatToken)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(resume)
		if err != nil {
			return err
		}
		n, err := q.SaveSessionRunResumeContext(ctx, sqlc.SaveSessionRunResumeContextParams{RunID: db.ParseUUIDOrEmpty(admission.RunID), FencingToken: admission.Handle.FencingToken, ResumeContext: raw})
		if err != nil {
			return err
		}
		if n != 1 {
			return sessionruntime.ErrRunOwnershipLost
		}
		return nil
	})
}

func (s *Service) runRecoveredUserInput(ctx context.Context, in UserInputRecoveryInput, saved failedUserInputSnapshot, admission sessionruntime.Admission) error {
	if err := in.Reauthorize(ctx); err != nil {
		return err
	}
	ctx, req, err := s.prepareWorkspaceRequest(ctx, ChatRequest{BotID: in.BotID, ChatID: in.BotID, ThreadID: in.SessionID, UserID: in.ActorID, SourceChannelIdentityID: in.ActorID, WorkspaceTargetID: saved.decision.WorkspaceTargetID, CurrentChannel: saved.resume.CurrentChannel, ReplyTarget: saved.resume.ReplyTarget, ConversationType: saved.resume.ConversationType})
	if err != nil {
		return err
	}
	cfg, model, _, err := s.buildBaseRunConfig(ctx, baseRunConfigParams{BotID: in.BotID, ChatID: in.BotID, SessionID: in.SessionID, UserID: in.ActorID, ChannelIdentityID: in.ActorID, CurrentPlatform: saved.resume.CurrentChannel, ReplyTarget: saved.resume.ReplyTarget, ConversationType: saved.resume.ConversationType, SessionToken: in.ChatToken, SessionType: sessionmode.Chat, Model: saved.resume.Model, ReasoningEffort: saved.resume.ReasoningEffort})
	if err != nil {
		return err
	}
	cfg.RunID = admission.RunID
	cfg, err = s.prepareContinuationRunConfigWithRequiredInput(ctx, cfg, historyScopeFallbackFromChatRequest(req), compactionSummaryScope(in.BotID, "", in.SessionID, saved.resume.ConversationType, "", saved.resume.ReplyTarget), nil, saved.source.RequestMessageID)
	if err != nil {
		return err
	}
	if err := in.Reauthorize(ctx); err != nil {
		return err
	}
	cfg.LiveToolStream, cfg.CanRequestUserInput = true, s.canDeliverUserInputStream()
	if err := s.sessionManager.ValidateRunOwnership(ctx, admission.Handle); err != nil {
		return err
	}
	req.RunID, req.RunHandle, req.TurnID, req.TurnPosition = admission.RunID, admission.Handle, admission.TurnID, &admission.TurnPosition
	req.ChatID, req.UserMessagePersisted, req.PersistedUserMessageID, req.SkipMemoryExtraction = in.BotID, true, saved.source.RequestMessageID, true
	req.CurrentChannel, req.ReplyTarget, req.ConversationType = saved.resume.CurrentChannel, saved.resume.ReplyTarget, saved.resume.ConversationType
	rc := resolvedContext{runConfig: cfg, model: model}
	committer := s.newAgentStepCommitter(ctx, req, rc)
	timing := newReasoningTimingTracker(nil)
	configureNativeReasoningTiming(&cfg, timing, committer)
	_, err = s.runTriggeredNativeStream(ctx, cfg, req, rc, admission.Handle, committer, timing)
	return err
}
