package computertask

import (
	"context"
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

func TestProducerResultRequiresCommittedOriginalLedger(t *testing.T) {
	r := dbstore.ComputerOperationRecord{ID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "fixture"}
	p := &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: r.ID, BotID: r.BotID, UserID: r.UserID, SessionID: r.SessionID, TargetID: r.TargetID, RunID: r.RunID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(`C:\Fixture`)}, Artifact: deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"}}
	d := Details{Kind: "herdr_pi_task", Batch: &BatchDetails{Progress: agenvo.BatchProgress{Attempt: p.Task.Attempt, Generation: p.Task.Generation, Producer: p}, Result: &agenvo.PiJSONResult{Outcome: "succeeded"}}}
	entry := deliveryEntry{Reference: DeliveryRequest{Version: 1, Direction: "upload", ContentHash: strings.Repeat("c", 64), SizeBytes: 7, Mime: "text/plain"}, Scope: strings.Repeat("d", 64), At: time.Now(), Outcome: "pending", Lease: "lease", Until: time.Now().Add(time.Minute), Attempts: 1}
	for _, outcome := range []string{"pending", "committed", "failed", "canceled"} {
		entry.Outcome = outcome
		r.Details, _ = json.Marshal(map[string]any{"delivery_ledger": deliveryLedger{Version: 3, Entries: map[string]deliveryEntry{p.DeliveryID(): entry}}})
		got := result(r, d)
		if got.Delivery == nil || got.PiResult.Outcome != "succeeded" || (got.Delivery.Reference != nil) != (outcome == "committed") {
			t.Fatal("execution became delivery proof", outcome)
		}
	}
	entry.Outcome = "committed"
	r.Details, _ = json.Marshal(map[string]any{"delivery_ledger": deliveryLedger{Version: 3, Entries: map[string]deliveryEntry{"another-delivery": entry}}})
	if result(r, d).Delivery.Reference != nil {
		t.Fatal("unrelated delivery exposed")
	}
	p.Task.OperationID = uuid.NewString()
	if got := result(r, d).Delivery; got.State != "unavailable" || got.Reference != nil {
		t.Fatal("foreign plan exposed")
	}
}

type producerPermissions struct{}

func (producerPermissions) HasBotPermission(context.Context, string, string, string) (bool, error) {
	return true, nil
}

func TestProducerOriginalOperationAndFiniteWake(t *testing.T) {
	source, store, transport, _ := setupBatch(t)
	binding := toolcontext.ComputerBinding{BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "producer-run"}
	mount := store.mounts["pc-a"]
	mount.ID = binding.TargetID
	mount.BotID = binding.BotID
	store.mounts = map[string]dbstore.BotRemoteRuntimeBindingRecord{mount.ID: mount}
	source.service.relay = agenvo.NewService(store, producerPermissions{}, transport.call)
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	input := Submit{Kind: "herdr_pi_task", Task: "write declared result", Artifact: &deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"}}
	first, err := source.service.submit(ctx, binding, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := source.service.submit(ctx, binding, input)
	if err != nil || second.ID != first.ID || transport.runs != 1 {
		t.Fatal("duplicate business submission", err)
	}
	if transport.progress.Producer == nil || transport.progress.Producer.Task.OperationID != first.ID || transport.progress.Producer.Task.RunID != binding.RunID {
		t.Fatal("original operation identity lost")
	}
	var current Result
	for range 5 {
		current, err = source.service.observe(ctx, binding, first.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if current.PiResult != nil {
			break
		}
	}
	if current.PiResult == nil || current.Delivery == nil || current.Delivery.State != "pending" || current.Delivery.Reference != nil || transport.producerWakes == 0 || transport.runs != 1 {
		t.Fatal("business completion or acceptance became delivery")
	}
	wakes := transport.producerWakes
	if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil || transport.producerWakes != wakes+1 || transport.runs != 1 {
		t.Fatal("completed task did not safely wake pending delivery", err)
	}
	transport.changedPane = true
	if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil || transport.producerWakes != wakes+1 {
		t.Fatal("changed control pane was used", err)
	}
	store.revoked = true
	if _, err = source.service.observe(ctx, binding, first.ID, true); err == nil || transport.producerWakes != wakes+1 {
		t.Fatal("revoked target was used")
	}
}
