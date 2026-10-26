//go:build integration

package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/transferclient"
)

// Real task HTTP, stable device registry, async validation and formal migrated
// NOBYPASSRLS PostgreSQL. Payloads are deliberately tiny; this is a lifecycle
// bound/recovery contract, not a throughput or physical-power-loss benchmark.
func exerciseReceiptTurnover(ctx context.Context, t *testing.T, store *postgresstore.Store, admin *pgxpool.Pool, original computertask.DeliveryIdentity) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	root := t.TempDir()
	verifier := newRegisteredTransferVerifier(ctx, t, store, original)
	mount, err := store.GetMount(ctx, original.Binding.BotID, original.Binding.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := agenvo.DecodeRuntimeTarget(mount.RuntimeProvider, mount.RuntimeID, mount.RuntimeProviderConfig)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var handler http.Handler
	var active *handlers.ComputerTransferHandler
	var assets *media.Service
	restart := func() {
		mu.Lock()
		defer mu.Unlock()
		if active != nil {
			active.Close()
			_ = assets.Uploads().Close()
		}
		assets = media.NewService(nil, localfs.NewOriginals(root))
		assets.SetRequirePersistence(true)
		if err := assets.EnableUploads(root); err != nil { //nolint:contextcheck // Existing synchronous store constructor has no context parameter.
			t.Fatal(err)
		}
		messages := handlers.NewMessageHandler(log, nil, nil, nil, nil)
		messages.SetMediaService(assets)
		active, err = handlers.NewDeviceComputerTransferHandler(ctx, computertask.NewPostgresDeliveryService(store, assets), messages, verifier, log)
		if err != nil {
			t.Fatal(err)
		}
		handler = server.NewServer(log, ":0", "isolated-account-secret", active) //nolint:contextcheck // HTTP supplies request contexts.
	}
	restart()
	defer func() { active.Close(); _ = assets.Uploads().Close() }()
	lose := false
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if lose && strings.HasSuffix(r.URL.Path, "/complete") {
			lose = false
			handler.ServeHTTP(httptest.NewRecorder(), r)
			w.WriteHeader(499)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer https.Close()
	grant := func(enabled bool) {
		permissions := `["chat"]`
		if enabled {
			permissions = `["chat","workspace_read","workspace_write","workspace_exec"]`
		}
		if _, err := admin.Exec(ctx, "UPDATE bot_user_grants SET permissions=$1 WHERE bot_id=$2", permissions, original.Binding.BotID); err != nil {
			t.Fatal(err)
		}
	}
	defer grant(true)
	retained := 0
	rounds := 2
	if os.Getenv("MEMOH_TEST_RECEIPT_TURNOVER_EXTENDED") == "1" {
		rounds = 12
	}
	for round := range rounds {
		id := original
		id.OperationID, id.Binding.RunID = uuid.NewString(), fmt.Sprintf("turnover-%d", round)
		details, _ := json.Marshal(computertask.Details{Kind: "herdr_pi_task", Batch: &computertask.BatchDetails{Progress: agenvo.BatchProgress{Attempt: id.Attempt, Generation: id.Generation}}, Phase: "submitted"})
		operation, claimed, err := store.ClaimComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID, TargetID: id.Binding.TargetID, RunID: id.Binding.RunID, Fingerprint: id.Binding.RunID, TargetFingerprint: agenvo.TargetFingerprint(target), Details: details})
		if err != nil || !claimed {
			t.Fatal("operation", claimed, err)
		}
		operation.State = "completed"
		if _, err := store.SaveComputerOperation(ctx, operation); err != nil {
			t.Fatal(err)
		}
		for index := range 12 {
			if index%24 == 0 {
				restart()
			}
			body := []byte(fmt.Sprintf("synthetic-turnover-%d-%d", round, index))
			sum := sha256.Sum256(body)
			hash := hex.EncodeToString(sum[:])
			job := transferclient.Job{Version: 1, DeliveryID: fmt.Sprintf("file-%03d", index), OperationID: id.OperationID, BotID: id.Binding.BotID, SnapshotID: "fixture", LocatorRevision: "fixture", Phase: "queued", Spec: transferclient.Spec{SizeBytes: int64(len(body)), SHA256: hash, Mime: "text/plain"}}
			client, err := transferclient.NewDeviceTask(https.URL, job, "upload", pgDeviceTask(id), https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + verifier.token, nil })
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := client.Create(ctx, job.BotID, job.Spec)
			if err != nil {
				t.Fatalf("round %d item %d create: %v", round, index, err)
			}
			recordPath := filepath.Join(root, ".uploads", receipt.ID+".json")
			ref := transferclient.Reference{DeliveryID: job.DeliveryID, OperationID: job.OperationID, BotID: job.BotID, ContentHash: hash, SizeBytes: job.Spec.SizeBytes, Mime: job.Spec.Mime}
			binding := client.DeliveryBinding(nil)
			if index == 0 && round == 0 {
				if err := binding.Commit(ctx, "upload", ref, nil); err == nil {
					t.Fatal("no ready proof committed")
				}
				if _, err := os.Stat(recordPath); err != nil {
					t.Fatal("unconfirmed receipt lost", err)
				}
			}
			if _, err := client.Patch(ctx, job.BotID, receipt.ID, 0, body); err != nil {
				t.Fatal(err)
			}
			if index == 1 && round == 0 {
				mu.Lock()
				lose = true
				mu.Unlock()
			}
			ready, err := client.Complete(ctx, job.BotID, receipt.ID)
			if index == 1 && round == 0 {
				var status *transferclient.HTTPError
				if !errors.As(err, &status) || status.Status != 499 {
					t.Fatal("missing lost response", err)
				}
				restart()
				ready, err = client.Get(ctx, job.BotID, receipt.ID)
			}
			if err != nil || ready.State != "ready" {
				t.Fatal("complete", err)
			}
			if _, err := os.Stat(recordPath); err != nil {
				t.Fatal("ready alone retired", err)
			}
			if index == 2 && round == 0 {
				grant(false)
				var status *transferclient.HTTPError
				if err := binding.Commit(ctx, "upload", ref, nil); !errors.As(err, &status) || status.Status != 403 {
					t.Fatal("revoked commit", err)
				}
				if _, err := os.Stat(recordPath); err != nil {
					t.Fatal("revocation retired evidence", err)
				}
				grant(true)
			}
			if index == 3 && round == 0 {
				if out, err := binding.Resolve(ctx, "upload", ref, true); err != nil || out != transferclient.OutcomeCanceled {
					t.Fatal("cancel", out, err)
				}
				retained++
				if _, err := os.Stat(recordPath); err != nil {
					t.Fatal("cancel erased proof", err)
				}
				client.CloseIdleConnections()
				continue
			}
			var backup []byte
			if index == 4 && round == 0 {
				backup, err = os.ReadFile(recordPath) //nolint:gosec // Exact synthetic receipt backup in this isolated root.
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(recordPath, []byte("corrupt-synthetic-receipt"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err = binding.Commit(ctx, "upload", ref, nil)
			if err != nil && !errors.Is(err, transferclient.ErrTransport) {
				t.Fatal("commit", err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				out, err := binding.Resolve(ctx, "upload", ref, false)
				if len(backup) > 0 && err != nil {
					if got, readErr := os.ReadFile(recordPath); readErr != nil || string(got) != "corrupt-synthetic-receipt" { //nolint:gosec // Same synthetic fixture receipt.
						t.Fatal("damaged evidence erased", readErr)
					}
					// Only restore the exact synthetic backup captured above. A committed
					// DB decision does not justify forging absent or damaged receipt bytes.
					if err := os.WriteFile(recordPath, backup, 0o600); err != nil {
						t.Fatal(err)
					}
					backup = nil
					continue
				}
				if err != nil {
					t.Fatal("resolve", err)
				}
				if out == transferclient.OutcomeCommitted {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("async decision did not finish")
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			if len(backup) > 0 {
				t.Fatal("corrupt cleanup falsely acknowledged")
			}
			if _, err := os.Stat(recordPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("committed receipt not retired", err)
			}
			// Original ID/ref survive only in the authenticated existing ledger.
			if again, err := client.Create(ctx, job.BotID, job.Spec); err != nil || again.ID != receipt.ID || again.State != "ready" {
				t.Fatal("Create replay lost original receipt", err)
			}
			if again, err := client.Complete(ctx, job.BotID, receipt.ID); err != nil || again != ready {
				t.Fatal("Complete replay changed proof", err)
			}
			if err := binding.Commit(ctx, "upload", ref, nil); err != nil {
				t.Fatal("Commit replay", err)
			}
			if _, err := os.Stat(recordPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("replay resurrected staging metadata", err)
			}
			entries, err := os.ReadDir(filepath.Join(root, ".uploads"))
			if err != nil || len(entries) > 65+retained {
				t.Fatal("unbounded staging entries", len(entries), err)
			}
			reader, _, err := assets.Open(ctx, job.BotID, hash)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || string(got) != string(body) {
				t.Fatal("original changed", err)
			}
			client.CloseIdleConnections()
		}
		t.Logf("batch %d completed", round+1)
	}
	t.Logf("%d files, %d operation batches/reopenings; committed staging retired, canceled evidence retained, exact replay and original bytes verified", rounds*12, rounds)
}
