//go:build integration

package db_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/transferclient"
)

// Actual Server, stable registry verifier, restricted PG role, NAS upload/media
// handlers and disk queue. This Linux upload fixture is not Windows acceptance.
func exerciseProducerHandoff(ctx context.Context, t *testing.T, store *postgresstore.Store, assets *media.Service, id computertask.DeliveryIdentity, ref computertask.DeliveryRequest) {
	t.Helper()
	verifier := newRegisteredTransferVerifier(ctx, t, store, id)
	log := slog.New(slog.DiscardHandler)
	messages := handlers.NewMessageHandler(log, nil, nil, nil, nil)
	messages.SetMediaService(assets)
	h, err := handlers.NewDeviceComputerTransferHandler(ctx, computertask.NewPostgresDeliveryService(store, assets), messages, verifier, log)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	e := server.NewServer(log, ":0", "synthetic-account-secret", h) //nolint:contextcheck // The fixture passes request contexts through the real Server.
	var creates atomic.Int64
	var lose atomic.Bool
	lose.Store(true)
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media/uploads") {
			creates.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/commit") && lose.Swap(false) {
			e.ServeHTTP(httptest.NewRecorder(), r)
			w.WriteHeader(499)
			return
		}
		e.ServeHTTP(w, r)
	}))
	defer https.Close()
	root := t.TempDir()
	config, state, source := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source")
	for _, path := range []string{config, state, source} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": https.URL, "source_root": source})
	for path, raw := range map[string][]byte{filepath.Join(config, "transfer.json"): settings, filepath.Join(config, "device.credential"): []byte(verifier.token), filepath.Join(source, "result-one"): []byte("synthetic delivery content")} {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	closed := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID, TargetID: id.Binding.TargetID, RunID: id.Binding.RunID, Attempt: id.Attempt, Generation: id.Generation, RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "result-one", Mime: ref.Mime}}, Closed: true, SHA256: ref.ContentHash, SizeBytes: ref.SizeBytes}

	// Persist the bounded declaration in the same operation; preserve every
	// existing ledger key while CAS-updating BatchProgress.
	scope := dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID}
	operation, err := store.ReadComputerOperation(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	var details computertask.Details
	var original map[string]json.RawMessage
	if json.Unmarshal(operation.Details, &details) != nil || details.Batch == nil || json.Unmarshal(operation.Details, &original) != nil {
		t.Fatal("original batch absent")
	}
	plan := closed.Plan
	plan.Version = 2
	plan.Artifacts = []deliverymeta.Artifact{plan.Artifact, {SourceRef: "result-two", Mime: ref.Mime}}
	plan.Artifact = deliverymeta.Artifact{}
	details.Batch.Progress.Producer = &plan
	original["batch"], _ = json.Marshal(details.Batch)
	operation.Details, _ = json.Marshal(original)
	if _, err := store.SaveComputerOperation(ctx, operation); err != nil {
		t.Fatal("plan CAS", err)
	}
	raw, _ := json.Marshal(closed)
	getenv := func(key string) string {
		switch key {
		case "HERDR_PLUGIN_CONFIG_DIR":
			return config
		case "HERDR_PLUGIN_STATE_DIR":
			return state
		}
		return ""
	}
	limits := transferclient.QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	status, err := transferclient.EnqueueProducer(ctx, getenv, "fixture", limits, bytes.NewReader(raw), https.Client(), time.Now())
	if err != nil || !status.Registered {
		t.Fatal("stdin admission", err)
	}
	for range 15 {
		status, err = transferclient.AdvanceProducer(ctx, getenv, "fixture", limits, https.Client(), producerNextDue(status))
		if err != nil {
			t.Fatal("finite advance", err)
		}
		if status.Associated {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !status.Associated || status.AssociationBlocked || status.Job.DeliveryID != closed.DeliveryID() || status.Job.OperationID != id.OperationID || creates.Load() != 1 {
		t.Fatal("producer did not recover original identity", status.AssociationState, creates.Load())
	}
	op, err := store.ReadComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Ledger struct {
			Entries map[string]struct {
				Outcome   string
				Reference computertask.DeliveryRequest
			} `json:"entries"`
		} `json:"delivery_ledger"`
	}
	if json.Unmarshal(op.Details, &persisted) != nil || persisted.Ledger.Entries[closed.DeliveryID()].Outcome != "committed" || persisted.Ledger.Entries[closed.DeliveryID()].Reference != ref {
		t.Fatal("NAS ledger reference absent")
	}
	// Replaying the closed handoff after restart uses the existing sealed job.
	replay, err := transferclient.EnqueueProducer(ctx, getenv, "fixture", limits, bytes.NewReader(raw), https.Client(), time.Now())
	if err != nil || !replay.Associated || replay.Job.ReceiptID != status.Job.ReceiptID || creates.Load() != 1 {
		t.Fatal("replayed producer created new work", err)
	}
	// Same installed device credential and original operation, second file. Jobs=1 limits active processing, not metadata registration.
	second := closed
	second.Artifact.SourceRef = "result-two"
	if err := os.WriteFile(filepath.Join(source, second.Artifact.SourceRef), []byte("synthetic delivery content"), 0o600); err != nil {
		t.Fatal(err)
	}
	nextRaw, _ := json.Marshal(second)
	next, err := transferclient.EnqueueProducer(ctx, getenv, "fixture", limits, bytes.NewReader(nextRaw), https.Client(), time.Now())
	if err != nil || !next.Registered {
		t.Fatal("second admission", err)
	}
	for range 15 {
		next, err = transferclient.AdvanceProducer(ctx, getenv, "fixture", limits, https.Client(), producerNextDue(next))
		if err != nil {
			t.Fatal("second advance", err)
		}
		if next.Associated {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !next.Associated || next.PayloadState != transferclient.PayloadReleased || creates.Load() != 2 || next.Job.ReceiptID == status.Job.ReceiptID {
		t.Fatal("distinct original per-file receipts lost", err)
	}
	operation, err = store.ReadComputerOperation(ctx, scope)
	if err != nil || json.Unmarshal(operation.Details, &persisted) != nil {
		t.Fatal("read multiple ledger", err)
	}
	if persisted.Ledger.Entries[closed.DeliveryID()].Outcome != "committed" || persisted.Ledger.Entries[second.DeliveryID()].Outcome != "committed" {
		t.Fatal("partial ledger overwritten")
	}
	for _, metadata := range [][]byte{raw, nextRaw} {
		replay, err := transferclient.EnqueueProducer(ctx, getenv, "fixture", limits, bytes.NewReader(metadata), https.Client(), time.Now())
		if err != nil || !replay.Associated || creates.Load() != 2 {
			t.Fatal("repeated multi-file hook duplicated create", err)
		}
	}
}

// Advance the fixture clock to the persisted retry time, never reset the job's
// retry budget. A fixed future timestamp can still be before the next backoff
// when the asynchronous commit has not finished during the previous call.
func producerNextDue(status transferclient.QueueStatus) time.Time {
	now := time.Now()
	for _, due := range []time.Time{status.Job.NextAttempt, status.AssociationDue} {
		if due.After(now) {
			now = due
		}
	}
	return now
}
