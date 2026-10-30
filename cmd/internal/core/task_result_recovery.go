package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/taskresult"
)

// RecoverResult grants one additional summary by the existing JSON CAS. A
// terminal, unreported previous invocation is required: missing admission or
// an active writer is never guessed safe. Restart reuses the saved new receipt.
func (h *delegationHost) RecoverResult(ctx context.Context, r botdelegation.Record, input botdelegation.RecoveryInput) (botdelegation.RecoveryView, error) {
	b := r.Identity
	for range 8 {
		t, err := h.loadResultTask(ctx, "bot_delegation", r.ID, b.SourceBotID, b.UserID, b.SourceSessionID)
		if errors.Is(err, errResultNotReady) {
			return botdelegation.RecoveryView{}, botdelegation.ErrConflict
		}
		if err != nil {
			return botdelegation.RecoveryView{}, err
		}
		if err := h.recoveryExecutionReady(ctx, r, t); err != nil {
			return botdelegation.RecoveryView{}, err
		}
		if taskresult.EventID(t.state, t.run, t.text) != input.EventID {
			return botdelegation.RecoveryView{}, botdelegation.ErrConflict
		}
		e, ok := t.delivery[input.EventID]
		if !ok || e.Attempt < 1 || e.MessageID != "" || e.State != t.state || e.RunID != t.run || e.Text != t.text {
			return botdelegation.RecoveryView{}, botdelegation.ErrConflict
		}
		resume := false
		for _, audit := range e.Recoveries {
			if audit.FromAttempt == input.ExpectedAttempt && audit.Attempt == e.Attempt && audit.RequestedBy == b.UserID {
				resume = true
			}
		}
		if !resume && (e.Attempt != input.ExpectedAttempt || e.ReportState != "attention") {
			return botdelegation.RecoveryView{}, botdelegation.ErrConflict
		}
		ref := taskresult.Reference{Kind: "bot_delegation", TaskID: r.ID, EventID: input.EventID, Attempt: e.Attempt}
		observed, err := h.store.TaskResultPublication(ctx, sqlc.TaskResultPublicationParams{BotID: nativeTaskUUID(b.SourceBotID), SessionID: nativeTaskUUID(b.SourceSessionID), InvocationID: application.HostFollowUpInvocation(b.SourceBotID, b.SourceSessionID, ref.String()), Receipt: ref.String()})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return botdelegation.RecoveryView{}, err
		}
		if err == nil && observed.MessageID.Valid {
			return botdelegation.RecoveryView{}, botdelegation.ErrConflict
		}
		if resume {
			// Same authorization after uncertain enqueue or process loss; the
			// existing ingress deduplicates its exact invocation/queue identity.
			if err := h.reconcileResult(ctx, t.kind, t.id, t.bot, t.user, t.session); err != nil {
				return botdelegation.RecoveryView{}, err
			}
			current, err := h.loadResultTask(ctx, t.kind, t.id, t.bot, t.user, t.session)
			if err != nil {
				return botdelegation.RecoveryView{}, err
			}
			return botdelegation.RecoveryView{EventID: input.EventID, Event: current.delivery[input.EventID]}, nil
		}
		if err != nil || !taskresult.Terminal(observed.State) {
			return botdelegation.RecoveryView{}, botdelegation.ErrConflict
		}
		e.Recoveries = append(e.Recoveries, taskresult.Recovery{FromAttempt: e.Attempt, Attempt: e.Attempt + 1, RequestedBy: b.UserID, RequestedAt: time.Now().UTC().Format(time.RFC3339Nano), PreviousRunID: observed.RunID.String(), PreviousRunState: observed.State})
		e.Attempt++
		e.ReportState = "pending"
		t.delivery[input.EventID] = e
		next, err := json.Marshal(t.delivery)
		if err != nil {
			return botdelegation.RecoveryView{}, err
		}
		if err = t.save(ctx, t.raw, next); errors.Is(err, pgx.ErrNoRows) {
			continue
		} else if err != nil {
			return botdelegation.RecoveryView{}, err
		}
		// Continue through a fresh authorization read after the durable grant.
	}
	return botdelegation.RecoveryView{}, botdelegation.ErrConflict
}

// Use original invocation facts, including the admission-before-run-binding
// crash window. A saved failure/unknown without execution admission is a
// reportable outcome, not permission to dispatch that execution again.
func (h *delegationHost) recoveryExecutionReady(ctx context.Context, r botdelegation.Record, t resultTask) error {
	missing := func() error {
		if (t.state == "unknown" || t.state == "failed") && r.State == t.state {
			return nil
		}
		return botdelegation.ErrConflict
	}
	if r.Identity.TargetSessionID == "" {
		return missing()
	}
	worker, err := h.store.GetSessionRunByInvocation(ctx, sqlc.GetSessionRunByInvocationParams{SessionID: nativeTaskUUID(r.Identity.TargetSessionID), InvocationID: "bot-delegation:" + r.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return missing()
	}
	if err != nil {
		return err
	}
	if worker.TeamID.String() != r.Identity.TeamID || worker.BotID.String() != r.Identity.TargetBotID || worker.SessionID.String() != r.Identity.TargetSessionID || worker.RunID.String() != t.run || !taskresult.Terminal(worker.State) {
		return botdelegation.ErrConflict
	}
	// Retain the existing ownership/history/fencing check for an observed run.
	visible, err := h.service.Ledger.TargetRun(ctx, r)
	if err != nil {
		return err
	}
	if visible == nil || visible.ID != worker.RunID.String() {
		return botdelegation.ErrConflict
	}
	sameOutcome := worker.State == t.state || worker.State == "lost" && t.state == "unknown" || worker.State == "aborted" && t.state == "failed"
	if !sameOutcome {
		return botdelegation.ErrConflict
	}
	return nil
}
