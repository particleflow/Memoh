package computertask

import (
	"context"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

// ResumeNative uses persisted run attribution, like FollowupNative. A terminal
// original run needs no new model turn: the authenticated host owns this action.
// Only the explicit bounded continuation is reachable; it cannot claim new work.
func (s *Source) ResumeNative(ctx context.Context, b toolcontext.ComputerBinding, id string, in Submit) (Result, error) {
	trusted, ok := toolcontext.HostComputerBinding(ctx)
	if !ok || trusted != b || id == "" || in.ResumeOperationID != "" {
		return Result{}, ErrDenied
	}
	r, _, err := s.service.load(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	if r.RunID == "" {
		return Result{}, agenvo.ErrUnsupported
	}
	if b.RunID != "" && b.RunID != r.RunID {
		return Result{}, ErrDenied
	}
	b.RunID = r.RunID
	in.ResumeOperationID = id
	return s.service.submit(ctx, b, in)
}
