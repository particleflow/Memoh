package computertask

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

// PiContinuity separates historical checks from current availability. Query and
// recover never assert the original history still exists merely from a receipt.
type PiContinuity struct {
	Status         string                     `json:"status"`
	Reason         string                     `json:"reason"`
	LastInvocation *agenvo.BatchHistoryReport `json:"last_invocation,omitempty"`
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
func (s *Service) batchHistoryExpectation(ctx context.Context, r dbstore.ComputerOperationRecord, p agenvo.BatchProgress) (*agenvo.BatchHistoryExpectation, error) {
	h := &agenvo.BatchHistoryExpectation{Version: agenvo.BatchHistoryVersion, Mode: "unconfirmed"}
	reader, ok := s.store.(dbstore.ComputerPiHistoryStore)
	if !ok {
		return h, agenvo.ErrUnknown
	}
	prior, err := reader.PreviousComputerPiOperation(ctx, r, p.PiSessionID)
	if errors.Is(err, db.ErrNotFound) {
		h.Mode = "first"
		return h, nil
	}
	if err != nil {
		return h, agenvo.ErrUnknown
	}
	var old Details
	if prior.BotID != r.BotID || prior.SessionID != r.SessionID || prior.UserID != r.UserID || prior.TargetID != r.TargetID || prior.TargetFingerprint != r.TargetFingerprint || json.Unmarshal(prior.Details, &old) != nil || old.Batch == nil {
		return h, nil
	}
	b := old.Batch
	if b.Progress.PiSessionID != p.PiSessionID || b.Progress.History == nil || b.Receipt == nil || b.Receipt.Validate(b.Progress) != nil {
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
