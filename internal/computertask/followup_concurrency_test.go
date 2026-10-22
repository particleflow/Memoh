package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

type followupReadBarrierStore struct {
	dbstore.ComputerOperationStore
	reads   atomic.Int32
	arrived chan int64
	release chan struct{}
}

func (s *followupReadBarrierStore) ReadComputerOperation(ctx context.Context, owner dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	r, err := s.ComputerOperationStore.ReadComputerOperation(ctx, owner)
	if err == nil && s.reads.Add(1) <= 2 {
		// Capture both initial snapshots before either caller can reserve.
		s.arrived <- r.Revision
		select {
		case <-s.release:
		case <-ctx.Done():
			return r, ctx.Err()
		}
	}
	return r, err
}

func TestNativeFollowupSameRevisionCASAndCapacity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing int
		sameID   bool
		rejected bool
		changed  string
	}{
		{name: "same_intent_same_revision", sameID: true},
		{name: "different_intents_same_revision"},
		{name: "same_uuid_different_task_same_revision", existing: 1, sameID: true, changed: "task"},
		{name: "same_uuid_different_mode_same_revision", existing: 1, sameID: true, changed: "mode"},
		{name: "63_to_64_accepted", existing: maxFollowups - 1},
		{name: "63_to_64_rejected_still_consumes_capacity", existing: maxFollowups - 1, rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, store, transport, initial := followupFixture(t, true)
			if tc.rejected {
				transport.fault = "rejected"
			}
			seed := make([]FollowupRecord, tc.existing)
			for i := range seed {
				// Rejected requests count too: this is cumulative capacity,
				// not a limit on accepted or concurrently active requests.
				seed[i] = FollowupRecord{RequestID: uuid.NewString(), Fingerprint: "synthetic-seed", RunID: "earlier-run", Mode: "current_agent", Progress: agenvo.AgentFollowupProgress{State: "rejected"}}
			}
			r := store.records[initial.ID]
			var err error
			r.Details, err = withFollowups(r.Details, seed)
			if err != nil {
				t.Fatal(err)
			}
			store.records[initial.ID] = r
			before, _ := json.Marshal(seed)
			barrier := &followupReadBarrierStore{ComputerOperationStore: store, arrived: make(chan int64, 2), release: make(chan struct{})}
			b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", RunID: "racing-run", TargetID: "pc-a"}
			bound, err := toolcontext.WithComputerBinding(t.Context(), b)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(bound, 10*time.Second)
			var wg sync.WaitGroup
			var release sync.Once
			defer func() { cancel(); release.Do(func() { close(barrier.release) }); wg.Wait() }()
			inputs := [2]Followup{
				{OperationID: initial.ID, RequestID: uuid.NewString(), Mode: "bound_session", Task: "synthetic next instruction"},
				{OperationID: initial.ID, RequestID: uuid.NewString(), Mode: "bound_session", Task: "synthetic next instruction"},
			}
			if tc.sameID {
				inputs[1] = inputs[0]
			}
			switch tc.changed {
			case "task":
				inputs[1].Task = "a different synthetic instruction under the same UUID"
			case "mode":
				inputs[1].Mode = "current_agent"
			}
			type outcome struct {
				result Result
				err    error
			}
			outcomes := make(chan outcome, 2)
			for _, in := range inputs {
				// Separate service instances share only the durable store/relay.
				service := NewService(barrier, store, source.service.relay, nil, source.service.authorize)
				wg.Add(1)
				go func() {
					defer wg.Done()
					got, callErr := service.followup(ctx, b, in)
					outcomes <- outcome{result: got, err: callErr}
				}()
			}
			for range 2 {
				select {
				case revision := <-barrier.arrived:
					if revision != r.Revision {
						t.Fatal("callers did not read the same initial revision")
					}
				case <-ctx.Done():
					t.Fatal("initial-read barrier was not reached")
				}
			}
			release.Do(func() { close(barrier.release) })
			wg.Wait()
			close(outcomes)
			winners, conflicts := 0, 0
			for out := range outcomes {
				if out.result.ID != initial.ID {
					t.Fatal("race replaced original operation")
				}
				switch {
				case tc.changed != "" && errors.Is(out.err, ErrInvalid) && out.result.RequestConflict:
					conflicts++
				case tc.changed == "" && errors.Is(out.err, agenvo.ErrUnknown):
					conflicts++
				case tc.rejected && errors.Is(out.err, agenvo.ErrRejected), !tc.rejected && out.err == nil:
					winners++
				default:
					t.Fatalf("unexpected race outcome: %v, request_conflict=%v", out.err, out.result.RequestConflict)
				}
			}
			if winners != 1 || conflicts != 1 || !slices.Equal(transport.methods, []string{"agent.get", "agent.prompt"}) {
				t.Fatalf("CAS did not select one dispatcher: winners=%d conflicts=%d calls=%v", winners, conflicts, transport.methods)
			}
			var saved Details
			if err := json.Unmarshal(store.records[initial.ID].Details, &saved); err != nil {
				t.Fatal(err)
			}
			if len(saved.Followups) != tc.existing+1 || len(store.records) != 1 {
				t.Fatal("CAS race lost or overfilled the ledger")
			}
			after, _ := json.Marshal(saved.Followups[:tc.existing])
			if string(before) != string(after) {
				t.Fatal("race rewrote earlier request history")
			}
			last := saved.Followups[tc.existing]
			want := "accepted"
			if tc.rejected {
				want = "rejected"
			}
			if last.Progress.State != want {
				t.Fatalf("wrong saved receipt: %s", last.Progress.State)
			}
			restarted := NewService(store, store, source.service.relay, nil, source.service.authorize)
			duplicate := inputs[0]
			loser := inputs[1]
			if last.Fingerprint == fingerprint(inputs[1]) {
				duplicate, loser = inputs[1], inputs[0]
			}
			if last.Fingerprint != fingerprint(duplicate) || last.Mode != duplicate.Mode || !slices.Equal(transport.prompts, []string{duplicate.Task}) {
				t.Fatal("winner's durable intent did not match the single delivered prompt")
			}
			got, duplicateErr := restarted.followup(ctx, b, duplicate)
			if got.Followup == nil || got.Followup.State != want || (duplicateErr != nil) != tc.rejected {
				t.Fatal("restart did not reuse the winning receipt")
			}
			if tc.changed != "" {
				ledger := observationSnapshot(t, store)
				conflict, err := restarted.followup(ctx, b, loser)
				if !errors.Is(err, ErrInvalid) || !conflict.RequestConflict || !slices.Equal(ledger, observationSnapshot(t, store)) {
					t.Fatal("different intent under the same UUID lost its conflict or rewrote the winner")
				}
			}
			if tc.existing == maxFollowups-1 {
				next := duplicate
				next.RequestID = uuid.NewString()
				if _, err := restarted.followup(ctx, b, next); !errors.Is(err, ErrInvalid) {
					t.Fatal("64-entry cumulative capacity was bypassed")
				}
			}
			if !slices.Equal(transport.methods, []string{"agent.get", "agent.prompt"}) {
				t.Fatal("duplicate or capacity check dispatched another input")
			}
		})
	}
}
