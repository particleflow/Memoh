package botdelegation

import (
	"context"

	"github.com/felinics/memoh/internal/taskresult"
)

// ExpectedAttempt is the compare-and-set token. Repeating the same request can
// resume that recovery, but cannot grant another attempt after it fails.
type RecoveryInput struct {
	EventID         string `json:"event_id" validate:"required"`
	ExpectedAttempt int    `json:"expected_attempt" validate:"required"`
}

type RecoveryView struct {
	EventID string           `json:"event_id"`
	Event   taskresult.Event `json:"event"`
}

type savedResultRecoveryHost interface {
	RecoverResult(context.Context, Record, RecoveryInput) (RecoveryView, error)
}

// RecoverDelivery only re-reports an existing, completed specialist result.
// It does not call Submit, CreateSession, Run or reset any execution state.
func (s *Service) RecoverDelivery(ctx context.Context, bot, user, session, id string, input RecoveryInput) (RecoveryView, error) {
	ref := taskresult.Reference{Kind: "bot_delegation", TaskID: id, EventID: input.EventID, Attempt: input.ExpectedAttempt}
	if _, err := taskresult.Parse(ref.String()); err != nil || input.ExpectedAttempt < taskresult.MaxAttempts || input.ExpectedAttempt == 2147483647 {
		return RecoveryView{}, ErrInput
	}
	r, err := s.ReadRecord(ctx, bot, user, session, id)
	if err != nil {
		return RecoveryView{}, err
	}
	if r.Payload.ResultDeliveryVersion != 1 || r.State != "completed" {
		return RecoveryView{}, ErrConflict
	}
	h, ok := s.Host.(savedResultRecoveryHost)
	if !ok {
		return RecoveryView{}, ErrDenied
	}
	return h.RecoverResult(ctx, r, input)
}
