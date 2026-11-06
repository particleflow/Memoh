package application

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	sdk "github.com/felinics/twilight/sdk"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/step"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/inputsource"
)

// queueStepCoordinator orders queue consumption after history persistence.
// History remains a fenced PostgreSQL transaction; queue state lives in the
// session runtime backend (memory or Redis). Queue state is transient and is
// never included in the history transaction.
type queueStepCoordinator struct {
	service              *Service
	req                  ChatRequest
	persister            messagepkg.AgentStepPersister
	replacementPersister messagepkg.AgentReplacementPersister
	run                  sessionruntime.RunHandle
	steerEnabled         bool
	pendingSteer         *sessionruntime.SteerClaimRef
	// pendingSteerTurn is the slot drawn for pendingSteer. The step that
	// consumes the input files its user row under this turn instead of minting
	// one at commit, so the name the client was shown at claim time is the name
	// history ends up using.
	pendingSteerTurn *messagepkg.TurnSlot
	// pendingSteerIdentity is the attribution the claimed item recorded at
	// ingress. The row that consumes the input is a real user message, so it
	// carries the sender and input source an idle send would have, and the
	// claim frame publishes the same identity before the row exists.
	pendingSteerIdentity steerInputIdentity
}

type queueStepOutcome struct {
	historyCommitted     bool
	persisted            []messagepkg.Message
	appliedSteerItemID   string
	claimedSteer         *sessionruntime.SteerItem
	claimedSteerTurn     *messagepkg.TurnSlot
	claimedSteerIdentity steerInputIdentity
	replacementFinalized bool
}

func newQueueStepCoordinator(s *Service, req ChatRequest) *queueStepCoordinator {
	if !req.QueueSteerEnabled && req.TurnReplacement == nil {
		return nil
	}
	if s == nil || s.sessionManager == nil || s.messageService == nil || req.RunHandle.RunID == "" || req.RunHandle.OwnerID == "" || req.RunHandle.FencingToken <= 0 {
		return nil
	}
	persister, ok := s.messageService.(messagepkg.AgentStepPersister)
	if !ok {
		return nil
	}
	replacementPersister, _ := s.messageService.(messagepkg.AgentReplacementPersister)
	if req.TurnReplacement != nil && replacementPersister == nil {
		return nil
	}
	q := &queueStepCoordinator{
		service: s, req: req, persister: persister,
		replacementPersister: replacementPersister,
		run:                  req.RunHandle,
		steerEnabled:         req.QueueSteerEnabled,
	}
	return q
}

func (q *queueStepCoordinator) persistHistory(ctx context.Context, step messagepkg.AgentStep) ([]messagepkg.Message, error) {
	if len(step.Messages) == 0 {
		return nil, nil
	}
	if q.req.TurnReplacement != nil {
		return q.replacementPersister.PersistAgentReplacementStep(ctx, step)
	}
	return q.persister.PersistAgentStep(ctx, step)
}

// allocateSteerTurn names the claimed input before it is persisted, mirroring
// what admission does for a run's request turn. A failure is not fatal: the
// step commit still mints a turn, and the client falls back to the provisional
// identity it used before this existed.
func (q *queueStepCoordinator) allocateSteerTurn(ctx context.Context) *messagepkg.TurnSlot {
	allocator, ok := q.service.messageService.(messagepkg.TurnSlotAllocator)
	if !ok {
		return nil
	}
	slot, err := allocator.AllocateTurnSlot(context.WithoutCancel(ctx), q.run.SessionID)
	if err != nil {
		if q.service.logger != nil {
			q.service.logger.WarnContext(ctx, "allocate steer turn slot failed",
				slog.String("session_id", q.run.SessionID), slog.Any("error", err))
		}
		return nil
	}
	return &slot
}

