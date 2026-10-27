package agenvo

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

// Run against an isolated migrated test database. All fixtures roll back;
// never use this test with a production database.
func TestPostgresAccountAndBotAuthorization(t *testing.T) {
	dsn := os.Getenv("MEMOH_AGENVO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated migrated PostgreSQL test database")
	}
	ctx := t.Context()
	pool, err := db.OpenPostgresDSN(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	store := postgresstore.NewWithQueries(dbsqlc.New(tx))
	owner, other, botA, botB, connection := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users(id) VALUES($1),($2)", owner, other)
	exec("INSERT INTO team_members(user_id) VALUES($1),($2)", owner, other)
	exec("INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'agenvo-test-a'),($3,$2,'agenvo-test-b')", botA, owner, botB)
	exec("INSERT INTO mcp_connections(id,bot_id,name,type) VALUES($1,$2,'synthetic','http')", connection, botA)
	if _, err := store.OwnedHTTPMCPConnectionBot(ctx, owner, connection); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OwnedHTTPMCPConnectionBot(ctx, other, connection); err == nil {
		t.Fatal("another account shared owner's MCP")
	}
	// An unrelated team must not see the source or the mount, even under the
	// database's development superuser: queries enforce the explicit team scope.
	teamID := uuid.NewString()
	exec("INSERT INTO teams(id) VALUES($1)", teamID)
	exec("SELECT set_config('memoh.team_id',$1,true)", teamID)
	if _, err := store.OwnedHTTPMCPConnectionBot(ctx, owner, connection); err == nil {
		t.Fatal("cross-team source accepted")
	}
	exec("SELECT set_config('memoh.team_id','00000000-0000-0000-0000-000000000001',true)")
	exec("UPDATE mcp_connections SET is_active=false WHERE id=$1", connection)
	if _, err := store.OwnedHTTPMCPConnectionBot(ctx, owner, connection); err == nil {
		t.Fatal("disabled connection accepted")
	}
	exec("UPDATE mcp_connections SET is_active=true WHERE id=$1", connection)
	exec("UPDATE bots SET owner_user_id=$1 WHERE id=$2", other, botA)
	if _, err := store.OwnedHTTPMCPConnectionBot(ctx, owner, connection); err == nil {
		t.Fatal("transferred source accepted")
	}
	exec("UPDATE bots SET owner_user_id=$1 WHERE id=$2", owner, botA)
	exec("UPDATE team_members SET is_active=false WHERE user_id=$1", owner)
	if _, err := store.OwnedHTTPMCPConnectionBot(ctx, owner, connection); err == nil {
		t.Fatal("inactive owner membership accepted")
	}
}
