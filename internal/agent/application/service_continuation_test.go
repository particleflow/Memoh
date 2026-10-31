package application

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	contextfrag "github.com/felinics/memoh/internal/agent/context/fragment"
	historyfrag "github.com/felinics/memoh/internal/agent/context/history"
	userinput "github.com/felinics/memoh/internal/agent/decision/input"
	"github.com/felinics/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	sessionpkg "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type continuationTurnRequestResolverService struct {
	messagepkg.Service
	requestMessageID string
	err              error
}

func (s *continuationTurnRequestResolverService) GetVisibleHistoryTurnRequestMessageIDByTurn(context.Context, string, string) (string, error) {
	return s.requestMessageID, s.err
}

type continuationRecoveryQueries struct {
	dbstore.Queries
	run sqlc.SessionRun
}

func (q continuationRecoveryQueries) GetSessionRun(context.Context, pgtype.UUID) (sqlc.SessionRun, error) {
	return q.run, nil
}

func TestContinuationMissingTurnNeverFallsBackToLatestInput(t *testing.T) {
	for _, scenario := range []string{"ordinary_run", "wrong_scope", "wrong_fence"} {
		t.Run(scenario, func(t *testing.T) {
			botID, sessionID, runID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
			row := sqlc.SessionRun{BotID: db.ParseUUIDOrEmpty(botID), SessionID: db.ParseUUIDOrEmpty(sessionID), FencingToken: 9, InputJson: []byte(`{"kind":"accepted_user_input_recovery","request_message_id":"dddddddd-dddd-4ddd-8ddd-dddddddddddd"}`)}
			switch scenario {
			case "ordinary_run":
				row.InputJson = []byte(`{"kind":"message"}`)
			case "wrong_scope":
				row.SessionID = db.ParseUUIDOrEmpty(runID)
			case "wrong_fence":
				row.FencingToken++
			}
			// Any accidental latest-message or arbitrary-message lookup invokes
			// the nil embedded service and fails this test instead of succeeding.
			service := &Service{queries: continuationRecoveryQueries{run: row}, messageService: &continuationTurnRequestResolverService{err: pgx.ErrNoRows}}
			if _, err := service.continuationTurnRequestMessageID(t.Context(), sessionID, sessionruntime.RunHandle{BotID: botID, SessionID: sessionID, RunID: runID, TurnID: runID, FencingToken: 9}); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("missing original turn was replaced: %v", err)
			}
		})
	}
}

func TestContinuationTurnRequestMessageIDUsesAdmittedTurn(t *testing.T) {
	t.Parallel()

	service := &Service{messageService: &continuationTurnRequestResolverService{
		requestMessageID: "request-message-id",
	}}
	got, err := service.continuationTurnRequestMessageID(
		context.Background(),
		"session-id",
		sessionruntime.RunHandle{TurnID: "turn-id"},
	)
	if err != nil {
		t.Fatalf("continuationTurnRequestMessageID() error = %v", err)
	}
	if got != "request-message-id" {
		t.Fatalf("continuation request message ID = %q, want %q", got, "request-message-id")
	}
}

func TestPrepareContinuationRunConfigReplacesStaleContextAndSetsCapabilities(t *testing.T) {
	t.Parallel()

	resolver := &Service{userInput: &userinput.Service{}}
	eventCh := make(chan WSStreamEvent)
	staleIndex := 0
	staleMemoryIndex := 0
	base := native.RunConfig{
		Query:                          "stale query",
		Messages:                       []sdk.Message{sdk.UserMessage("stale context")},
		ContextCurrentUserMessageIndex: &staleIndex,
		ContextMemoryMessageIndex:      &staleMemoryIndex,
		ContextSourceFrags: []contextfrag.ContextFrag{{
			ID:   "stale-source-fragment",
			Kind: contextfrag.KindConversationEvent,
		}},
		ContextFrags: []contextfrag.ContextFrag{{
			ID:   "stale-fragment",
			Kind: contextfrag.KindConversationEvent,
		}},
	}

	got, err := resolver.prepareContinuationRunConfig(
		context.Background(),
		base,
		historyfrag.ScopeFallback{},
		contextfrag.Scope{},
		eventCh,
	)
	if err != nil {
		t.Fatalf("prepareContinuationRunConfig() error = %v", err)
	}
	if got.Query != "" || len(got.Messages) != 0 {
		t.Fatalf("continuation retained stale context: %#v", got)
	}
	for _, frag := range got.ContextFrags {
		if frag.ID == "stale-fragment" {
			t.Fatalf("continuation retained stale fragment: %#v", frag)
		}
	}
	if got.ContextCurrentUserMessageIndex != nil {
		t.Fatalf("continuation retained stale current-user index: %#v", got.ContextCurrentUserMessageIndex)
	}
	if got.ContextMemoryMessageIndex != nil {
		t.Fatalf("continuation retained stale memory index: %#v", got.ContextMemoryMessageIndex)
	}
	if got.ContextTrimmableMessages != len(got.Messages) || len(got.ContextHistoryTokenEstimates) != len(got.Messages) {
		t.Fatalf("budget signals = trimmable:%d estimates:%d messages:%d",
			got.ContextTrimmableMessages, len(got.ContextHistoryTokenEstimates), len(got.Messages))
	}
	for _, frag := range got.ContextSourceFrags {
		if frag.ID == "stale-source-fragment" {
			t.Fatalf("continuation retained stale source fragment: %#v", frag)
		}
	}
	if !got.LiveToolStream || !got.CanRequestUserInput {
		t.Fatalf("continuation capabilities = live:%v input:%v, want true/true", got.LiveToolStream, got.CanRequestUserInput)
	}
}

func TestPrepareContinuationRunConfigPropagatesArtifactProjectionFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("artifact projection unavailable")
	resolver := &Service{queries: &recordingCompactionLogQueries{listErr: sentinel}}
	got, err := resolver.prepareContinuationRunConfig(
		context.Background(),
		native.RunConfig{Query: "must not survive"},
		historyfrag.ScopeFallback{},
		contextfrag.Scope{SessionID: "00000000-0000-0000-0000-00000000f401"},
		nil,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("prepareContinuationRunConfig() error = %v, want %v", err, sentinel)
	}
	if got.Query != "" || len(got.Messages) != 0 || len(got.ContextFrags) != 0 {
		t.Fatalf("failed continuation returned partial config: %#v", got)
	}
}

func TestPendingRuntimeDecisionsFailsClosedOnRuntimeLookupFailure(t *testing.T) {
	sentinel := errors.New("session unavailable")
	service := &Service{queries: &abortLifecycleQueries{pending: true}, sessionService: &fakeBackgroundSessionService{getFn: func(context.Context, string) (sessionpkg.Thread, error) { return sessionpkg.Thread{}, sentinel }}}
	got, err := service.PendingRuntimeDecisions(t.Context(), "00000000-0000-0000-0000-000000000001")
	if !errors.Is(err, sentinel) || got != nil {
		t.Fatalf("runtime lookup error treated as native: %v %v", got, err)
	}
}