// steerTurnForStep is the slot the step about to be persisted must file its
// injected user row under, or nil when this step carries no claimed steer.
func (q *queueStepCoordinator) steerTurnForStep() *messagepkg.TurnSlot {
	if q == nil || q.pendingSteer == nil {
		return nil
	}
	return q.pendingSteerTurn
}

func (q *queueStepCoordinator) releaseSteerClaim(ctx context.Context) {
	if q == nil || q.pendingSteer == nil || q.service == nil || q.service.sessionManager == nil {
		return
	}
	_ = q.service.sessionManager.ReleaseSteer(ctx, sessionruntime.Key{BotID: q.run.BotID, SessionID: q.run.SessionID}, *q.pendingSteer)
	// The slot drawn for this input is spent. A later claim draws its own; the
	// gap is harmless because positions are only ever compared.
	q.pendingSteerTurn = nil
	q.pendingSteerIdentity = steerInputIdentity{}
}

func (q *queueStepCoordinator) commit(
	ctx context.Context,
	kind queueStepKind,
	agentStep messagepkg.AgentStep,
	previouslyPersisted []messagepkg.Message,
) (queueStepOutcome, error) {
	var outcome queueStepOutcome
	if q == nil || q.service == nil || q.service.sessionManager == nil {
		return outcome, errors.New("live queue step transaction is unavailable")
	}
	persisted, err := q.persistHistory(context.WithoutCancel(ctx), agentStep)
	if err != nil {
		q.releaseSteerClaim(ctx)
		return outcome, err
	}
	outcome.persisted = persisted
	outcome.historyCommitted = true
	if q.pendingSteer != nil {
		if err := q.service.sessionManager.ApplySteer(ctx, sessionruntime.Key{BotID: q.run.BotID, SessionID: q.run.SessionID}, *q.pendingSteer); err != nil {
			// The steer text is already part of the committed step. Releasing the
			// claim here would let the next commit inject it a second time; leave
			// it claimed and fail the step, so terminal cleanup rejects the item.
			return outcome, err
		}
		outcome.appliedSteerItemID = string(q.pendingSteer.ItemID)
		q.pendingSteer = nil
		q.pendingSteerTurn = nil
		q.pendingSteerIdentity = steerInputIdentity{}
	}
	if kind == queueStepDeferredDecision {
		// The loop parks after this step. A claim taken here would sit
		// unapplied across the decision, and across any owner change while
		// the run waits. The resumed execution claims at its next complete
		// step or model-interruption checkpoint, keeping the approved tool
		// result ahead of the new input.
		// A deferred decision ends this producer just like a final step. Its
		// replacement must be visible before an accepted answer re-enters the
		// run: that continuation resolves its request by the admitted turn.
		// Keep the steer boundary unchanged; no input is claimed while parked.
		return q.finalizeReplacement(ctx, outcome, previouslyPersisted)
	}

	var item sessionruntime.SteerItem
	var claim sessionruntime.SteerClaimRef
	var claimed bool
	if q.steerEnabled {
		item, claim, claimed, err = q.service.sessionManager.ClaimNextSteer(ctx, q.run, kind == queueStepFinal)
	}
	if err != nil {
		return outcome, err
	}
	if claimed {
		q.pendingSteer = &claim
		q.pendingSteerTurn = q.allocateSteerTurn(ctx)
		q.pendingSteerIdentity = q.resolveSteerIdentity(ctx, item)
		outcome.claimedSteer = &item
		outcome.claimedSteerTurn = q.pendingSteerTurn
		outcome.claimedSteerIdentity = q.pendingSteerIdentity
	} else if q.req.TurnReplacement != nil && kind == queueStepFinal {
		return q.finalizeReplacement(ctx, outcome, previouslyPersisted)
	}
	return outcome, nil
}

