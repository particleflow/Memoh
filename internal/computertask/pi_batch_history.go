package computertask

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/felinics/memoh/internal/computertarget"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

// PiContinuity separates historical checks from current availability. Query and
// recover never assert the original history still exists merely from a receipt.
type PiContinuity struct {
	Status         string                             `json:"status"`
	Reason         string                             `json:"reason"`
	LastInvocation *computertarget.BatchHistoryReport `json:"last_invocation,omitempty"`
}

func batchContinuity(batch *BatchDetails) *PiContinuity {
	if batch == nil {
		return nil
	}
	out := &PiContinuity{Status: "unconfirmed", Reason: "history_not_rechecked"}
	switch {
	case batch.Progress.History == nil:
		out.Reason = "legacy_evidence_missing"
	case batch.Progress.History.Mode == "unconfirmed":
		out.Reason = "prior_evidence_unconfirmed"
	case batch.Receipt != nil && batch.Receipt.Validate(batch.Progress) == nil:
		out.LastInvocation = batch.Receipt.History
	}
	return out
}

// The caller owns the DB unsettled slot before reading, and persists the chosen
// expectation before any workspace/tab/pane mutation. No failed read means new.
func (s *Service) batchHistoryExpectation(ctx context.Context, r dbstore.ComputerOperationRecord, p computertarget.BatchProgress) (*computertarget.BatchHistoryExpectation, error) {
	h := &computertarget.BatchHistoryExpectation{Version: computertarget.BatchHistoryVersion, Mode: "unconfirmed"}
	reader, ok := s.store.(dbstore.ComputerPiHistoryStore)
	if !ok {
		return h, computertarget.ErrUnknown
	}
	prior, err := reader.PreviousComputerPiOperation(ctx, r, p.PiSessionID)
	if errors.Is(err, db.ErrNotFound) {
		h.Mode = "first"
		return h, nil
	}
	if err != nil {
		return h, computertarget.ErrUnknown
	}
	var old Details
	if prior.BotID != r.BotID || prior.SessionID != r.SessionID || prior.UserID != r.UserID || prior.TargetID != r.TargetID || json.Unmarshal(prior.Details, &old) != nil || old.Batch == nil {
		return h, nil
	}
	b := old.Batch
	// Session-level evidence shared by both relaxations below: "this one
	// operation never started" is not "this Pi session never started". A proven
	// start may have had external side effects even after its history files
	// were lost, so one started invocation anywhere in the Pi session keeps the
	// session unconfirmed. ErrNotFound is the only proof the gate relaxes on;
	// any other read error also fails closed.
	neverStarted := false
	if _, startedErr := reader.LatestStartedPiOperation(ctx, r, p.PiSessionID); errors.Is(startedErr, db.ErrNotFound) {
		neverStarted = true
	}
	if b.Receipt == nil {
		// An admission-blocked prior (incident E3: rejected at the continuity
		// gate before any native mutation — the same pre-dispatch shape
		// resumeInputPreparation eligibility recognizes) proves nothing was
		// ever dispatched for that operation. It must not strand every later
		// request at pi_continuity_unconfirmed when the whole Pi session is
		// proven start-free; a first invocation is the accurate expectation.
		// Anything that might have dispatched keeps its receipt and stays
		// guarded below.
		if neverStarted && prior.State == "not_started" && b.Progress.Phase == "" && b.Progress.WorkspaceID == "" && b.Progress.PaneID == "" && b.Progress.RequestID == "" && b.Result == nil {
			h.Mode = "first"
		}
		return h, nil
	}
	if prior.TargetFingerprint != r.TargetFingerprint {
		// A target profile change (incident E3: adding the explicit agent PATH)
		// must not strand the conversation. When the latest prior operation
		// proves Pi never started and no earlier operation of this Pi session
		// ever started either, the session holds no live history, and a first
		// invocation is the accurate expectation. Anything less proven stays
		// unconfirmed.
		if neverStarted && b.Progress.PiSessionID == p.PiSessionID && b.Receipt.Validate(b.Progress) == nil && !b.Receipt.Started {
			h.Mode = "first"
		}
		return h, nil
	}
	if b.Progress.PiSessionID != p.PiSessionID || b.Progress.History == nil || b.Receipt.Validate(b.Progress) != nil {
		return h, nil
	}
	if b.Receipt.History.Evidence.Valid() {
		h.Mode = "resume"
		evidence := *b.Receipt.History.Evidence
		h.Evidence = &evidence
	} else if !b.Receipt.Started {
		// Proven no invocation preserves the prior expectation; this does not turn
		// an unproven legacy record into a first invocation or heal missing history.
		*h = *b.Progress.History
	}
	return h, nil
}
