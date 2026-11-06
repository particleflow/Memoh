package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	contextfrag "github.com/felinics/memoh/internal/agent/context/fragment"
	historyfrag "github.com/felinics/memoh/internal/agent/context/history"
	"github.com/felinics/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/inputsource"
)

type continuationTurnRequestMessageResolver interface {
	GetVisibleHistoryTurnRequestMessageIDByTurn(context.Context, string, string) (string, error)
}

func (s *Service) continuationTurnRequestMessageID(ctx context.Context, sessionID string, runHandle sessionruntime.RunHandle) (string, error) {
	turnID := strings.TrimSpace(runHandle.TurnID)
	if turnID == "" || s == nil || s.messageService == nil {
		return "", nil
	}
	resolver, ok := s.messageService.(continuationTurnRequestMessageResolver)
	if !ok {
		return "", nil
	}
	id, err := resolver.GetVisibleHistoryTurnRequestMessageIDByTurn(ctx, sessionID, turnID)
	if !errors.Is(err, pgx.ErrNoRows) || s.queries == nil {
		return id, err
	}
	if _, parseErr := db.ParseUUID(runHandle.RunID); parseErr != nil || runHandle.FencingToken <= 0 {
		return "", err
	}
	// An explicitly recovered answer continues an existing history turn under
	// a new run. Only the server-authored recovery admission can name that
	// anchor; never fall back to the latest input in the conversation.
	run, readErr := s.queries.GetSessionRun(ctx, db.ParseUUIDOrEmpty(runHandle.RunID))
	if readErr != nil {
		return "", readErr
	}
	var saved acceptedUserInputRecovery
	if json.Unmarshal(run.InputJson, &saved) != nil || saved.Kind != acceptedUserInputRecoveryKind ||
		run.SessionID.String() != sessionID || run.BotID.String() != runHandle.BotID ||
		run.FencingToken != runHandle.FencingToken || saved.RequestMessageID == "" {
		return "", err
	}
	turn, readErr := s.messageService.GetVisibleTurnByMessage(ctx, sessionID, saved.RequestMessageID)
	if readErr != nil {
		return "", readErr
	}
	if turn.BotID != runHandle.BotID || turn.RequestMessageID != saved.RequestMessageID {
		return "", ErrUserInputRecoveryUnavailable
	}
	return turn.RequestMessageID, nil
}

func (s *Service) prepareContinuationRunConfig(
	ctx context.Context,
	base native.RunConfig,
	fallback historyfrag.ScopeFallback,
	summaryScope contextfrag.Scope,
	eventCh chan<- WSStreamEvent,
) (native.RunConfig, error) {
	return s.prepareContinuationRunConfigWithRequiredInput(ctx, base, fallback, summaryScope, eventCh, "")
}

func (s *Service) prepareContinuationRunConfigWithRequiredInput(
	ctx context.Context,
	base native.RunConfig,
	fallback historyfrag.ScopeFallback,
	summaryScope contextfrag.Scope,
	eventCh chan<- WSStreamEvent,
	requestMessageID string,
) (native.RunConfig, error) {
	loaded, err := s.loadHistoryRecords(ctx, fallback, summaryScope.SessionID, defaultMaxContextMinutes, 0)
	if err != nil {
		return native.RunConfig{}, err
	}
	loaded = pruneHistoryForGateway(loaded)
	loaded, err = s.ensureRequiredHistoryMessage(ctx, loaded, ChatRequest{BotID: summaryScope.BotID, ChatID: fallback.ChatID, ThreadID: summaryScope.SessionID, RequiredHistoryMessageID: requestMessageID, ConversationType: fallback.ConversationType, ReplyTarget: fallback.ReplyTarget})
	if err != nil {
		return native.RunConfig{}, err
	}
	loaded, err = s.replaceCompactedMessages(ctx, summaryScope.SessionID, summaryScope, loaded, compactionArtifactBoundary{})
	if err != nil {
		return native.RunConfig{}, err
	}
	loaded = projectInterruptedHistoryReasoning(loaded)
	messages, retained, _ := trimMessagesAndRecordsByTokens(s.logger, loaded, 0)
	messages = sanitizeMessages(messages)
	historyEstimates := make([]int, len(messages))
	for i := range messages {
		historyEstimates[i] = estimateMessageTokens(messages[i])
	}
	base.ContextHistoryTokenEstimates = historyEstimates
	base.ContextTrimmableMessages = len(messages)

	base.ContextFrags = historyContextFragsForMessages(messages, retained)
	// Close any tool call left open by an interrupted turn before the transcript
	// reaches providers that enforce strict assistant-tool adjacency. A process
	// restart can orphan a deferred ask_user / tool-approval call while a later
	// request still completes normally; repairing here (not in ContextFrags)
	// keeps the fragments faithful to history while the outgoing messages stay
	// provider-valid. Applies to every continuation path that resumes after a
	// deferred tool call.
	base.Messages = modelMessagesToSDKMessages(repairToolCallClosures(nonNilModelMessages(messages), syntheticToolClosureError))
	base.ContextCurrentUserMessageIndex = nil
	base.ContextMemoryMessageIndex = nil
	base.Query = ""
	base.LiveToolStream = eventCh != nil
	base.CanRequestUserInput = s.canDeliverUserInputWS(eventCh)
	source := inputsource.Unattributed(summaryScope.BotID, summaryScope.SessionID)
	for _, record := range retained {
		if requestMessageID != "" && record.DBMessageID == requestMessageID {
			source = inputsource.FromMetadata(record.Metadata, summaryScope.BotID, summaryScope.SessionID)
			break
		}
	}
	return s.prepareRunConfig(ctx, base, ChatRequest{BotID: summaryScope.BotID, ThreadID: summaryScope.SessionID, InputSource: &source}), nil
}