// resolveSteerIdentity reads the attribution the claimed item recorded at
// ingress. The queue payload is the complete turn the ingress admitted, so its
// sender and channel are the same facts an idle send would have carried. An
// item without a command (written by an older release) has no recorded sender
// and stays unattributed rather than borrowing the enclosing turn's actor.
func (q *queueStepCoordinator) resolveSteerIdentity(ctx context.Context, item sessionruntime.SteerItem) steerInputIdentity {
	identity := steerInputIdentity{}
	cmd := decodeQueuePayload(item.Payload).Command
	if cmd == nil || cmd.DelegationResultID != "" || cmd.TaskResultReceipt != "" {
		identity.Source = inputsource.Unattributed(q.run.BotID, q.run.SessionID)
		return identity
	}
	channelIdentityID, senderUserID := q.service.resolvePersistSenderIDs(ctx, ChatRequest{
		BotID: q.run.BotID, ThreadID: q.run.SessionID,
		UserID: strings.TrimSpace(cmd.UserID), SourceChannelIdentityID: strings.TrimSpace(cmd.SourceChannelIdentityID),
		CurrentChannel: strings.TrimSpace(cmd.CurrentChannel),
	})
	if senderUserID == "" && channelIdentityID == "" {
		// The recorded actor does not resolve to an account this deployment can
		// name. The row stays unattributed rather than claiming a user it cannot
		// identify: the client renders kind=user without a sender as a foreign,
		// unknown author anyway.
		identity.Source = inputsource.Unattributed(q.run.BotID, q.run.SessionID)
		return identity
	}
	identity.SenderUserID = senderUserID
	identity.SenderChannelIdentityID = channelIdentityID
	identity.Source = (inputsource.Source{
		Kind: inputsource.User, BotID: q.run.BotID, SessionID: q.run.SessionID,
		UserID: strings.TrimSpace(cmd.UserID), ChannelIdentityID: strings.TrimSpace(cmd.SourceChannelIdentityID),
		Channel: strings.TrimSpace(cmd.CurrentChannel),
	}).Scoped(q.run.BotID, q.run.SessionID)
	if identity.Source.Kind != inputsource.User {
		// A recorded source that cannot be scoped to this conversation is not
		// usable; the sender fields still describe the row we are writing.
		identity.Source = inputsource.Unattributed(q.run.BotID, q.run.SessionID)
	}
	return identity
}

// steerInputIdentity is the attribution of one claimed queue input.
type steerInputIdentity struct {
	SenderUserID            string
	SenderChannelIdentityID string
	Source                  inputsource.Source
}

func (q *queueStepCoordinator) finalizeReplacement(ctx context.Context, outcome queueStepOutcome, previouslyPersisted []messagepkg.Message) (queueStepOutcome, error) {
	if q.req.TurnReplacement == nil {
		return outcome, nil
	}
	allPersisted := append([]messagepkg.Message(nil), previouslyPersisted...)
	allPersisted = append(allPersisted, outcome.persisted...)
	requestID := strings.TrimSpace(q.req.TurnReplacement.RequestMessageID)
	if requestID == "" {
		requestID = firstUserID(allPersisted)
	}
	assistantID := firstAssistantID(allPersisted)
	if assistantID == "" {
		return outcome, errors.New("replacement assistant message was not persisted")
	}
	if err := q.replacementPersister.FinalizeAgentReplacement(ctx, q.req.ThreadID, *q.req.TurnReplacement, requestID, assistantID); err != nil {
		return outcome, err
	}
	outcome.replacementFinalized = true
	return outcome, nil
}

type queueStepKind string

const (
	queueStepToolLoop         queueStepKind = "tool_loop"
	queueStepDeferredDecision queueStepKind = "deferred_decision"
	queueStepFinal            queueStepKind = "final"
	queueStepSteered          queueStepKind = "steered"
)

func classifyQueueStep(record *step.Record) queueStepKind {
	if record == nil || record.Deferred != nil {
		return queueStepDeferredDecision
	}
	if record.Result.FinishReason == sdk.FinishReasonToolCalls && len(record.Result.ToolCalls) > 0 {
		return queueStepToolLoop
	}
	return queueStepFinal
}
