package application

import (
	"context"

	"github.com/google/uuid"

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
	// The durable turn idempotency key is derived from the queue item ID. Keep
	// that ID stable as well, even if an uncertain delivery outlives queue memory.
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("memoh/host-follow-up/v1\x00"+cmd.BotID+"\x00"+cmd.ThreadID+"\x00"+invocation)).String()
	_, err := s.enqueueFollowUpCommandWithID(ctx, id, invocation, cmd)
	return err
}
