package application

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/runtimefence"
)

type presenceSQL interface {
	ReadConversationPresenceFacts(context.Context, sqlc.ReadConversationPresenceFactsParams) (sqlc.ReadConversationPresenceFactsRow, error)
	InsertConversationPresence(context.Context, sqlc.InsertConversationPresenceParams) (sqlc.ConversationPresence, error)
	LatestConversationPresence(context.Context, sqlc.LatestConversationPresenceParams) (sqlc.ConversationPresence, error)
	ClaimConversationPresence(context.Context, sqlc.ClaimConversationPresenceParams) (int64, error)
	PublishConversationPresence(context.Context, sqlc.PublishConversationPresenceParams) (int64, error)
	CloseConversationPresenceRun(context.Context, sqlc.CloseConversationPresenceRunParams) (int64, error)
	FinalizeConversationPresenceTemperature(context.Context, sqlc.FinalizeConversationPresenceTemperatureParams) (int64, error)
	InsertConversationPresenceDecision(context.Context, sqlc.InsertConversationPresenceDecisionParams) error
}

type presenceTransactions interface {
	InTx(context.Context, func(dbstore.Queries) error) error
	SupportsTransactions() bool
}

// postgresPresenceClaims uses the existing application PostgreSQL transaction
// owner and runtime fence. It never changes run input/fingerprints or history.
type postgresPresenceClaims struct {
	queries   dbstore.Queries
	handle    sessionruntime.RunHandle
	actor     string
	source    inputsource.Source
	authorize func(context.Context) (bool, error)
}

func (s *postgresPresenceClaims) transaction(ctx context.Context, ticket conversationpresence.Ticket, terminalOK bool, action func(presenceSQL, sqlc.SessionRun) error) error {
	tx, ok := s.queries.(presenceTransactions)
	if !ok || !tx.SupportsTransactions() || s.authorize == nil || ticket.Scope.BotID != s.handle.BotID || ticket.Scope.SessionID != s.handle.SessionID || ticket.Scope.ActorID != s.actor || ticket.Generation != s.handle.Generation || ticket.Fence != s.handle.FencingToken || ticket.RunID != s.handle.RunID {
		return conversationpresence.ErrInvalidContract
	}
	// Closing withdraws feedback under the existing host fence. Requiring a
	// live chat grant here would prevent cleanup precisely when it is revoked.
	// Preparation, claiming and publication still require permission twice.
	if !terminalOK {
		allowed, err := s.authorize(ctx)
		if err != nil {
			return err
		}
		if !allowed {
			return runtimefence.ErrStale
		}
	}
	ctx = runtimefence.WithContext(ctx, runtimefence.Fence{BotID: s.handle.BotID, SessionID: s.handle.SessionID, Token: s.handle.FencingToken})
	runID, err := db.ParseUUID(s.handle.RunID)
	if err != nil {
		return err
	}
	return tx.InTx(ctx, func(q dbstore.Queries) error {
		if err := runtimefence.LockBotForSessionWrite(ctx, q, s.handle.BotID); err != nil {
			return err
		}
		if err := runtimefence.Lock(ctx, q, s.handle.BotID, s.handle.SessionID); err != nil {
			return err
		}
		run, err := q.GetSessionRun(ctx, runID)
		if err != nil {
			return err
		}
		if run.BotID.String() != s.handle.BotID || run.SessionID.String() != s.handle.SessionID || run.FencingToken != s.handle.FencingToken || ticket.Scope.TeamID != "" && ticket.Scope.TeamID != run.TeamID.String() || ticket.InvocationID != "" && ticket.InvocationID != run.InvocationID {
			return runtimefence.ErrStale
		}
		if run.OwnerID.Valid && run.OwnerID.String != "" && run.OwnerID.String != s.handle.OwnerID {
			return runtimefence.ErrStale
		}
		if !terminalOK && (run.State != "running" || run.AbortRequestedAt.Valid || run.OwnerID.String != s.handle.OwnerID) {
			return runtimefence.ErrStale
		}
		if !terminalOK {
			allowed, err := s.authorize(ctx)
			if err != nil {
				return err
			}
			if !allowed {
				return runtimefence.ErrStale
			}
		}
		p, ok := q.(presenceSQL)
		if !ok {
			return conversationpresence.ErrInvalidContract
		}
		return action(p, run)
	})
}

