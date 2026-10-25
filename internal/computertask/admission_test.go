package computertask

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type pausedComputerStore struct {
	*fixtureStore
	failure error
}

func (s pausedComputerStore) ClaimComputerOperation(context.Context, dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, bool, error) {
	return dbstore.ComputerOperationRecord{}, false, s.failure
}

func (s pausedComputerStore) SaveComputerOperation(context.Context, dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	return dbstore.ComputerOperationRecord{}, s.failure
}

func TestComputerAdmissionToolRejectsWithoutDispatchOrPhantomReservation(t *testing.T) {
	for _, failure := range []error{db.ErrComputerAdmissionPaused, db.ErrComputerAdmissionScope} {
		t.Run(failure.Error(), func(t *testing.T) {
			source, store, relay, initial := followupFixture(t, true)
			before := store.records[initial.ID]
			source.service.store = pausedComputerStore{fixtureStore: store, failure: failure}
			client := standardClient(t, source, "pc-a", false)
			for _, name := range []string{"computer_task_submit", "computer_task_followup"} {
				t.Run(name, func(t *testing.T) {
					args := map[string]any{"kind": "herdr_pi_task", "task": "synthetic new task"}
					if name == "computer_task_followup" {
						args = followupArgs(initial.ID, "bound_session")
					}
					wire := invoke(t, client, name, args, true)
					var response map[string]any
					if err := json.Unmarshal(wire, &response); err != nil {
						t.Fatal(err)
					}
					if response["code"] != admissionErrorCode(failure) || response["business_dispatched"] != false {
						t.Fatalf("missing deterministic failure: %s", wire)
					}
					key := "operation_created"
					if name == "computer_task_followup" {
						key = "request_reserved"
					}
					if response[key] != false {
						t.Fatal("missing rejection facts", response)
					}
				})
			}
			after := store.records[initial.ID]
			if after.Revision != before.Revision || string(after.Details) != string(before.Details) || len(relay.prompts) != 0 {
				t.Fatal("paused input mutated task or prompted Pi")
			}
		})
	}
}
