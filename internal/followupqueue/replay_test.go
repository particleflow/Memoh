package followupqueue

import (
	"errors"
	"testing"

	runtime "github.com/felinics/memoh/internal/agent/runtime/session"
)

// This rule is the whole difference between a delivery that resumes after a
// deploy and one that conflicts on every retry, so each status is pinned.
func TestReplayFollowUpUpgradeRule(t *testing.T) {
	old := []byte(`{"text":"old release payload"}`)
	rebuilt := []byte(`{"text":"rebuilt release payload"}`)
	claim := &runtime.FollowUpClaimRef{ItemID: "item", TriggerRunID: "run", ClaimToken: "token"}

	cases := []struct {
		name   string
		status runtime.QueueStatus
		claim  *runtime.FollowUpClaimRef
		adopt  bool
	}{
		{name: "accepted adopts the rebuilt payload", status: runtime.QueueAccepted, adopt: true},
		// An older release terminalized the item before anyone claimed it, so it
		// never executed. Its invocation only becomes deliverable this way.
		{name: "canceled revives", status: runtime.QueueCanceled, adopt: true},
		{name: "expired revives", status: runtime.QueueExpired, adopt: true},
		// A claim proves the item left the queue: in flight, or its outcome recorded.
		{name: "claimed refuses", status: runtime.QueueClaimed, claim: claim},
		{name: "applied refuses", status: runtime.QueueApplied},
		{name: "rejected refuses", status: runtime.QueueRejected},
		// A promotion stranded between its two writes has no proof no steer ran.
		{name: "stranded promotion refuses", status: queueStatusPromotionUnconfirmed},
		{name: "accepted with a claim refuses", status: runtime.QueueAccepted, claim: claim},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := runtime.FollowUpItem{Status: tc.status, Payload: append([]byte(nil), old...), ErrorCode: "prior-code", Claim: tc.claim}
			changed, err := replayFollowUp(&item, rebuilt)
			if !tc.adopt {
				if !errors.Is(err, runtime.ErrQueueInvocationConflict) {
					t.Fatalf("want conflict, got changed=%v err=%v", changed, err)
				}
				if string(item.Payload) != string(old) || item.Status != tc.status {
					t.Fatalf("a refused replay must not touch the item: %#v", item)
				}
				return
			}
			if err != nil {
				t.Fatalf("adopt: %v", err)
			}
			if !changed || string(item.Payload) != string(rebuilt) {
				t.Fatalf("payload not adopted: changed=%v payload=%q", changed, item.Payload)
			}
			if item.Status != runtime.QueueAccepted || item.ErrorCode != "" || item.Claim != nil {
				t.Fatalf("revival must clear the terminal state: %#v", item)
			}
		})
	}
}

// Equal bytes are not an upgrade: they never overturn what the queue recorded.
func TestReplayFollowUpEqualPayloadKeepsRecordedOutcome(t *testing.T) {
	payload := []byte(`{"text":"same payload"}`)
	for _, status := range []runtime.QueueStatus{runtime.QueueAccepted, runtime.QueueCanceled, runtime.QueueApplied, runtime.QueueRejected, runtime.QueueClaimed} {
		item := runtime.FollowUpItem{Status: status, Payload: append([]byte(nil), payload...), ErrorCode: "kept"}
		changed, err := replayFollowUp(&item, payload)
		if err != nil {
			t.Fatalf("%s: equal payload is a replay, not a conflict: %v", status, err)
		}
		if changed || item.Status != status || item.ErrorCode != "kept" {
			t.Fatalf("%s: equal payload must change nothing: %#v", status, item)
		}
	}
}
