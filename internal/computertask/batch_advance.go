package computertask

import (
	"bytes"
	"context"
	"errors"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

// A batch advances inside the existing result-delivery scan. Each pass reads the
// active batches in creation order and lets every batch make one step of
// progress: settle the in-flight operation when its receipt is terminal, then
// dispatch the next queued item once the lane is free. Because a lane holds at
// most one unsettled operation, consecutive items of one batch run strictly in
// submission order, and separate batches of the same conversation/target
// interleave without overtaking.
//
// Nothing here imposes a queue-length, item-count, retry or rate limit. The only
// gates are the ones the single-task path already has: an unsettled operation in
// the lane, a paused admission gate, or an item that already reached a terminal
// state.

// BatchAdvanceStep reports what one scan pass observed for one batch.
type BatchAdvanceStep struct {
	BatchID string `json:"batch_id"`
	State   string `json:"state"`
	Item    int    `json:"item"`
	Action  string `json:"action"`
	Detail  string `json:"detail,omitempty"`
	Changed bool   `json:"changed"`
}

// advanceBatch makes at most one observable transition for one batch. It never
// blocks, never retries an unknown outcome and never dispatches two items at
// once.
func (s *Service) advanceBatch(ctx context.Context, rec dbstore.ComputerTaskBatchRecord) (BatchAdvanceStep, error) {
	step := BatchAdvanceStep{BatchID: rec.ID, State: rec.State, Item: -1, Action: "idle"}
	items, err := DecodeBatchItems(rec.Items)
	if err != nil {
		// A malformed row is reported through the caller view, never rewritten here.
		step.Action = "unreadable"
		return step, nil
	}
	position := advanceIndex(items)
	if position < 0 {
		step.Action = "terminal"
		return step, nil
	}
	step.Item = position
	if items[position].State == BatchItemInFlight {
		return s.observeBatchItem(ctx, rec, items, position, step)
	}
	return s.dispatchBatchItem(ctx, rec, items, position, step)
}

// advanceIndex is the only position a batch may act on. A queued item behind an
// unsettled in-flight item must wait: skipping it would reorder the queue.
func advanceIndex(items []BatchItem) int {
	for i, item := range items {
		switch item.State {
		case BatchItemInFlight, BatchItemQueued:
			return i
		}
	}
	return -1
}

// observeBatchItem reads the pinned single-task receipt and settles the item
// only on a terminal, immutable outcome. Observation is the same authorized
// refresh computer_task_result performs; an unsettled result keeps the slot.
func (s *Service) observeBatchItem(ctx context.Context, rec dbstore.ComputerTaskBatchRecord, items []BatchItem, position int, step BatchAdvanceStep) (BatchAdvanceStep, error) {
	binding := toolcontext.ComputerBinding{BotID: rec.BotID, UserID: rec.UserID, SessionID: rec.SessionID, TargetID: rec.TargetID, RunID: rec.RunID}
	observed, err := s.observe(ctx, binding, items[position].OperationID, true)
	if err != nil {
		// A denied or unavailable read means the binding could not be rebuilt at
		// this pass (for example the target registration moved). The item keeps
		// its slot; the next pass retries the same read and never dispatches
		// around it.
		step.Action, step.Detail = "observe_deferred", "receipt_unreadable"
		return step, nil
	}
	step.Action = "observed"
	if !batchOutcomeTerminal(observed) {
		blocked := batchItemBlocked(observed)
		if blocked == items[position].Blocked {
			return step, nil
		}
		items[position].Blocked = blocked
		if err := s.persistBatchItems(ctx, rec, items); err != nil {
			return step, err
		}
		step.Changed = true
		return step, nil
	}
	items[position].Blocked, items[position].Outcome = false, batchItemOutcome(observed)
	if items[position].Outcome == "succeeded" {
		items[position].State = BatchItemSucceeded
	} else {
		items[position].State, items[position].Error = BatchItemFailed, batchItemReason(observed)
	}
	step.Changed, step.Detail = true, items[position].Outcome
	if err := s.persistBatchItems(ctx, rec, items); err != nil {
		return step, err
	}
	return step, nil
}

// dispatchBatchItem submits the next queued item through the unchanged
// single-task path. That path already enforces the one-unsettled-operation lane
// through its partial unique index, so a busy lane surfaces as its conflict
// receipt and the item simply stays queued.
func (s *Service) dispatchBatchItem(ctx context.Context, rec dbstore.ComputerTaskBatchRecord, items []BatchItem, position int, step BatchAdvanceStep) (BatchAdvanceStep, error) {
	binding := toolcontext.ComputerBinding{BotID: rec.BotID, UserID: rec.UserID, SessionID: rec.SessionID, TargetID: rec.TargetID, RunID: rec.RunID}
	dispatched, dispatchErr := s.submit(ctx, binding, items[position].Spec.Submit(rec.TargetID, rec.ID))
	if blockedBy := dispatched.BlockedByOperation; blockedBy != "" {
		// Another unresolved operation owns this lane. The batch does not adopt
		// it and does not dispatch around it: the item waits its turn.
		step.Action, step.Detail = "dispatch_deferred", "lane_busy"
		return step, nil
	}
	if dispatched.ID != "" {
		// The single-task path created this receipt, so it owns the outcome from
		// here on. An unsettled dispatch stays in flight and blocked; the batch
		// never re-dispatches an operation whose result is unknown.
		items[position].State, items[position].OperationID = BatchItemInFlight, dispatched.ID
		items[position].Blocked = batchItemBlocked(dispatched)
		if err := s.persistBatchItems(ctx, rec, items); err != nil {
			return step, err
		}
		step.Action, step.Changed, step.Detail = "dispatched", true, dispatched.ID
		return step, nil
	}
	if dispatchErr != nil && batchDispatchRetryable(dispatchErr) {
		// A gate that is not the item's own fault (paused admission, an
		// unreachable runtime) leaves the item queued and reports the reason.
		step.Action, step.Detail = "dispatch_deferred", batchFailureReason(dispatchErr)
		return step, nil
	}
	items[position].State = BatchItemFailed
	items[position].Outcome = "not_started"
	items[position].Error = batchFailureReason(dispatchErr)
	if err := s.persistBatchItems(ctx, rec, items); err != nil {
		return step, err
	}
	step.Action, step.Changed, step.Detail = "failed", true, items[position].Error
	return step, nil
}

// persistBatchItems writes one transition under the batch row lock, guarded by
// the snapshot the transition read. A concurrent writer (a user cancel) that
// changed the items first makes this write fail instead of silently reverting
// it; the next pass re-reads and re-decides.
func (s *Service) persistBatchItems(ctx context.Context, rec dbstore.ComputerTaskBatchRecord, items []BatchItem) error {
	return s.batches.ChangeComputerTaskBatch(ctx, rec.ID, func(next *dbstore.ComputerTaskBatchRecord) error {
		if !bytes.Equal(next.Items, rec.Items) {
			return ErrConflict
		}
		return storeBatchItems(next, items)
	})
}

// batchOutcomeTerminal reports whether the single-task receipt carries the
// immutable business outcome a batch may record. Mirrors the host's own
// completion test: a saved Pi result on a released invocation slot.
func batchOutcomeTerminal(observed Result) bool {
	if observed.ID == "" || observed.PiResult == nil {
		return false
	}
	if observed.State != "completed" && observed.State != "not_started" && observed.State != "rejected" {
		return false
	}
	switch observed.PiResult.Outcome {
	case "succeeded", "failed", "aborted", "incomplete", "invalid_protocol", "not_started", "output_limit":
		return true
	}
	return false
}

// batchItemBlocked reports whether this receipt cannot settle by waiting: its
// outcome is unknown, so a later pass would only re-read the same uncertainty.
// A dispatched or submitted item is still progressing and is not blocked; the
// collection loop advances it on the next pass.
func batchItemBlocked(observed Result) bool {
	if observed.ID == "" {
		return false
	}
	return observed.State == "unknown"
}

func batchItemOutcome(observed Result) string {
	if observed.PiResult != nil {
		return observed.PiResult.Outcome
	}
	if observed.State == "not_started" || observed.State == "rejected" {
		return "not_started"
	}
	return "unknown"
}

// batchItemReason is the bounded machine cause kept on a failed item. It names
// a receipt field, never private content, paths or credentials.
func batchItemReason(observed Result) string {
	if observed.PiResult == nil {
		return "outcome_unavailable"
	}
	if observed.PiResult.Outcome == "not_started" && observed.PiResult.Preflight != "" {
		return observed.PiResult.Preflight
	}
	return observed.PiResult.Outcome
}

// batchFailureReason maps a dispatch error to a bounded machine cause. It never
// echoes the error text, which can carry private paths or provider detail.
func batchFailureReason(err error) string {
	switch {
	case err == nil:
		return "unknown"
	case errors.Is(err, ErrInvalid):
		return "invalid_request"
	case errors.Is(err, ErrDenied):
		return "denied"
	case errors.Is(err, ErrApproval):
		return "approval_required"
	case errors.Is(err, ErrPreflightUnavailable):
		return "connection_unavailable"
	case admissionBlocked(err):
		return admissionErrorCode(err)
	default:
		return "dispatch_failed"
	}
}

// batchDispatchRetryable reports whether the failure is a gate the batch simply
// meets again on a later pass, rather than an item-level defect.
func batchDispatchRetryable(err error) bool {
	return errors.Is(err, ErrPreflightUnavailable) || admissionBlocked(err)
}

// ScanBatches advances every active batch once. It runs inside the existing
// result-delivery scan, so it needs a host callback that rebuilds each batch's
// own authorized conversation context; a failure in one batch never stops the
// others. The binding is attached first, exactly as the result pulls do,
// because the registered rebuild reads HostComputerBinding to verify it.
func (s *Service) ScanBatches(ctx context.Context) error {
	if s.batches == nil || s.batchContext == nil {
		return nil
	}
	records, err := s.batches.ListActiveComputerTaskBatches(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, rec := range records {
		binding := toolcontext.ComputerBinding{BotID: rec.BotID, UserID: rec.UserID, SessionID: rec.SessionID, TargetID: rec.TargetID, RunID: rec.RunID}
		bound, bindErr := toolcontext.WithComputerBinding(ctx, binding)
		if bindErr == nil {
			bound, bindErr = s.batchContext(bound, binding)
		}
		if bindErr != nil {
			continue // Not routable this pass; the batch keeps its position.
		}
		if _, advanceErr := s.advanceBatch(bound, rec); advanceErr != nil {
			errs = append(errs, advanceErr)
		}
	}
	return errors.Join(errs...)
}
