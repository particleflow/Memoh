package botdelegation

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestUnknownDeliveryRequiresPersistedOptIn(t *testing.T) {
	for _, version := range []int{0, 1} {
		t.Run(map[int]string{0: "legacy", 1: "web-native-opt-in"}[version], func(t *testing.T) {
			s, _, h, in := serviceFixture(t)
			h.state = "unknown"
			identity, selected, err := s.Authority.Bind(t.Context(), "user", "manager", "child", in.TargetBotID)
			if err != nil {
				t.Fatal(err)
			}
			r, _, err := s.Ledger.Claim(t.Context(), Record{ID: uuid.NewString(), RequestID: in.RequestID, Identity: identity, SourceSelection: selected, State: "accepted", DeliveryState: "pending", Payload: Payload{Message: in.Message, ResultDeliveryVersion: version}})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.execute(t.Context(), r); err != nil {
				t.Fatal(err)
			}
			stored, err := s.ReadRecord(t.Context(), "manager", "user", "child", r.ID)
			if err != nil || stored.State != "unknown" || stored.Result.Summary != nil || len(stored.Result.Outputs) != 0 || h.runs.Load() != 1 {
				t.Fatalf("unknown execution not retained: %+v %v", stored, err)
			}
			if version == 0 {
				if h.deliveries.Load() != 0 || stored.DeliveryState != "pending" {
					t.Fatalf("legacy unknown gained a new delivery: calls=%d state=%s", h.deliveries.Load(), stored.DeliveryState)
				}
				if _, err = s.RetryDelivery(t.Context(), "manager", "user", "child", r.ID); !errors.Is(err, ErrConflict) {
					t.Fatalf("legacy unknown retry admitted: %v", err)
				}
				after, err := s.ReadRecord(t.Context(), "manager", "user", "child", r.ID)
				if err != nil || after.Revision != stored.Revision || h.deliveries.Load() != 0 || h.runs.Load() != 1 {
					t.Fatal("legacy retry changed the record or dispatched work")
				}
			} else if h.deliveries.Load() != 1 || stored.DeliveryState != "accepted" {
				t.Fatal("opted-in unknown notification was lost")
			}
		})
	}
}
