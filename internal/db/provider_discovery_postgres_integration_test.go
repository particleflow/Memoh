//go:build integration

package db_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/providers"
	"github.com/felinics/memoh/internal/team"
)

func TestProviderDiscoveryReadOnlyTeamIsolation(t *testing.T) {
	ctx := t.Context()
	pool := freshMigratedDB(t)
	const otherTeam = "00000000-0000-0000-0000-0000000000f2"
	const ownProvider = "11111111-1111-4111-8111-111111111111"
	const otherProvider = "22222222-2222-4222-8222-222222222222"
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/models" {
			t.Errorf("unexpected inference request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"fixture-image-edit"}]}`)
	}))
	t.Cleanup(upstream.Close)
	if _, err := pool.Exec(ctx, `INSERT INTO teams (id,slug) VALUES ($1,'discovery-other')`, otherTeam); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]string{"base_url": upstream.URL, "api_key": "synthetic"})
	for _, row := range []struct{ team, id string }{{team.DefaultTeamID, ownProvider}, {otherTeam, otherProvider}} {
		if _, err := pool.Exec(ctx, `INSERT INTO providers (id,team_id,name,client_type,enable,config) VALUES ($1,$2,'discovery-fixture','openai-completions',true,$3)`, row.id, row.team, cfg); err != nil {
			t.Fatal(err)
		}
	}
	conn := rlsConn(t, pool, teamMigrationDSN(t))
	// This transaction is enforced READ ONLY by PostgreSQL, on a role without
	// superuser/RLS bypass. The service must succeed without any database write.
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('memoh.team_id',$1,true)`, team.DefaultTeamID); err != nil {
		t.Fatal(err)
	}
	svc := providers.NewService(nil, postgresstore.NewQueries(sqlc.New(tx)), "")
	_, err = svc.DiscoverModels(ctx, otherProvider)
	if !errors.Is(err, providers.ErrDiscoveryUnavailable) || calls.Load() != 0 {
		t.Fatal("cross-team discovery was not rejected before HTTP", err)
	}
	got, err := svc.DiscoverModels(ctx, ownProvider)
	if err != nil || len(got.Models) != 1 || calls.Load() != 1 {
		t.Fatal("read-only discovery failed", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("discovery attempted a database mutation", err)
	}
}
