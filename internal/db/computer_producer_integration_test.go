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
	limits := transferclient.QueueLimits{Jobs: 2, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	status, err := transferclient.EnqueueProducer(ctx, getenv, "fixture", limits, bytes.NewReader(raw), https.Client(), time.Now())
	if err != nil || !status.Admitted {
		t.Fatal("stdin admission", err)
	}
	for range 15 {
		status, err = transferclient.AdvanceProducer(ctx, getenv, "fixture", limits, https.Client(), time.Now().Add(time.Hour))
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
}
