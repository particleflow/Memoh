package application

import (
	"context"
	"encoding/json"
	"errors"

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

func (s *postgresPresenceClaims) prepare(ctx context.Context, policy conversationpresence.ContinuityPolicy, reply bool) (conversationpresence.Ticket, conversationpresence.Facts, bool, error) {
	ticket := s.baseTicket()
	var facts conversationpresence.Facts
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
		last := f.LastDirectInput.Time
		if hasPrior && prior.CreatedAt.Time.After(last) {
			last = prior.CreatedAt.Time
		}
		now := run.CreatedAt.Time
		facts = conversationpresence.Facts{Admitted: true, FirstDirectInput: f.PreviousInputs == 0 && !hasPrior, HistoryAvailable: true, PreviousSettled: f.PreviousSettled, PendingUserInput: f.PendingInput.Bool, ReplyContinuation: reply, LastDirectInput: last, Now: now}
		facts.RecentExchange = !last.IsZero() && (policy.IdleAfter <= 0 || now.Sub(last) < policy.IdleAfter)
		decision := conversationpresence.Classify(s.source, s.handle.BotID, s.handle.SessionID, facts, policy)
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
	return ticket, facts, inserted, err
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
