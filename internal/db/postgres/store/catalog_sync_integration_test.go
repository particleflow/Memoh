package postgresstore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/catalogsync"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

// Uses a dedicated synthetic database, never an existing deployment.
func TestCatalogSyncPostgresIsolation(t *testing.T) {
	dsn := os.Getenv("MEMOH_CATALOG_FIXTURE_DSN")
	if dsn == "" {
		t.Skip("fixture DSN not supplied")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE OR REPLACE FUNCTION public.memoh_current_team_id() RETURNS uuid LANGUAGE sql IMMUTABLE AS $$ SELECT '00000000-0000-0000-0000-000000000001'::uuid $$;
 CREATE TABLE providers (id uuid PRIMARY KEY, team_id uuid NOT NULL DEFAULT public.memoh_current_team_id(), enable bool NOT NULL, config jsonb NOT NULL DEFAULT '{}', client_type text NOT NULL DEFAULT 'openai-completions');
 CREATE TABLE models (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), team_id uuid NOT NULL DEFAULT public.memoh_current_team_id(), provider_id uuid NOT NULL REFERENCES providers(id), model_id text NOT NULL, name text, type text NOT NULL, enable bool NOT NULL, config jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(team_id,provider_id,model_id));
 INSERT INTO providers(id,enable) VALUES ('00000000-0000-0000-0000-000000000101',true)`)
	if err != nil {
		t.Fatal(err)
	}
	q := NewQueriesWithPool(pool, sqlc.New(pool))
	id := "00000000-0000-0000-0000-000000000101"
	record := func(id, config string) catalogsync.Model {
		return catalogsync.Model{ID: id, Name: id, Config: []byte(config)}
	}
	if err = q.ApplyModelCatalog(ctx, id, []byte(`{}`), "openai-completions", []catalogsync.Model{record("a", `{"context_window":100}`), record("b", `{}`)}); err != nil {
		t.Fatal(err)
	}
	// Hold a row lock while an explicit user edit is made. The sync must wait,
	// read the committed new value and keep both the edit and the disabled flag.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `UPDATE models SET enable=false, config=jsonb_set(config,'{context_window}','222'::jsonb) WHERE model_id='a'`)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- q.ApplyModelCatalog(ctx, id, []byte(`{}`), "openai-completions", []catalogsync.Model{record("a", `{"context_window":300}`), record("c", `{"context_window":400}`)})
	}()
	select {
	case err := <-done:
		t.Fatalf("sync bypassed row lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	var enabled bool
	var window int
	var available bool
	if err = pool.QueryRow(ctx, `SELECT enable,(config->>'context_window')::int,(config->>'catalog_available')::bool FROM models WHERE model_id='a'`).Scan(&enabled, &window, &available); err != nil {
		t.Fatal(err)
	}
	if enabled || window != 222 || !available {
		t.Fatalf("user state lost: %v %d %v", enabled, window, available)
	}
	if err = pool.QueryRow(ctx, `SELECT (config->>'catalog_available')::bool FROM models WHERE model_id='b'`).Scan(&available); err != nil || available {
		t.Fatalf("removed row availability=%v err=%v", available, err)
	}
	if err = q.ApplyModelCatalog(ctx, id, []byte(`{}`), "openai-completions", []catalogsync.Model{record("a", `{"context_window":500}`), record("c", `{"context_window":600}`)}); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT (config->>'context_window')::int FROM models WHERE model_id='c'`).Scan(&window); err != nil || window != 600 {
		t.Fatalf("discovered change=%d err=%v", window, err)
	}
	if err = pool.QueryRow(ctx, `SELECT (config->>'context_window')::int FROM models WHERE model_id='a'`).Scan(&window); err != nil || window != 222 {
		t.Fatalf("custom value=%d err=%v", window, err)
	}
	// Invalid duplicate catalog must roll back without marking anything missing.
	if err = q.ApplyModelCatalog(ctx, id, []byte(`{}`), "openai-completions", []catalogsync.Model{record("a", `{}`), record("a", `{}`)}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err = pool.QueryRow(ctx, `SELECT (config->>'catalog_available')::bool FROM models WHERE model_id='c'`).Scan(&available); err != nil || !available {
		t.Fatalf("failed catalog changed rows: available=%v err=%v", available, err)
	}
	// A provider edited while discovery was in flight rejects the stale result.
	if _, err = pool.Exec(ctx, `UPDATE providers SET config = '{"base_url":"https://changed.invalid"}'::jsonb WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = q.ApplyModelCatalog(ctx, id, []byte(`{}`), "openai-completions", nil); err == nil {
		t.Fatal("stale provider config was accepted")
	}
	if err = pool.QueryRow(ctx, `SELECT (config->>'catalog_available')::bool FROM models WHERE model_id='c'`).Scan(&available); err != nil || !available {
		t.Fatalf("stale discovery changed catalog: available=%v err=%v", available, err)
	}
}
