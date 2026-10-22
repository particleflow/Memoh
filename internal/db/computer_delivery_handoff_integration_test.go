//go:build integration

package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/transferclient"
)

type pgGatedDeliveryAssets struct {
	*media.Service
	started chan struct{}
	release chan struct{}
}

func (a *pgGatedDeliveryAssets) OpenSeekable(ctx context.Context, bot, hash string) (media.ReadSeekCloser, media.Asset, error) {
	a.started <- struct{}{}
	select {
	case <-a.release:
	case <-ctx.Done():
		return nil, media.Asset{}, ctx.Err()
	}
	return a.Service.OpenSeekable(ctx, bot, hash)
}

// Runs under the real fixture's NOBYPASSRLS role. Only the incoming execution
// verifier and local destination publication service are synthetic ports.
func exerciseDeliveryHandoff(ctx context.Context, t *testing.T, store *postgresstore.Store, assets *media.Service, id computertask.DeliveryIdentity, ref computertask.DeliveryRequest) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	gated := &pgGatedDeliveryAssets{Service: assets, started: make(chan struct{}, 8), release: make(chan struct{})}
	async, err := computertask.NewAsyncDeliveryService(t.Context(), computertask.NewPostgresDeliveryService(store, gated), func(ctx context.Context, candidate computertask.DeliveryIdentity) error {
		if candidate != id {
			return computertask.ErrDeliveryBinding
		}
		return ctx.Err()
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	defer async.Close()
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(log)
	handlers.NewAsyncComputerDeliveryHandler(async).Register(e)
	messages := handlers.NewMessageHandler(log, nil, nil, bots.NewService(log, postgresstore.NewQueries(store.SQLC())), accounts.NewService(log, store))
	messages.SetMediaService(assets)
	messages.Register(e)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().Header.Get("Authorization") != "Bearer isolated-delivery-fixture" {
				return echo.NewHTTPError(http.StatusForbidden)
			}
			bound, err := toolcontext.WithComputerBinding(c.Request().Context(), id.Binding)
			if err != nil {
				return err
			}
			bound, err = computertask.WithDeliveryIdentity(bound, id)
			if err != nil {
				return err
			}
			c.SetRequest(c.Request().WithContext(bound))
			c.Set("user", &jwt.Token{Valid: true, Claims: jwt.MapClaims{"sub": id.Binding.UserID, "user_id": id.Binding.UserID}})
			return next(c)
		}
	})
	var drop atomic.Bool
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/commit") && drop.Swap(false) {
			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, r) // Real handler/store commit, deliberately lost HTTP reply.
			conn, _, hijackErr := w.(http.Hijacker).Hijack()
			if hijackErr == nil {
				_ = conn.Close()
			}
			return
		}
		e.ServeHTTP(w, r)
	}))
	defer https.Close()
	client, err := transferclient.New(https.URL, https.Client(), func(context.Context) (string, error) { return "Bearer isolated-delivery-fixture", nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	root := t.TempDir()
	open := func() *transferclient.DiskQueue {
		q, err := transferclient.OpenHerdrQueue(func(key string) string {
			if key != "HERDR_PLUGIN_STATE_DIR" {
				t.Fatal(key)
			}
			return root
		}, "fixture-v1", transferclient.QueueLimits{Jobs: 4, Bytes: 8 << 20, JournalBytes: 1 << 20, Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	queue := open()
	defer func() { _ = queue.Close() }()
	step := func(key string, binding *transferclient.AssociationBinding) transferclient.QueueStatus {
		r, err := queue.Inspect(key)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if r.Job.NextAttempt.After(now) {
			now = r.Job.NextAttempt
		}
		if r.AssociationDue.After(now) {
			now = r.AssociationDue
		}
		if err := queue.RunOne(ctx, key, client, binding, now); err != nil {
			t.Fatal(err)
		}
		r, err = queue.Inspect(key)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	job := transferclient.Job{Version: 1, DeliveryID: "http-upload", SnapshotID: "http-upload", BotID: id.Binding.BotID, OperationID: id.OperationID, LocatorRevision: "fixture-v1", Phase: "queued", Spec: transferclient.Spec{SizeBytes: ref.SizeBytes, SHA256: ref.ContentHash, Mime: ref.Mime}}
	source, err := os.CreateTemp(t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	if _, err := source.WriteString("synthetic delivery content"); err != nil {
		t.Fatal(err)
	}
	if err := queue.Admit(ctx, "upload", job, source); err != nil {
		t.Fatal(err)
	}
	binding := client.DeliveryBinding(nil)
	var status transferclient.QueueStatus
	for range 5 {
		status = step(job.DeliveryID, binding)
		if status.Job.Phase == "ready" {
			break
		}
	}
	if status.Job.Phase != "ready" {
		t.Fatalf("upload protocol: %+v", status)
	}
	drop.Store(true)
	status = step(job.DeliveryID, binding)
	if status.AssociationState != transferclient.AssociationUnknown || status.Associated {
		t.Fatalf("lost submit: %+v", status)
	}
	select {
	case <-gated.started:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted scan did not start")
	}
	// Simultaneous execution receipt updates use the same CAS while a scan runs.
	operation, err := store.ReadComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, SessionID: id.Binding.SessionID, UserID: id.Binding.UserID})
	if err != nil {
		t.Fatal(err)
	}
	operation.State = "completed"
	if _, err := store.SaveComputerOperation(ctx, operation); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	queue = open() // Durable association unknown survives queue/process ownership change.
	close(gated.release)
	awaitRemoteDelivery(ctx, t, async, id.OperationID, job.DeliveryID, ref, "committed")
	if status = step(job.DeliveryID, binding); !status.Associated {
		t.Fatalf("reconcile after restart: %+v", status)
	}
	operation, err = store.ReadComputerOperation(ctx, operation)
	if err != nil || operation.State != "completed" {
		t.Fatalf("execution receipt lost: %+v %v", operation, err)
	}

	job.DeliveryID, job.SnapshotID = "http-download", "http-download"
	if err := queue.Admit(ctx, "download", job, nil); err != nil {
		t.Fatal(err)
	}
	pub := &pgLocalPublication{root: t.TempDir(), expected: transferclient.Reference{DeliveryID: job.DeliveryID, OperationID: job.OperationID, BotID: job.BotID, ContentHash: ref.ContentHash, SizeBytes: ref.SizeBytes, Mime: ref.Mime}, lose: true}
	binding = client.DeliveryBinding(pub)
	if status = step(job.DeliveryID, binding); status.Job.Phase != "ready" {
		t.Fatalf("Range/hash download: %+v", status)
	}
	status = step(job.DeliveryID, binding) // OS publish succeeds; callback reply is lost.
	if status.AssociationState != transferclient.AssociationUnknown {
		t.Fatalf("publication unknown: %+v", status)
	}
	if err := queue.RequestCancel(job.DeliveryID); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the fixture port from its disk state; no in-memory winner survives.
	pub = &pgLocalPublication{root: pub.root, expected: pub.expected}
	binding = client.DeliveryBinding(pub)
	status = step(job.DeliveryID, binding)
	if status.Job.Phase == "canceled" {
		t.Fatal("NAS cancel retracted published file")
	}
	download := ref
	download.Direction = "download"
	awaitRemoteDelivery(ctx, t, async, id.OperationID, job.DeliveryID, download, "committed")
	if status = step(job.DeliveryID, binding); !status.Associated || !status.Job.CancelRequested {
		t.Fatalf("publication winner: %+v", status)
	}
	// A local fence made before publication must suppress every later publish.
	fenced := &pgLocalPublication{root: t.TempDir(), expected: pub.expected}
	if state, err := fenced.Resolve(ctx, pub.expected, true); err != nil || state != transferclient.OutcomeCanceled {
		t.Fatalf("local fence: %s %v", state, err)
	}
	if state, err := fenced.Publish(ctx, pub.expected, strings.NewReader("synthetic delivery content")); err != nil || state != transferclient.OutcomeCanceled {
		t.Fatalf("late local publish: %s %v", state, err)
	}
}

func awaitRemoteDelivery(ctx context.Context, t *testing.T, service *computertask.AsyncDeliveryService, operation, key string, ref computertask.DeliveryRequest, want string) {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		out, err := service.Resolve(ctx, operation, key, ref, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if out.Outcome == want {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("waiting for %s: %+v", want, out)
		}
	}
}

// Isolated Linux fixture only, not a Windows destination binding. The real
// destination owner must provide cross-process locking and path/ACL controls.
type pgLocalPublication struct {
	mu       sync.Mutex
	root     string
	expected transferclient.Reference
	lose     bool
}

func (p *pgLocalPublication) Resolve(ctx context.Context, ref transferclient.Reference, cancel bool) (transferclient.AssociationOutcome, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resolve(ctx, ref, cancel)
}

func (p *pgLocalPublication) resolve(ctx context.Context, ref transferclient.Reference, cancel bool) (transferclient.AssociationOutcome, error) {
	if ref != p.expected {
		return "", transferclient.ErrBinding
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(p.root, "published")) // Bounded synthetic payload.
	if err == nil {
		sum := sha256.Sum256(data)
		if int64(len(data)) != ref.SizeBytes || hex.EncodeToString(sum[:]) != ref.ContentHash {
			return "", transferclient.ErrIntegrity
		}
		return transferclient.OutcomeCommitted, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, err = os.Stat(filepath.Join(p.root, "fenced")); err == nil {
		return transferclient.OutcomeCanceled, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if !cancel {
		return transferclient.OutcomeAbsent, nil
	}
	if err := p.persist("fenced", strings.NewReader("fenced")); err != nil {
		return "", err
	}
	return transferclient.OutcomeCanceled, nil
}

func (p *pgLocalPublication) Publish(ctx context.Context, ref transferclient.Reference, data io.Reader) (transferclient.AssociationOutcome, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, err := p.resolve(ctx, ref, false)
	if err != nil || state != transferclient.OutcomeAbsent {
		return state, err
	}
	if err := p.persist("published", data); err != nil {
		return "", err
	}
	if p.lose {
		p.lose = false
		return "", context.DeadlineExceeded
	}
	return p.resolve(ctx, ref, false)
}

func (p *pgLocalPublication) persist(name string, data io.Reader) error {
	f, err := os.CreateTemp(p.root, "pending-")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err := io.Copy(f, io.LimitReader(data, 1024)); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(p.root, name)); err != nil {
		return err
	}
	dir, err := os.Open(p.root)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
