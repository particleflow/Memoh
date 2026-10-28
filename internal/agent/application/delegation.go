package application

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/turn"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
)

func botTaskResultMetadata(req ChatRequest) map[string]any {
	// The existing source marker lets a0 history readers hide the input too.
	// The subtype distinguishes a new result turn from mid-turn feedback,
	// which must instead bind to an already-persisted user request.
	return map[string]any{messagepkg.MessageSourceMetadataKey: messagepkg.MessageSourceInternalFeedback, messagepkg.InternalInputKindMetadataKey: messagepkg.InternalInputKindBotTaskResult, "delegation_id": req.DelegationResultID}
}

// SetDelegationScope installs the host-owned durable scope lookup. Runtime and
// prompt code remain unaware of delegation; ordinary sessions are unchanged.
func (s *Service) SetDelegationScope(prepare func(context.Context, ChatRequest) (context.Context, ChatRequest, error)) {
	s.delegationScope = prepare
}

type delegationResultAssetsKey struct{}

type delegationResultAssets struct {
	bot, thread, delegation string
	refs                    []turn.OutboundAssetRef
}

// WithDelegationResultAssets is for the host's live-authorized ledger projection,
// never client/model attachment metadata. Rebuilding it at turn admission also
// covers queued delivery and process restarts without granting a media alias.
func WithDelegationResultAssets(ctx context.Context, bot, thread, delegation string, refs []turn.OutboundAssetRef) context.Context {
	return context.WithValue(ctx, delegationResultAssetsKey{}, delegationResultAssets{bot, thread, delegation, append([]turn.OutboundAssetRef(nil), refs...)})
}

func authorizedDelegationResultAssets(ctx context.Context, cmd turn.StartTurnCommand) []turn.OutboundAssetRef {
	assets, ok := ctx.Value(delegationResultAssetsKey{}).(delegationResultAssets)
	if !ok || cmd.DelegationResultID == "" || assets.bot != cmd.BotID || assets.thread != cmd.ThreadID || assets.delegation != cmd.DelegationResultID {
		return nil
	}
	return append([]turn.OutboundAssetRef(nil), assets.refs...)
}

// EnqueueAuthorizedFollowUp accepts a complete host-authorized turn, including
// real attachment references. Identity and authorization belong to the ingress,
// exactly as for EnqueueFollowUp. Acceptance is not execution completion.
func (s *Service) EnqueueAuthorizedFollowUp(ctx context.Context, invocation string, cmd turn.StartTurnCommand) error {
	if strings.TrimSpace(invocation) == "" || strings.TrimSpace(cmd.TeamID) == "" || strings.TrimSpace(cmd.BotID) == "" || strings.TrimSpace(cmd.ThreadID) == "" {
		return sessionruntime.ErrQueueInvalidReference
	}
	if s.delegationScope != nil {
		var err error
		ctx, _, err = s.delegationScope(ctx, chatRequestFromCommand(cmd))
		if err != nil {
			return err
		}
	}
	// The durable turn idempotency key is derived from the queue item ID. Keep
	// that ID stable as well, even if an uncertain delivery outlives queue memory.
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("memoh/host-follow-up/v1\x00"+cmd.BotID+"\x00"+cmd.ThreadID+"\x00"+invocation)).String()
	// Use exactly the command that a queued continuation restores. A delivery
	// racing with the terminal observer, or retried after queue memory is lost,
	// must address the same durable invocation and the same payload.
	cmd.NoDefer = true
	cmd.IdempotencyKey = "follow-up:" + id
	for range 4 {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := s.enqueueFollowUpCommandWithID(ctx, id, invocation, cmd)
		if !errors.Is(err, sessionruntime.ErrQueueNoActiveRun) {
			return err
		}
		// The supervisor commonly finishes before its specialist. Its new turn
		// is server-owned, just like a queue continuation, and must outlive the
		// short result-save/delivery request. StartTurn rechecks authorization.
		runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		handle, err := s.StartTurn(runCtx, cmd)
		if err == nil {
			go func() {
				defer cancel()
				drainDeferredTurn(handle)
			}()
			return nil
		}
		cancel()
		if errors.Is(err, turn.ErrDuplicateTurn) && !errors.Is(err, sessionruntime.ErrInvocationConflict) {
			return nil
		}
		if !errors.Is(err, turn.ErrSessionBusy) {
			return err
		}
		// A different turn won admission after the queue observed idle. Retry
		// enqueue with the same identity; exhaustion remains an unknown delivery.
	}
	return turn.ErrSessionBusy
}
