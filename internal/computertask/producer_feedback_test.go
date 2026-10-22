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
