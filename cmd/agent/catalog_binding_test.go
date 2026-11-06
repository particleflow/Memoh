package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/fx"

	"github.com/felinics/memoh/internal/catalogsync"
	"github.com/felinics/memoh/internal/config"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/providers"
)

type bindingFixture struct {
	dbstore.Queries
	provider          sqlc.Provider
	disabled, deleted atomic.Bool
	reads             atomic.Int64
	applied           chan string
}

func (q *bindingFixture) GetProviderByID(_ context.Context, id pgtype.UUID) (sqlc.Provider, error) {
	q.reads.Add(1)
	if q.deleted.Load() || id != q.provider.ID {
		return sqlc.Provider{}, pgx.ErrNoRows
	}
	row := q.provider
	row.Enable = !q.disabled.Load()
	return row, nil
}

func (q *bindingFixture) ApplyModelCatalog(_ context.Context, id string, _ []byte, _ string, _ []catalogsync.Model) error {
	q.applied <- id
	return nil
}

func TestCatalogBindingPersistenceRevocationAndFx(t *testing.T) {
	var gets, posts atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts.Add(1)
			http.Error(w, "forbidden", http.StatusMethodNotAllowed)
			return
		}
		gets.Add(1)
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture/chat"}]}`))
	}))
	defer upstream.Close()
	id := pgtype.UUID{Bytes: [16]byte{77}, Valid: true}
	q := &bindingFixture{provider: sqlc.Provider{ID: id, Enable: true, ClientType: string(models.ClientTypeOpenAICompletions), Config: []byte(`{"base_url":"` + upstream.URL + `"}`)}, applied: make(chan string, 4)}
	log := slog.New(slog.DiscardHandler)
	path := filepath.Join(t.TempDir(), "fixture.toml")
	load := func(target string) config.Config {
		t.Helper()
		if err := os.WriteFile(path, []byte(fmt.Sprintf("[model_catalog_sync]\nprovider_id = %q\n", target)), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	start := func(cfg config.Config) *fx.App {
		t.Helper()
		svc := providers.NewService(log, q, "")
		app := fx.New(fx.Supply(cfg, svc), fx.Provide(provideCatalogTargetResolver), fx.Invoke(startCatalogSync), fx.NopLogger)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return app
	}
	stop := func(app *fx.App) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Stop(ctx); err != nil {
			t.Fatal(err)
		}
	}
	waitApply := func() {
		t.Helper()
		select {
		case got := <-q.applied:
			if got != id.String() {
				t.Fatalf("target=%q", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("binding did not sync")
		}
	}
	cfg := load(id.String())
	app := start(cfg)
	waitApply()
	stop(app)
	// A fresh load and a fresh service simulate restart from the same persistent file.
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	app = start(cfg)
	waitApply()
	stop(app)
	if gets.Load() != 6 || posts.Load() != 0 {
		t.Fatalf("gets=%d posts=%d", gets.Load(), posts.Load())
	}
	// Disabled, deleted, unconfigured, and malformed targets never visit upstream.
	svc := providers.NewService(log, q, "")
	resolve := provideCatalogTargetResolver(cfg, svc)
	q.disabled.Store(true)
	if got, err := resolve(context.Background()); err != nil || got != "" {
		t.Fatalf("disabled got=%q err=%v", got, err)
	}
	q.disabled.Store(false)
	q.deleted.Store(true)
	if got, err := resolve(context.Background()); err != nil || got != "" {
		t.Fatalf("deleted got=%q err=%v", got, err)
	}
	q.deleted.Store(false)
	q.provider.ClientType = string(models.ClientTypeAnthropicMessages)
	if got, err := resolve(context.Background()); err != nil || got != "" {
		t.Fatalf("unsupported client got=%q err=%v", got, err)
	}
	q.provider.ClientType = string(models.ClientTypeOpenAICompletions)
	cfg = load("")
	reads := q.reads.Load()
	resolve = provideCatalogTargetResolver(cfg, svc)
	if got, err := resolve(context.Background()); err != nil || got != "" || q.reads.Load() != reads {
		t.Fatalf("revoked got=%q err=%v reads=%d", got, err, q.reads.Load())
	}
	cfg = load("not-a-uuid")
	resolve = provideCatalogTargetResolver(cfg, svc)
	if got, err := resolve(context.Background()); err == nil || got != "" || q.reads.Load() != reads {
		t.Fatalf("malformed got=%q err=%v", got, err)
	}
	if gets.Load() != 6 || posts.Load() != 0 {
		t.Fatalf("revoked targets accessed upstream: gets=%d posts=%d", gets.Load(), posts.Load())
	}
}
