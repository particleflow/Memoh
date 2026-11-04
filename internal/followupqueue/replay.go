package followupqueue

import (
	"bytes"

	runtime "github.com/felinics/memoh/internal/agent/runtime/session"
)

// replayFollowUp resolves an enqueue whose invocation a previous release
// already recorded in the durable queue.
//
// Equal bytes change nothing, so a cancel or a recorded outcome stays the
// record of what happened. Different bytes are the deployment-upgrade case: a
// deploy changes how the same durable invocation's payload is encoded, so the
// rebuilt command never matches the bytes an older release stored. That alone
// used to wedge the invocation forever, because nothing replaced the item and
// nothing expired it.
//
// An item nobody has claimed still stands for that delivery intent, so it
// adopts the new bytes. An item an earlier release terminalized before any claim
// (canceled, expired) never executed either, so it returns to accepted — that is
// what makes the invocation claimable again instead of conflicting on every
// retry. A claimed item is in flight, and an applied or rejected one records the
// outcome; those keep refusing the change. An item stranded between the two
// writes of a promotion ("unknown") stays untouched, because the acknowledgement
// proving no steer ran is missing.
func replayFollowUp(item *runtime.FollowUpItem, payload []byte) (bool, error) {
	if bytes.Equal(item.Payload, payload) {
		return false, nil
	}
	if item.Claim != nil {
		return false, runtime.ErrQueueInvocationConflict
	}
	switch item.Status {
	case runtime.QueueAccepted, runtime.QueueCanceled, runtime.QueueExpired:
	default:
		return false, runtime.ErrQueueInvocationConflict
	}
	item.Payload = bytes.Clone(payload)
	item.Status = runtime.QueueAccepted
	item.ErrorCode = ""
	item.Claim = nil
	return true, nil
}
