//go:build integration

package db_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/transferclient"
)

// Prefix mounting stands in only for a gateway's path rewrite, not NAS/auth/file
// semantics. Real Server handlers, registry verifier, full-migration PG/RLS and
// Media storage handle every request. This is not an Nginx deployment test.
func exerciseFileAPIBase(ctx context.Context, t *testing.T, store *postgresstore.Store, assets *media.Service, id computertask.DeliveryIdentity, request computertask.DeliveryRequest) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	verifier := newRegisteredTransferVerifier(ctx, t, store, id)
	files := handlers.NewMessageHandler(log, nil, nil, nil, nil)
	files.SetMediaService(assets)
	h, err := handlers.NewDeviceComputerTransferHandler(ctx, computertask.NewPostgresDeliveryService(store, assets), files, verifier, log)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	endpoint := server.NewServer(log, ":0", "isolated-api-base", h) //nolint:contextcheck // HTTP requests supply their own contexts.
	var mu sync.Mutex
	paths := map[string]bool{}
	seen := map[apiBaseReadRequest]bool{}
	mounted := http.StripPrefix("/api", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[apiBaseReadRequest{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Range"), r.Header.Get("If-Range")}] = true
		mu.Unlock()
		endpoint.ServeHTTP(w, r)
	}))
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.Method+" "+r.URL.Path] = true
		mu.Unlock()
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		mounted.ServeHTTP(w, r)
	}))
	defer https.Close()
	job := transferclient.Job{Version: 1, DeliveryID: "api-base-prefix", SnapshotID: "api-base-prefix", LocatorRevision: "fixture-v1", Phase: "queued", OperationID: id.OperationID, BotID: id.Binding.BotID, Spec: transferclient.Spec{SizeBytes: request.SizeBytes, SHA256: request.ContentHash, Mime: request.Mime}, Task: pgDeviceTask(id)}
	client, err := transferclient.NewDeviceTask(https.URL+"/api", job, "upload", job.Task, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + verifier.token, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if limits, err := client.Limits(ctx, job.BotID); err != nil || !limits.ResumableEnabled {
		t.Fatal("prefixed limits", err)
	}
	upload, err := client.Create(ctx, job.BotID, job.Spec)
	if err != nil {
		t.Fatal("prefixed create", err)
	}
	if same, err := client.Get(ctx, job.BotID, upload.ID); err != nil || same.ID != upload.ID {
		t.Fatal("prefixed original receipt", err)
	}
	if part, err := client.Patch(ctx, job.BotID, upload.ID, 0, []byte("synthetic delivery content")); err != nil || part.Offset != job.Spec.SizeBytes {
		t.Fatal("prefixed append", err)
	}
	ready, err := client.Complete(ctx, job.BotID, upload.ID)
	if err != nil || ready.ID != upload.ID || ready.ContentHash != job.Spec.SHA256 {
		t.Fatal("prefixed complete", err)
	}
	ref := transferclient.Reference{DeliveryID: job.DeliveryID, OperationID: job.OperationID, BotID: job.BotID, ContentHash: job.Spec.SHA256, SizeBytes: job.Spec.SizeBytes, Mime: job.Spec.Mime}
	binding := client.DeliveryBinding(nil)
	if err := binding.Commit(ctx, "upload", ref, nil); err != nil && !errors.Is(err, transferclient.ErrTransport) {
		t.Fatal("prefixed commit", err)
	}
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		out, err := binding.Resolve(deadline, "upload", ref, false)
		if err != nil {
			t.Fatal("prefixed resolve", err)
		}
		if out == transferclient.OutcomeCommitted {
			break
		}
		if out != transferclient.OutcomePending {
			t.Fatal("unexpected association", out)
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-timer.C:
		case <-deadline.Done():
			timer.Stop()
			t.Fatal("prefixed verification did not finish")
		}
	}
	t.Run("download-range-head-query", func(t *testing.T) {
		exerciseAPIBaseDownload(ctx, t, assets, id, job, https.URL+"/api", https.Client(), verifier.token, func(want apiBaseReadRequest) bool {
			mu.Lock()
			defer mu.Unlock()
			return seen[want]
		})
	})
	if out, err := binding.Resolve(ctx, "upload", ref, false); err != nil || out != transferclient.OutcomeCommitted {
		t.Fatal("downloads changed original upload association", err)
	}
	file := "/api/internal/computer-transfers/" + job.OperationID + "/" + job.DeliveryID + "/bots/" + job.BotID + "/media"
	association := "/api/internal/computer-transfers/" + job.OperationID + "/" + job.DeliveryID + "/internal/computer-deliveries/" + job.OperationID + "/" + job.DeliveryID
	mu.Lock()
	defer mu.Unlock()
	for _, key := range []string{"GET " + file + "/limits", "POST " + file + "/uploads", "GET " + file + "/uploads/" + upload.ID, "PATCH " + file + "/uploads/" + upload.ID, "POST " + file + "/uploads/" + upload.ID + "/complete", "POST " + association + "/commit", "POST " + association + "/resolve"} {
		if !paths[key] {
			t.Fatal("API prefix or task namespace was lost")
		}
	}
	t.Log("configured /api preserved for limits/create/status/append/complete/commit/resolve; real device verifier, PG and file store; same upload receipt and committed reference")
}
