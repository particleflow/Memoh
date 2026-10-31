package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/fx"

	"github.com/felinics/memoh/internal/catalogsync"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/providers"
)

type lifecycleCatalogQueries struct {
	dbstore.Queries
	row     sqlc.Provider
	reads   atomic.Int64
	applied chan string
}

func (q *lifecycleCatalogQueries) GetProviderByID(_ context.Context, _ pgtype.UUID) (sqlc.Provider, error) {
	q.reads.Add(1)
	return q.row, nil
}

func (q *lifecycleCatalogQueries) ApplyModelCatalog(_ context.Context, id string, _ []byte, _ string, _ []catalogsync.Model) error {
	q.applied <- id
	return nil
}

func startFixture(t *testing.T, q *lifecycleCatalogQueries, resolver providers.CatalogTargetResolver) *fx.App {
	t.Helper()
	svc := providers.NewService(slog.New(slog.DiscardHandler), q, "")
	options := []fx.Option{fx.Supply(svc), fx.Invoke(startCatalogSync), fx.NopLogger}
	if resolver != nil {
		options = append(options, fx.Supply(resolver))
	}
	app := fx.New(options...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Stop(ctx); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	return app
}

func TestCatalogLifecycleNoResolverDoesNotAccessProvider(t *testing.T) {
	q := &lifecycleCatalogQueries{applied: make(chan string, 1)}
	startFixture(t, q, nil)
	if n := q.reads.Load(); n != 0 {
		t.Fatalf("provider read without resolver: %d", n)
	}
}

func TestCatalogLifecycleEmptyTargetDoesNotAccessProvider(t *testing.T) {
	resolved := make(chan struct{}, 1)
	resolver := providers.CatalogTargetResolver(func(context.Context) (string, error) { resolved <- struct{}{}; return "", nil })
	q := &lifecycleCatalogQueries{applied: make(chan string, 1)}
	startFixture(t, q, resolver)
	select {
	case <-resolved:
	case <-time.After(5 * time.Second):
		t.Fatal("resolver not invoked")
	}
	if n := q.reads.Load(); n != 0 {
		t.Fatalf("provider read for empty target: %d", n)
	}
}

func TestCatalogLifecycleStartsFixtureAndStops(t *testing.T) {
	var posts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts.Add(1)
			http.Error(w, "unexpected", http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture/chat"}]}`))
	}))
	defer server.Close()
	id := pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	q := &lifecycleCatalogQueries{row: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + server.URL + `"}`)}, applied: make(chan string, 1)}
	resolver := providers.CatalogTargetResolver(func(context.Context) (string, error) { return id.String(), nil })
	app := startFixture(t, q, resolver)
	select {
	case got := <-q.applied:
		if got != id.String() {
			t.Fatalf("target=%q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("initial sync not applied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if n := posts.Load(); n != 0 {
		t.Fatalf("inference requests=%d", n)
	}
}

func TestCatalogLifecycleStopCancelsInflightDiscovery(t *testing.T) {
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
		cancelled <- struct{}{}
	}))
	defer server.Close()
	id := pgtype.UUID{Bytes: [16]byte{10}, Valid: true}
	q := &lifecycleCatalogQueries{row: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + server.URL + `"}`)}, applied: make(chan string, 1)}
	app := startFixture(t, q, providers.CatalogTargetResolver(func(context.Context) (string, error) { return id.String(), nil }))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("discovery did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request not cancelled")
	}
	select {
	case <-q.applied:
		t.Fatal("cancelled discovery applied")
	default:
	}
}
