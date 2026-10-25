package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

type sessionAbortObservationStore interface {
	SessionRunMatchesAbortScope(context.Context, sqlc.SessionRunMatchesAbortScopeParams) (bool, error)
	RecordSessionRunAbortControl(context.Context, sqlc.RecordSessionRunAbortControlParams) (int64, error)
	GetSessionRunAbortControl(context.Context, sqlc.GetSessionRunAbortControlParams) (sqlc.GetSessionRunAbortControlRow, error)
}

// The runtime records intent before routing. Validate both the readable session
// and its exact run at the transport boundary so a mismatched ID cannot stamp
// another run's intent even when routing would eventually reject it.
func (h *LocalChannelHandler) authorizeAbortRun(ctx context.Context, botID, sessionID, runID string) error {
	if h.abortObservations == nil {
		return apperror.New(apperror.CodeInternal, nil)
	}
	var bot, session, run pgtype.UUID
	if bot.Scan(botID) != nil || session.Scan(sessionID) != nil || run.Scan(runID) != nil {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	matched, err := h.abortObservations.SessionRunMatchesAbortScope(ctx, sqlc.SessionRunMatchesAbortScopeParams{BotID: bot, SessionID: session, RunID: run})
	if err != nil {
		return apperror.Wrap(apperror.CodeInternal, err, nil)
	}
	if !matched {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	return nil
}

// SetAbortObservationStore associates observations with the existing run. Failures
// must never prevent or change the result of an existing cancel control.
func (h *LocalChannelHandler) SetAbortObservationStore(store sessionAbortObservationStore) {
	h.abortObservations = store
}

func (h *SessionHandler) SetAbortObservationStore(store sessionAbortObservationStore) {
	h.abortObservations = store
}

// ClientReason is an untrusted client declaration reduced to a fixed enum,
// never an actor identity or a causal explanation supplied by the server.
func safeAbortClientReason(reason string) string {
	switch reason {
	case "user_stop", "bot_switch", "auth_reset", "workdir_mismatch", "client_cleanup":
		return reason
	default:
		return "unspecified"
	}
}

//nolint:contextcheck // observation must not delay cancellation or depend on socket lifetime.
func (h *LocalChannelHandler) observeAbortControl(ctx context.Context, botID, sessionID, runID, controlID, reason string) {
	if h.abortObservations == nil {
		return
	}
	var bot, session, run pgtype.UUID
	if bot.Scan(botID) != nil || session.Scan(sessionID) != nil || run.Scan(runID) != nil {
		return
	}
	// The identifier is for deduplication only; arbitrary client strings must
	// not become public metadata or logs. Legacy empty IDs share one observation.
	digest := sha256.Sum256([]byte(controlID))
	args := sqlc.RecordSessionRunAbortControlParams{
		BotID: bot, SessionID: session, RunID: run,
		ClientReason: safeAbortClientReason(reason),
		RequestHash:  hex.EncodeToString(digest[:]),
		ReceivedAt:   pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	// Best effort, off the cancellation path. A crash or database failure may
	// leave this absent; never turn that absence into a claim about the actor.
	go func() {
		observationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if _, err := h.abortObservations.RecordSessionRunAbortControl(observationCtx, args); err != nil && h.logger != nil {
			h.logger.WarnContext(observationCtx, "record abort control observation failed", slog.String("run_id", runID))
		}
	}()
}

type sessionAbortControlResponse struct {
	ReceivedAt time.Time `json:"received_at"`
	// Source is assigned by the receiving server, not inferred from a user agent.
	Source string `json:"source"`
	// ClientReason is a client-reported enum, not proof of a human action.
	ClientReason string `json:"client_reason"`
}

func (h *SessionHandler) readAbortControl(ctx context.Context, botID, sessionID, runID string) (*sessionAbortControlResponse, error) {
	if h.abortObservations == nil {
		return nil, nil
	}
	var bot, session, run pgtype.UUID
	if err := bot.Scan(botID); err != nil {
		return nil, err
	}
	if err := session.Scan(sessionID); err != nil {
		return nil, err
	}
	if err := run.Scan(runID); err != nil {
		return nil, err
	}
	row, err := h.abortObservations.GetSessionRunAbortControl(ctx, sqlc.GetSessionRunAbortControlParams{BotID: bot, SessionID: session, RunID: run})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !row.ReceivedAt.Valid {
		return nil, nil
	}
	source := "unknown"
	if row.Source == "websocket" {
		source = "websocket"
	}
	return &sessionAbortControlResponse{
		ReceivedAt:   row.ReceivedAt.Time,
		Source:       source,
		ClientReason: safeAbortClientReason(row.ClientReason),
	}, nil
}
