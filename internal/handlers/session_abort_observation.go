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
	CompleteSessionRunAbortControl(context.Context, sqlc.CompleteSessionRunAbortControlParams) (int64, error)
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
func (h *LocalChannelHandler) observeAbortControl(ctx context.Context, botID, sessionID, runID, controlID, reason string) func(bool, error, string) {
	if h.abortObservations == nil {
		return nil
	}
	var bot, session, run pgtype.UUID
	if bot.Scan(botID) != nil || session.Scan(sessionID) != nil || run.Scan(runID) != nil {
		return nil
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
	return func(applied bool, resultErr error, serverSource string) {
		if serverSource != "agent_service" && serverSource != "runtime_controller" {
			return
		}
		completion := sqlc.CompleteSessionRunAbortControlParams{
			BotID: args.BotID, SessionID: args.SessionID, RunID: args.RunID,
			RequestHash: args.RequestHash, ReceivedAt: args.ReceivedAt, ClientReason: args.ClientReason,
			CompletedAt:  pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
			Applied:      pgtype.Bool{Bool: applied, Valid: true},
			ErrorClass:   pgtype.Text{String: abortOutcomeErrorClass(resultErr), Valid: true},
			ServerSource: pgtype.Text{String: serverSource, Valid: true},
		}
		go func() {
			observationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			if _, err := h.abortObservations.CompleteSessionRunAbortControl(observationCtx, completion); err != nil && h.logger != nil {
				h.logger.WarnContext(observationCtx, "record abort control outcome failed", slog.String("run_id", runID))
			}
		}()
	}
}

// Classification is server-generated and bounded; raw errors never enter the audit.
func abortOutcomeErrorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	default:
		return "control_error"
	}
}

type sessionAbortControlResponse struct {
	ReceivedAt time.Time `json:"received_at"`
	// Source is assigned by the receiving server, not inferred from a user agent.
	Source string `json:"source"`
	// ClientReason is a client-reported enum, not proof of a human action.
	ClientReason string `json:"client_reason"`
	// CompletedAt is a stored server return observation, not the run finish time.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// Applied is the observed control return, not proof of a unique abort cause.
	Applied    *bool  `json:"applied,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
	// ServerSource names the server branch; it is not an authenticated actor.
	ServerSource string `json:"server_source,omitempty"`
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
	out := &sessionAbortControlResponse{
		ReceivedAt:   row.ReceivedAt.Time,
		Source:       source,
		ClientReason: safeAbortClientReason(row.ClientReason),
	}
	if row.CompletedAt.Valid && row.Applied.Valid && row.ErrorClass.Valid && row.ServerSource.Valid {
		switch row.ErrorClass.String {
		case "none", "canceled", "deadline_exceeded", "control_error":
			out.ErrorClass = row.ErrorClass.String
		}
		switch row.ServerSource.String {
		case "agent_service", "runtime_controller":
			out.ServerSource = row.ServerSource.String
		}
		if out.ErrorClass != "" && out.ServerSource != "" {
			at := row.CompletedAt.Time
			applied := row.Applied.Bool
			out.CompletedAt = &at
			out.Applied = &applied
		} else {
			out.ErrorClass = ""
			out.ServerSource = ""
		}
	}
	return out, nil
}
