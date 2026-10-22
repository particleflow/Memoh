package application

import (
	"context"

	"github.com/felinics/memoh/internal/agent/turn"
)

// SetDelegationScope installs the host-owned durable scope lookup. Runtime and
// prompt code remain unaware of delegation; ordinary sessions are unchanged.
func (s *Service) SetDelegationScope(prepare func(context.Context, ChatRequest) (context.Context, ChatRequest, error)) {
	s.delegationScope = prepare
}

// EnqueueAuthorizedFollowUp accepts a complete host-authorized turn, including
// real attachment references. Identity and authorization belong to the ingress,
// exactly as for EnqueueFollowUp. Acceptance is not execution completion.
func (s *Service) EnqueueAuthorizedFollowUp(ctx context.Context, invocation string, cmd turn.StartTurnCommand) error {
	_, err := s.enqueueFollowUpCommand(ctx, invocation, cmd)
	return err
}
