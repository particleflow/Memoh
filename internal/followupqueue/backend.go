package followupqueue

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	runtime "github.com/felinics/memoh/internal/agent/runtime/session"
)

// Backend decorates the existing single-process live backend. Run ownership,
// fencing, streams and steer execution remain owned by that backend/manager.
type Backend struct {
	*runtime.MemoryBackend
	Store         *Store
	AuthorizeHTTP func(context.Context, runtime.Key, runtime.FollowUpItem) error
	mu            sync.Mutex
}

func NewBackend(live *runtime.MemoryBackend, store *Store) *Backend {
	return &Backend{MemoryBackend: live, Store: store}
}

func ordered(state *State, limit int) []runtime.FollowUpItem {
	var items []runtime.FollowUpItem
	for _, item := range state.Items {
		if item.Status == runtime.QueueAccepted {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Position < items[j].Position })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func itemAt(state *State, id runtime.FollowUpItemID) *runtime.FollowUpItem {
	for i := range state.Items {
		if state.Items[i].ID == id {
			return &state.Items[i]
		}
	}
	return nil
}

func (b *Backend) EnqueueFollowUp(ctx context.Context, key runtime.Key, itemID, invocation string, payload []byte) (out runtime.FollowUpItem, err error) {
	if strings.TrimSpace(itemID) == "" || strings.TrimSpace(invocation) == "" || len(payload) == 0 {
		return out, runtime.ErrQueueInvalidReference
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	err = b.Store.Change(ctx, key, func(state *State) error {
		for _, item := range state.Items {
			if item.InvocationID == invocation {
				if !bytes.Equal(item.Payload, payload) {
					return runtime.ErrQueueInvocationConflict
				}
				out = item
				return nil
			}
		}
		snapshot, exists, e := b.Load(ctx, key)
		if e != nil {
			return e
		}
		run := snapshot.CurrentRunView
		if !exists || run == nil || !runtime.IsActiveRunStatus(run.Status) {
			return runtime.ErrQueueNoActiveRun
		}
		if len(ordered(state, 0)) >= runtime.MaxPendingQueueItems {
			return runtime.ErrQueueCapacityExceeded
		}
		var position int64
		for _, item := range state.Items {
			if item.Position > position {
				position = item.Position
			}
			if string(item.ID) == itemID {
				return runtime.ErrQueueInvocationConflict
			}
		}
		out = runtime.FollowUpItem{ID: runtime.FollowUpItemID(itemID), BotID: key.BotID, SessionID: key.SessionID, InvocationID: invocation, EnqueuedDuringRunID: run.RunID, Payload: bytes.Clone(payload), Status: runtime.QueueAccepted, Position: position + 1, CreatedAt: time.Now().UTC()}
		state.Items = append(state.Items, out)
		state.HTTP[itemID], _ = ctx.Value(httpOriginKey{}).(bool)
		return nil
	})
	return
}

func (b *Backend) PendingQueues(ctx context.Context, key runtime.Key, limit int) ([]runtime.SteerItem, []runtime.FollowUpItem, error) {
	steers, _, err := b.MemoryBackend.PendingQueues(ctx, key, limit)
	if err != nil {
		return nil, nil, err
	}
	state, err := b.Store.Read(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	visible := ordered(&state, 0)
	for _, item := range state.Items {
		if item.Status == "unknown" {
			visible = append(visible, item)
		}
	}
	sort.SliceStable(visible, func(i, j int) bool { return visible[i].Position < visible[j].Position })
	if limit > 0 && len(visible) > limit {
		visible = visible[:limit]
	}
	return steers, visible, nil
}

func (b *Backend) ReorderFollowUp(ctx context.Context, key runtime.Key, item, before runtime.FollowUpPendingRef) (out []runtime.FollowUpItem, err error) {
	if item.ItemID == "" || item.ItemID == before.ItemID {
		return nil, runtime.ErrQueueInvalidReference
	}
	err = b.Store.Change(ctx, key, func(state *State) error {
		pending := ordered(state, 0)
		from, to := -1, -1
		for i, v := range pending {
			if v.ID == item.ItemID {
				from = i
			}
			if v.ID == before.ItemID {
				to = i
			}
		}
		if from < 0 || (before.ItemID != "" && to < 0) {
			return runtime.ErrQueueNotPending
		}
		moving := pending[from]
		pending = append(pending[:from], pending[from+1:]...)
		if before.ItemID == "" {
			to = len(pending)
		} else if from < to {
			to--
		}
		pending = append(pending, moving)
		copy(pending[to+1:], pending[to:len(pending)-1])
		pending[to] = moving
		for i, v := range pending {
			itemAt(state, v.ID).Position = int64(i + 1)
		}
		out = ordered(state, 0)
		return nil
	})
	return
}

func (b *Backend) UpdateFollowUp(ctx context.Context, key runtime.Key, id runtime.FollowUpItemID, payload []byte) (out runtime.FollowUpItem, err error) {
	if len(payload) == 0 {
		return out, runtime.ErrQueueInvalidReference
	}
	err = b.Store.Change(ctx, key, func(state *State) error {
		item := itemAt(state, id)
		if item == nil || item.Status != runtime.QueueAccepted {
			return runtime.ErrQueueNotPending
		}
		item.Payload = bytes.Clone(payload)
		out = *item
		return nil
	})
	return
}

func (b *Backend) CancelFollowUp(ctx context.Context, key runtime.Key, id runtime.FollowUpItemID) error {
	return b.Store.Change(ctx, key, func(state *State) error {
		item := itemAt(state, id)
		if item == nil || item.Status != runtime.QueueAccepted {
			return runtime.ErrQueueNotPending
		}
		item.Status = runtime.QueueCanceled
		return nil
	})
}

func claimCandidate(state *State, trigger string) *runtime.FollowUpItem {
	if id := state.Claims[trigger]; id != "" {
		item := itemAt(state, runtime.FollowUpItemID(id))
		if item != nil && item.Status == runtime.QueueClaimed && item.Claim != nil {
			return item
		}
		return nil
	}
	pending := ordered(state, 1)
	if len(pending) == 0 {
		return nil
	}
	return itemAt(state, pending[0].ID)
}

var errCandidateChanged = errors.New("follow-up changed during authority check")

func (b *Backend) ClaimNextFollowUp(ctx context.Context, key runtime.Key, trigger string) (out runtime.FollowUpItem, ref runtime.FollowUpClaimRef, ok bool, err error) {
	if strings.TrimSpace(trigger) == "" {
		err = runtime.ErrQueueInvalidReference
		return
	}
	for {
		if err = ctx.Err(); err != nil {
			return
		}
		var before State
		before, err = b.Store.Read(ctx, key)
		if err != nil {
			return
		}
		candidate := claimCandidate(&before, trigger)
		if candidate == nil {
			return
		}
		httpOrigin := before.HTTP[string(candidate.ID)]
		// Do not hold a database connection/queue row lock while authority loads
		// account, contact and session state. Even a one-connection pool must work.
		if httpOrigin {
			if b.AuthorizeHTTP == nil {
				err = errors.New("HTTP follow-up current authority is unavailable")
				return
			}
			if err = b.AuthorizeHTTP(ctx, key, *candidate); err != nil {
				return
			}
		}
		err = b.Store.Change(ctx, key, func(state *State) error {
			item := claimCandidate(state, trigger)
			if item == nil || item.ID != candidate.ID || state.HTTP[string(item.ID)] != httpOrigin || !bytes.Equal(item.Payload, candidate.Payload) {
				return errCandidateChanged
			}
			if item.Status == runtime.QueueClaimed {
				out = *item
				ref = *item.Claim
				ok = true
				return nil
			}
			ref = runtime.FollowUpClaimRef{ItemID: item.ID, TriggerRunID: trigger, ClaimToken: uuid.NewString()}
			item.Claim = &ref
			item.Status = runtime.QueueClaimed
			state.Claims[trigger] = string(item.ID)
			out = *item
			ok = true
			return nil
		})
		if errors.Is(err, errCandidateChanged) {
			continue
		}
		if err != nil {
			ok = false
		}
		return
	}
}

func (b *Backend) transition(ctx context.Context, key runtime.Key, ref runtime.FollowUpClaimRef, status runtime.QueueStatus, code string) error {
	return b.Store.Change(ctx, key, func(state *State) error {
		item := itemAt(state, ref.ItemID)
		if item == nil || item.Status != runtime.QueueClaimed || item.Claim == nil || *item.Claim != ref {
			return runtime.ErrQueueInvalidReference
		}
		item.Status = status
		item.ErrorCode = code
		if status != runtime.QueueApplied {
			delete(state.Claims, ref.TriggerRunID)
			item.Claim = nil
		}
		return nil
	})
}

func (b *Backend) ApplyFollowUp(ctx context.Context, key runtime.Key, ref runtime.FollowUpClaimRef) error {
	return b.transition(ctx, key, ref, runtime.QueueApplied, "")
}

func (b *Backend) ReleaseFollowUp(ctx context.Context, key runtime.Key, ref runtime.FollowUpClaimRef) error {
	return b.transition(ctx, key, ref, runtime.QueueAccepted, "")
}

func (b *Backend) RejectFollowUp(ctx context.Context, key runtime.Key, ref runtime.FollowUpClaimRef, code string) error {
	if strings.TrimSpace(code) == "" {
		return runtime.ErrQueueInvalidReference
	}
	return b.transition(ctx, key, ref, runtime.QueueRejected, code)
}

// Promotion crosses durable input and a transient run-bound steer. Persist the
// intent before touching the live queue. An interrupted promotion remains
// unknown; it cannot become an automatically replayable follow-up.
func (b *Backend) PromoteFollowUpToSteer(ctx context.Context, key runtime.Key, ref runtime.FollowUpPendingRef) (out runtime.PromoteFollowUpResult, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var payload []byte
	var replay bool
	steerID := uuid.NewString()
	promotionCode := "queue_promotion_unconfirmed"
	err = b.Store.Change(ctx, key, func(state *State) error {
		if original, ok := state.Promotions[string(ref.ItemID)]; ok {
			if item := itemAt(state, ref.ItemID); item == nil || item.Status == "unknown" {
				return runtime.ErrQueueNotPending
			}
			out = original
			replay = true
			return nil
		}
		item := itemAt(state, ref.ItemID)
		if item == nil || item.Status != runtime.QueueAccepted {
			return runtime.ErrQueueNotPending
		}
		snapshot, exists, e := b.Load(ctx, key)
		if e != nil {
			return e
		}
		if !exists || snapshot.CurrentRunView == nil || !runtime.IsActiveRunStatus(snapshot.CurrentRunView.Status) {
			return runtime.ErrQueueNoActiveRun
		}
		if !runtime.SteerRunAvailable(snapshot.CurrentRunView) {
			return runtime.ErrQueueSteerUnsupported
		}
		payload = bytes.Clone(item.Payload)
		item.Status = runtime.QueueStatus("unknown")
		item.ErrorCode = promotionCode
		return nil
	})
	if err != nil {
		return out, err
	}
	if replay {
		return out, nil
	}
	steer, e := b.EnqueueSteer(ctx, key, steerID, "promoted:"+string(ref.ItemID), payload)
	if e != nil {
		// These live-backend errors prove no steer was accepted. Other outcomes
		// retain unknown, since loss of acknowledgement is not proof of rejection.
		if errors.Is(e, runtime.ErrQueueNoActiveRun) || errors.Is(e, runtime.ErrQueueSteerUnsupported) || errors.Is(e, runtime.ErrQueueCapacityExceeded) {
			err = b.Store.Change(ctx, key, func(state *State) error {
				item := itemAt(state, ref.ItemID)
				if item == nil || item.Status != "unknown" || item.ErrorCode != promotionCode {
					return runtime.ErrQueueInvalidReference
				}
				item.Status = runtime.QueueAccepted
				item.ErrorCode = ""
				return nil
			})
			if err != nil {
				return out, err
			}
		}
		return out, e
	}
	err = b.Store.Change(ctx, key, func(state *State) error {
		item := itemAt(state, ref.ItemID)
		if item == nil || item.Status != "unknown" || item.ErrorCode != promotionCode {
			return runtime.ErrQueueInvalidReference
		}
		item.Status = runtime.QueueCanceled
		state.Promotions[string(ref.ItemID)] = runtime.PromoteFollowUpResult{FollowUp: ref, Steer: steer}
		item.ErrorCode = ""
		return nil
	})
	out = runtime.PromoteFollowUpResult{FollowUp: ref, Steer: steer}
	return
}

var _ runtime.LiveQueueBackend = (*Backend)(nil)

// HTTPOrigin marks the existing authenticated HTTP queue ingress. It is never
// derived from client payload, task text, a Bot name or a runtime label.
type httpOriginKey struct{}

func HTTPOrigin(ctx context.Context) context.Context {
	return context.WithValue(ctx, httpOriginKey{}, true)
}
