package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestBatchHistoryApplicationRestartAndLegacy(t *testing.T) {
	for _, mode := range []string{"verified", "legacy", "receipt_missing", "post_loss", "target_changed"} {
		t.Run(mode, func(t *testing.T) {
			source, store, f, _ := setupBatch(t)
			c := standardClient(t, source, "pc-a", false)
			first := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic first"}, false))
			got := batchResultUntilReady(t, c, first.ID)
			if got.Continuity == nil || got.Continuity.Status != "unconfirmed" || got.Continuity.LastInvocation.After != "created" {
				t.Fatalf("history projection: %+v", got)
			}
			old := store.records[first.ID]
			var d Details
			_ = json.Unmarshal(old.Details, &d)
			switch mode {
			case "legacy":
				d.Batch.Progress.History = nil
				d.Batch.Receipt.History = nil
			case "receipt_missing":
				d.Batch.Receipt = nil
			case "post_loss":
				d.Batch.Receipt.History = &agenvo.BatchHistoryReport{Before: "new", After: "missing"}
			case "target_changed":
				old.TargetFingerprint = "changed"
			}
			old.Details, _ = json.Marshal(d)
			store.records[first.ID] = old
			// Reconstruct application from the same durable records, with a new turn.
			service := NewService(store, store, source.service.relay, nil, source.service.authorize)
			binding := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "run-b"}
			ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
			if err != nil {
				t.Fatal(err)
			}
			next, err := service.submit(ctx, binding, Submit{Kind: "herdr_pi_task", Task: "synthetic followup"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "verified" {
				if f.runs != 2 || f.progress.History.Mode != "resume" || f.progress.History.Evidence.File != "synthetic.jsonl" {
					t.Fatal("lost durable resume evidence")
				}
			} else if f.runs != 1 || next.State != "not_started" || next.Phase != "pi_continuity_unconfirmed" {
				t.Fatalf("unproven history dispatched: %+v runs=%d", next, f.runs)
			}
			repeated, err := service.submit(ctx, binding, Submit{Kind: "herdr_pi_task", Task: "synthetic followup"})
			if err != nil || repeated.ID != next.ID || f.runs > 2 {
				t.Fatal("duplicate or blocked request replayed")
			}
		})
	}
}

type unavailableHistoryStore struct{ *fixtureStore }

func (unavailableHistoryStore) PreviousComputerPiOperation(context.Context, dbstore.ComputerOperationRecord, string) (dbstore.ComputerOperationRecord, error) {
	return dbstore.ComputerOperationRecord{}, errors.New("PRIVATE query error")
}

func TestBatchHistoryReadFailureAndLatestEvidence(t *testing.T) {
	source, store, f, _ := setupBatch(t)
	source.service.store = unavailableHistoryStore{store}
	c := standardClient(t, source, "pc-a", false)
	invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic"}, true)
	if f.runs != 0 || len(f.calls) != 0 || len(store.records) != 1 {
		t.Fatal("failed DB read dispatched or lost reservation")
	}
	for _, r := range store.records {
		if r.State != "unknown" {
			t.Fatal("failed read presented as first use")
		}
	}
}

func TestBatchHistoryLatestUnprovenNeverSearchesPastIt(t *testing.T) {
	source, store, _, _ := setupBatch(t)
	c := standardClient(t, source, "pc-a", false)
	first := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic"}, false))
	batchResultUntilReady(t, c, first.ID)
	prior := store.records[first.ID]
	var d Details
	_ = json.Unmarshal(prior.Details, &d)
	current := prior
	current.ID = "current"
	// A newer legacy row must mask the older verified evidence.
	newer := prior
	newer.ID = "newer"
	newer.CreatedAt = time.Now()
	d.Batch.Progress.History = nil
	d.Batch.Receipt.History = nil
	newer.Details, _ = json.Marshal(d)
	store.records[newer.ID] = newer
	h, err := source.service.batchHistoryExpectation(t.Context(), current, d.Batch.Progress)
	if err != nil || h.Mode != "unconfirmed" || h.Evidence != nil {
		t.Fatalf("invented history: %+v %v", h, err)
	}
}

func TestBatchHistoryRecoveryNeverClaimsCurrentContinuity(t *testing.T) {
	source, store, _, id := recoveryFixture(t)
	r := store.records[id]
	var d Details
	_ = json.Unmarshal(r.Details, &d)
	d.Batch.Progress.History = nil
	d.Batch.Receipt.History = nil
	r.Details, _ = json.Marshal(d)
	store.records[id] = r
	c := standardClient(t, source, "pc-a", false)
	raw := invoke(t, c, "computer_task_recover", map[string]any{"operation_id": id}, false)
	var wire struct {
		Computer Recovery `json:"computer"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	got := wire.Computer
	if got.Continuity == nil || got.Continuity.Status != "unconfirmed" || got.Continuity.Reason != "legacy_evidence_missing" {
		t.Fatalf("legacy recovery invented continuity: %+v", got)
	}
}
