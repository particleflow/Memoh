package computertask

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestProducerFeedbackStopsUnchangedBlockedWake(t *testing.T) {
	source, store, transport, _ := setupBatch(t)
	binding := toolcontext.ComputerBinding{BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "feedback-fixture"}
	mount := store.mounts["pc-a"]
	mount.ID, mount.BotID = binding.TargetID, binding.BotID
	store.mounts = map[string]dbstore.BotRemoteRuntimeBindingRecord{mount.ID: mount}
	source.service.relay = agenvo.NewService(store, producerPermissions{}, transport.call)
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	first, err := source.service.submit(ctx, binding, Submit{Kind: "herdr_pi_task", Task: "write declared result", Artifact: &deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"}})
	if err != nil {
		t.Fatal(err)
	}
	plan := *transport.progress.Producer
	report := deliverymeta.Feedback{Version: 1, Task: plan.Task, PlanHash: deliverymeta.PlanHash(plan), Files: []deliverymeta.FileFeedback{{DeliveryID: plan.DeliveryID(), State: "blocked", Code: "authorization", Change: strings.Repeat("a", 64)}}}
	setReport := func() { transport.producerFeedback, _ = json.Marshal(report) }
	setReport()
	for range 5 {
		if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if transport.producerWakes != 0 {
		t.Fatalf("unchanged known denial woke transfer %d times", transport.producerWakes)
	}
	// A stale/foreign/partial report cannot clear the durable blocked advisory.
	good := report
	good.Files = append([]deliverymeta.FileFeedback(nil), report.Files...)
	report.Task.Attempt = strings.Repeat("f", 32)
	setReport()
	if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
		t.Fatal(err)
	}
	transport.producerFeedback = []byte(`{"version":1`)
	if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if transport.producerWakes != 0 {
		t.Fatal("invalid receipt cleared block")
	}
	// An explicit local reconciliation changes the original queue record; there
	// is no new business attempt or automatic reset of the rejected delivery.
	report = good
	report.Files = append([]deliverymeta.FileFeedback(nil), good.Files...)
	report.Files[0].State, report.Files[0].Code, report.Files[0].Retryable = "pending", "retry_backoff", true
	report.Files[0].Change = strings.Repeat("b", 64)
	report.Files[0].RetryAt = time.Now().Add(time.Minute)
	setReport()
	if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if transport.producerWakes != 0 {
		t.Fatal("backoff ignored")
	}
	report.Files[0].RetryAt = time.Now().Add(-time.Second)
	setReport()
	if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if transport.producerWakes != 1 || transport.runs != 1 {
		t.Fatal("reconciliation did not resume original delivery")
	}
	report = good
	report.Files = append([]deliverymeta.FileFeedback(nil), good.Files...)
	setReport()
	current, err := source.service.observe(ctx, binding, first.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.Delivery == nil || current.Delivery.State != "blocked" || current.Delivery.Reference != nil {
		t.Fatal("advisory became success")
	}
	canceled, err := source.service.cancel(ctx, binding, first.ID)
	if err != nil || !canceled.CancelRequested {
		t.Fatal("blocked delivery cannot request cancellation", err)
	}
	if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if transport.producerWakes != 1 {
		t.Fatal("canceled delivery woke")
	}
	store.revoked = true
	if _, err = source.service.observe(ctx, binding, first.ID, true); err == nil {
		t.Fatal("revoked target observed")
	}
}

func TestProducerFeedbackConcurrentCommitAndRevocation(t *testing.T) {
	for _, outcome := range []string{"commit", "revoke"} {
		t.Run(outcome, func(t *testing.T) {
			source, store, transport, _ := setupBatch(t)
			binding := toolcontext.ComputerBinding{BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "feedback-cas"}
			mount := store.mounts["pc-a"]
			mount.ID, mount.BotID = binding.TargetID, binding.BotID
			store.mounts = map[string]dbstore.BotRemoteRuntimeBindingRecord{mount.ID: mount}
			source.service.relay = agenvo.NewService(store, producerPermissions{}, transport.call)
			ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
			if err != nil {
				t.Fatal(err)
			}
			first, err := source.service.submit(ctx, binding, Submit{Kind: "herdr_pi_task", Task: "write declared result", Artifact: &deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"}})
			if err != nil {
				t.Fatal(err)
			}
			plan := *transport.progress.Producer
			report := deliverymeta.Feedback{Version: 1, Task: plan.Task, PlanHash: deliverymeta.PlanHash(plan), Files: []deliverymeta.FileFeedback{{DeliveryID: plan.DeliveryID(), State: "blocked", Code: "authorization", Change: strings.Repeat("a", 64)}}}
			transport.producerFeedback, _ = json.Marshal(report)
			for range 5 {
				if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			report.Files[0].State, report.Files[0].Code, report.Files[0].Retryable = "pending", "queued", true
			report.Files[0].Change = strings.Repeat("b", 64)
			transport.producerFeedback, _ = json.Marshal(report)
			transport.onPage = func() {
				transport.onPage = nil
				if outcome == "revoke" {
					store.revoked = true
					return
				}
				store.mu.Lock()
				defer store.mu.Unlock()
				r := store.records[first.ID]
				var details map[string]json.RawMessage
				if json.Unmarshal(r.Details, &details) != nil {
					t.Fatal("details")
				}
				entry := deliveryEntry{Reference: DeliveryRequest{Version: 1, Direction: "upload", ContentHash: strings.Repeat("c", 64), SizeBytes: 7, Mime: "text/plain"}, Scope: strings.Repeat("d", 64), At: time.Now(), Outcome: "committed"}
				details["delivery_ledger"], _ = json.Marshal(deliveryLedger{Version: 3, Entries: map[string]deliveryEntry{plan.DeliveryID(): entry}})
				r.Details, _ = json.Marshal(details)
				r.Revision++
				store.records[first.ID] = r
			}
			got, err := source.service.observe(ctx, binding, first.ID, true)
			if outcome == "revoke" {
				if err == nil {
					t.Fatal("revoked during read accepted")
				}
			} else {
				if err != nil || got.Delivery == nil || got.Delivery.State != "delivered" || got.Delivery.Reference == nil {
					t.Fatal("concurrent committed ledger lost", err)
				}
				if got.Delivery.Retryable || got.Delivery.NeedsAction {
					t.Fatal("stale advisory overrode committed ledger")
				}
				if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			if transport.producerWakes != 0 || transport.runs != 1 {
				t.Fatal("stale observer woke transfer or reexecuted business")
			}
			if outcome == "commit" {
				page := plan
				page.Artifact.SourceRef = "later-page"
				report.Version, report.Page = 2, &page
				report.Files[0].DeliveryID = page.DeliveryID()
				transport.producerFeedback, _ = json.Marshal(report)
				got, err := source.service.observe(ctx, binding, first.ID, true)
				if err != nil || got.Delivery == nil || got.Delivery.Scope != "page" || got.Delivery.State != "pending" || got.Delivery.Reference != nil {
					t.Fatal("completed first page concealed later page", err)
				}
				if transport.producerWakes != 1 || transport.runs != 1 {
					t.Fatal("later page did not wake only transfer")
				}
				var preserved struct {
					Ledger deliveryLedger `json:"delivery_ledger"`
				}
				_ = json.Unmarshal(store.records[first.ID].Details, &preserved)
				if preserved.Ledger.Entries[plan.DeliveryID()].Outcome != "committed" {
					t.Fatal("new page feedback lost earlier committed history")
				}
			}
		})
	}
}

func TestProducerFeedbackPerFileProgressAndUnknown(t *testing.T) {
	for _, deniedPosition := range []int{0, 1} {
		r := dbstore.ComputerOperationRecord{ID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "fixture", Details: dbstore.JSON(`{}`)}
		p := deliverymeta.Plan{Version: 2, Task: deliverymeta.Task{OperationID: r.ID, BotID: r.BotID, UserID: r.UserID, SessionID: r.SessionID, TargetID: r.TargetID, RunID: r.RunID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(`C:\Fixture`)}, Artifacts: []deliverymeta.Artifact{{SourceRef: "first", Mime: "text/plain"}, {SourceRef: "second", Mime: "text/plain"}, {SourceRef: "third", Mime: "text/plain"}}}
		f := &deliverymeta.Feedback{Version: 1, Task: p.Task, PlanHash: deliverymeta.PlanHash(p)}
		for _, file := range p.FilePlans() {
			f.Files = append(f.Files, deliverymeta.FileFeedback{DeliveryID: file.DeliveryID(), State: "pending", Code: "queued", Retryable: true, Change: strings.Repeat("c", 64)})
		}
		f.Files[deniedPosition].State, f.Files[deniedPosition].Code, f.Files[deniedPosition].Retryable = "blocked", "authorization", false
		f.Files[2].State, f.Files[2].Code, f.Files[2].Retryable = "unknown", "create_outcome_unknown", false
		d := Details{Batch: &BatchDetails{Progress: agenvo.BatchProgress{Attempt: p.Task.Attempt, Generation: p.Task.Generation, Producer: &p}, Feedback: f}}
		got := producerResult(r, d)
		if got.Pending != 3 || !got.canAdvance(time.Now()) || !got.NeedsAction || got.Files[deniedPosition].Retryable || got.Files[2].Reference != nil {
			t.Fatal("independent file progress lost")
		}
		waiting := 1 - deniedPosition
		f.Files[waiting].State, f.Files[waiting].Code, f.Files[waiting].Retryable = "blocked", "capacity_blocked", false
		got = producerResult(r, d)
		if got.State != "blocked" || got.canAdvance(time.Now()) || !got.hasPending() {
			t.Fatal("blocked/unknown must stop wakes but remain cancelable")
		}
	}
}
