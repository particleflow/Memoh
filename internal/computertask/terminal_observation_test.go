package computertask

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/computertarget"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

// The host terminal-observation hook fires exactly once, on the save that
// first persists the parsed outcome; re-reading a saved outcome never re-fires.
func TestTerminalObservationFiresOncePerOutcome(t *testing.T) {
	source, store, transport, _ := setupBatch(t)
	source.service.assets = nil
	binding := toolcontext.ComputerBinding{BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "terminal-run"}
	mount := store.mounts["pc-a"]
	mount.ID = binding.TargetID
	mount.BotID = binding.BotID
	store.mounts = map[string]dbstore.BotRemoteRuntimeBindingRecord{mount.ID: mount}
	source.service.relay = computertarget.NewService(store, producerPermissions{}, transport.call)
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var fired []string
	source.service.WithTerminalObservation(func(_ context.Context, r dbstore.ComputerOperationRecord, d Details) {
		mu.Lock()
		defer mu.Unlock()
		fired = append(fired, r.ID+"/"+d.Phase)
	})
	first, err := source.service.submit(ctx, binding, Submit{Kind: "herdr_pi_task", Model: "synthetic-model", Task: "write declared result", Artifact: &deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"}})
	if err != nil {
		t.Fatal(err)
	}
	var current Result
	for range 5 {
		if current, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
			t.Fatal(err)
		}
		if current.PiResult != nil {
			break
		}
	}
	if current.PiResult == nil {
		t.Fatal("fixture did not reach a parsed outcome")
	}
	for range 2 {
		if _, err = source.service.observe(ctx, binding, first.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 1 || !strings.HasPrefix(fired[0], first.ID+"/pi_") {
		t.Fatalf("terminal observation fired: %+v", fired)
	}
}