func (s *postgresPresenceClaims) baseTicket() conversationpresence.Ticket {
	return conversationpresence.Ticket{Scope: conversationpresence.Scope{ActorID: s.actor, BotID: s.handle.BotID, SessionID: s.handle.SessionID}, RunID: s.handle.RunID, Generation: s.handle.Generation, Fence: s.handle.FencingToken}
}

func (s *postgresPresenceClaims) prepare(ctx context.Context, reply bool) (conversationpresence.Ticket, conversationpresence.Facts, conversationpresence.Decision, bool, error) {
	ticket := s.baseTicket()
	var facts conversationpresence.Facts
	decision := conversationpresence.Decision{}
	inserted := false
	err := s.transaction(ctx, ticket, false, func(q presenceSQL, run sqlc.SessionRun) error {
		f, err := q.ReadConversationPresenceFacts(ctx, sqlc.ReadConversationPresenceFactsParams{SessionID: run.SessionID, TurnID: run.TurnID, RunID: run.RunID})
		if err != nil {
			return err
		}
		prior, err := q.LatestConversationPresence(ctx, sqlc.LatestConversationPresenceParams{SessionID: run.SessionID, ActorID: s.actor, RunID: run.RunID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		hasPrior := err == nil
		anchor := time.Time{}
		if hasPrior {
			anchor = prior.CreatedAt.Time
		}
		previousInputs, reliableHistory, err := presenceHistoryContinuity(f.PreviousSources, s.handle.BotID, s.handle.SessionID, anchor)
		if err != nil {
			return err
		}
		facts = conversationpresence.Facts{Admitted: true, FirstDirectInput: previousInputs == 0 && !hasPrior, HistoryAvailable: reliableHistory, PreviousSettled: f.PreviousSettled, PendingUserInput: f.PendingInput.Bool, ReplyContinuation: reply}
		decision = conversationpresence.Classify(s.source, s.handle.BotID, s.handle.SessionID, facts)
		episode := run.RunID
		if hasPrior && decision.Temperature != conversationpresence.Cold {
			episode = prior.EpisodeID
		}
		ticket.Scope.TeamID, ticket.InvocationID, ticket.EpisodeID = run.TeamID.String(), run.InvocationID, episode.String()
		_, err = q.InsertConversationPresence(ctx, sqlc.InsertConversationPresenceParams{BotID: run.BotID, SessionID: run.SessionID, RunID: run.RunID, InvocationID: run.InvocationID, ActorID: s.actor, EpisodeID: episode, Generation: s.handle.Generation, LiveGeneration: run.LiveGeneration.String, FencingToken: s.handle.FencingToken, Temperature: string(decision.Temperature), AdmittedAt: run.CreatedAt})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		} // Includes a close-before-prepare tombstone.
		inserted = err == nil
		return err
	})
	return ticket, facts, decision, inserted, err
}

// prepareStep1 is the fast-reply v3 first short transaction: facts + prior
// receipt read, then the receipt insert with temperature 'uncertain' (design
// v3 §6.1: the receipt exists from admission onward; the Jev decision only
// updates it later, and a close-before-prepare tombstone still suppresses the
// insert exactly like the legacy path).
func (s *postgresPresenceClaims) prepareStep1(ctx context.Context, reply bool) (conversationpresence.Ticket, conversationpresence.Facts, time.Time, bool, error) {
	ticket := s.baseTicket()
	var facts conversationpresence.Facts
	var admittedAt time.Time
	hasPrior := false
	inserted := false
	err := s.transaction(ctx, ticket, false, func(q presenceSQL, run sqlc.SessionRun) error {
		admittedAt = run.CreatedAt.Time
		f, err := q.ReadConversationPresenceFacts(ctx, sqlc.ReadConversationPresenceFactsParams{SessionID: run.SessionID, TurnID: run.TurnID, RunID: run.RunID})
		if err != nil {
			return err
		}
		p, err := q.LatestConversationPresence(ctx, sqlc.LatestConversationPresenceParams{SessionID: run.SessionID, ActorID: s.actor, RunID: run.RunID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		hasPrior = err == nil
		anchor := time.Time{}
		if hasPrior {
			anchor = p.CreatedAt.Time
		}
		previousInputs, reliableHistory, err := presenceHistoryContinuity(f.PreviousSources, s.handle.BotID, s.handle.SessionID, anchor)
		if err != nil {
			return err
		}
		facts = conversationpresence.Facts{Admitted: true, FirstDirectInput: previousInputs == 0 && !hasPrior, HistoryAvailable: reliableHistory, PreviousSettled: f.PreviousSettled, PendingUserInput: f.PendingInput.Bool, ReplyContinuation: reply}
		// Episode stays merged with the prior episode until the final decision
		// says cold (design v3 §6.1); step2 re-points it to run_id on cold.
		episode := run.RunID
		if hasPrior {
			episode = p.EpisodeID
		}
		ticket.Scope.TeamID, ticket.InvocationID, ticket.EpisodeID = run.TeamID.String(), run.InvocationID, episode.String()
		_, err = q.InsertConversationPresence(ctx, sqlc.InsertConversationPresenceParams{BotID: run.BotID, SessionID: run.SessionID, RunID: run.RunID, InvocationID: run.InvocationID, ActorID: s.actor, EpisodeID: episode, Generation: s.handle.Generation, LiveGeneration: run.LiveGeneration.String, FencingToken: s.handle.FencingToken, Temperature: string(conversationpresence.Uncertain), AdmittedAt: run.CreatedAt})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		} // Includes a close-before-prepare tombstone.
		inserted = err == nil
		return err
	})
	return ticket, facts, admittedAt, inserted, err
}

// finalizePresenceDecision is prepare-step2 (short transaction, design v3
// §6.1/§6.3): update the receipt decision (uncertain → cold/hot, skipped when
// the mux already closed the row) and insert the decision accounting row with
// outcome NULL for the finish write point.
func (s *postgresPresenceClaims) finalizePresenceDecision(ctx context.Context, ticket conversationpresence.Ticket, result *presenceJevResult, totalMs int64) error {
	detail, err := json.Marshal(result.Detail)
	if err != nil {
		return err
	}
	sessionID, err := db.ParseUUID(ticket.Scope.SessionID)
	if err != nil {
		return err
	}
	runID, err := db.ParseUUID(ticket.RunID)
	if err != nil {
		return err
	}
	return s.transaction(ctx, ticket, false, func(q presenceSQL, run sqlc.SessionRun) error {
		if _, err := q.FinalizeConversationPresenceTemperature(ctx, sqlc.FinalizeConversationPresenceTemperatureParams{Temperature: string(result.Temperature), SessionID: run.SessionID, InvocationID: run.InvocationID}); err != nil {
			return err
		}
		return q.InsertConversationPresenceDecision(ctx, sqlc.InsertConversationPresenceDecisionParams{
			SessionID:      sessionID,
			RunID:          runID,
			InvocationID:   run.InvocationID,
			ActorID:        s.actor,
			FinalMode:      result.Mode,
			Direction:      pgtype.Text{String: result.Direction, Valid: result.Direction != ""},
			Urgency:        pgtype.Int4{Int32: int32(result.Urgency), Valid: result.Urgency >= 1}, //nolint:gosec // urgency is clamped to 1-3 by the engine
			FallbackReason: pgtype.Text{String: result.FallbackReason, Valid: result.FallbackReason != ""},
			Pulls:          int32(len(result.Detail.Pulls)),    //nolint:gosec // bounded by pull_max (4)
			TotalMs:        int32(min(totalMs, math.MaxInt32)), //nolint:gosec // window-bounded elapsed (10s scale)
			Detail:         detail,
		})
	})
}

// recordPresenceOutcome is the fail-open finish write point (design v3
// §6.3): fill the accounting outcome after the coordinator reached a
// terminal state. Errors are logged by the caller, never blocking publish.
func (s *postgresPresenceClaims) recordPresenceOutcome(ctx context.Context, ticket conversationpresence.Ticket, outcome string) error {
	patch, err := json.Marshal(map[string]string{"outcome": outcome})
	if err != nil {
		return err
	}
	sessionID, err := db.ParseUUID(ticket.Scope.SessionID)
	if err != nil {
		return err
	}
	q, ok := s.queries.(interface {
		FinalizeConversationPresenceDecisionOutcome(context.Context, sqlc.FinalizeConversationPresenceDecisionOutcomeParams) (int64, error)
	})
	if !ok {
		return conversationpresence.ErrInvalidContract
	}
	_, err = q.FinalizeConversationPresenceDecisionOutcome(ctx, sqlc.FinalizeConversationPresenceDecisionOutcomeParams{OutcomePatch: patch, SessionID: sessionID, InvocationID: ticket.InvocationID})
	return err
}

func presenceHistoryContinuity(raw []byte, botID, sessionID string, anchor time.Time) (int64, bool, error) {
	var entries []struct {
		Source    json.RawMessage `json:"source"`
		CreatedAt time.Time       `json:"created_at"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return 0, false, err
	}
	var previousInputs int64
	reliable := true
	for _, entry := range entries {
		source := inputsource.FromMetadata(map[string]any{inputsource.MetadataKey: entry.Source}, botID, sessionID)
		switch source.Kind {
		case inputsource.User:
			previousInputs++
		case inputsource.Schedule, inputsource.Result, inputsource.Delegation, inputsource.NativeTask:
			// Ignore only supported machine sources validated by Scoped.
		default:
			// Unknown cannot be silently discarded as a machine input. A later
			// trusted receipt may anchor continuity beyond old unknown history.
			previousInputs++
			if anchor.IsZero() || entry.CreatedAt.After(anchor) {
				reliable = false
			}
		}
	}
	return previousInputs, reliable, nil
}

func (s *postgresPresenceClaims) Claim(ctx context.Context, ticket conversationpresence.Ticket) (bool, error) {
	var n int64
	err := s.transaction(ctx, ticket, false, func(q presenceSQL, run sqlc.SessionRun) error {
		episode, err := db.ParseUUID(ticket.EpisodeID)
		if err != nil {
			return err
		}
		n, err = q.ClaimConversationPresence(ctx, sqlc.ClaimConversationPresenceParams{SessionID: run.SessionID, InvocationID: run.InvocationID, EpisodeID: episode, ActorID: s.actor, Generation: s.handle.Generation, FencingToken: s.handle.FencingToken, OwnerID: pgtype.Text{String: s.handle.OwnerID, Valid: true}})
		return err
	})
	return n == 1, err
}

func (s *postgresPresenceClaims) Publish(ctx context.Context, ticket conversationpresence.Ticket, piece conversationpresence.Piece) (bool, error) {
	if piece.Kind != "conversation_ack" || piece.InputSource != s.source {
		return false, conversationpresence.ErrInvalidContract
	}
	raw, err := json.Marshal(piece)
	if err != nil {
		return false, err
	}
	var n int64
	err = s.transaction(ctx, ticket, false, func(q presenceSQL, run sqlc.SessionRun) error {
		episode, err := db.ParseUUID(ticket.EpisodeID)
		if err != nil {
			return err
		}
		n, err = q.PublishConversationPresence(ctx, sqlc.PublishConversationPresenceParams{Piece: raw, SessionID: run.SessionID, InvocationID: run.InvocationID, EpisodeID: episode, ActorID: s.actor, Generation: s.handle.Generation, FencingToken: s.handle.FencingToken, OwnerID: pgtype.Text{String: s.handle.OwnerID, Valid: true}})
		return err
	})
	return n == 1, err
}

func (s *postgresPresenceClaims) Close(ctx context.Context, ticket conversationpresence.Ticket, reason string) error {
	return s.transaction(ctx, ticket, true, func(q presenceSQL, run sqlc.SessionRun) error {
		_, err := q.CloseConversationPresenceRun(ctx, sqlc.CloseConversationPresenceRunParams{ActorID: s.actor, Generation: s.handle.Generation, FencingToken: s.handle.FencingToken, Reason: pgtype.Text{String: reason, Valid: true}, BotID: run.BotID, SessionID: run.SessionID, RunID: run.RunID})
		return err
	})
}
