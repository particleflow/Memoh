//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	memohdb "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
)

func TestComputerOperationsMigrationAndDurableIsolation(t *testing.T) {
	ctx := context.Background()
	pool := freshMigratedDB(t)
	dsn := teamMigrationDSN(t)
	// Only this freshly-created test database is stepped to 169 and back.
	stepDown(t, dsn, 1)
	stepUp(t, dsn, 1)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); err != nil {
		t.Fatal(err)
	}
	user, bot, session, target := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err = conn.Exec(ctx, "INSERT INTO users(id,username) VALUES($1,'computer-test')", user); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, "INSERT INTO team_members(user_id) VALUES($1)", user); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, "INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'computer-test')", bot, user); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, "INSERT INTO bot_sessions(id,bot_id) VALUES($1,$2)", session, bot); err != nil {
		t.Fatal(err)
	}
	input := dbstore.ComputerOperationRecord{ID: uuid.NewString(), BotID: bot, SessionID: session, UserID: user, TargetID: target, RunID: "synthetic-run", Fingerprint: "same-task", TargetFingerprint: "registration", Details: []byte(`{"kind":"foreground_bash"}`)}
	var created atomic.Int32
	var wg sync.WaitGroup
	var mu sync.Mutex
	var receipts []dbstore.ComputerOperationRecord
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := pool.Acquire(ctx)
			if e != nil {
				t.Error(e)
				return
			}
			defer c.Release()
			if _, e = c.Exec(ctx, "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); e != nil {
				t.Error(e)
				return
			}
			s := postgresstore.NewWithQueries(dbsqlc.New(c))
			r := input
			r.ID = uuid.NewString()
			got, won, e := s.ClaimComputerOperation(ctx, r)
			if e != nil {
				t.Error(e)
				return
			}
			if won {
				created.Add(1)
			}
			mu.Lock()
			receipts = append(receipts, got)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if created.Load() != 1 || len(receipts) != 8 {
		t.Fatalf("created=%d receipts=%d", created.Load(), len(receipts))
	}
	r := receipts[0]
	for _, other := range receipts {
		if other.ID != r.ID {
			t.Fatal("multiple durable submissions")
		}
	}
	store := postgresstore.NewWithQueries(dbsqlc.New(conn))
	connection, alias, unrelated := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, item := range []struct{ id, url string }{{connection, "https://example.invalid/computer"}, {alias, "https://example.invalid/computer/"}, {unrelated, "https://example.invalid/other"}} {
		config, _ := json.Marshal(map[string]string{"url": item.url})
		if _, err = conn.Exec(ctx, "INSERT INTO mcp_connections(id,bot_id,name,type,config) VALUES($1,$2,$3,'http',$4)", item.id, bot, item.id, config); err != nil {
			t.Fatal(err)
		}
	}
	config, _ := json.Marshal(map[string]string{"connection_id": connection})
	runtimeID := uuid.NewString()
	if _, err = conn.Exec(ctx, "INSERT INTO user_runtimes(id,user_id,name,api_token,provider,provider_config,activated_at,pending_expires_at) VALUES($1,$2,'historical-fixture',$3,'agenvo',$4,now(),NULL)", runtimeID, user, uuid.NewString(), config); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{connection, alias, unrelated} {
		managed, e := store.IsManagedComputerConnection(ctx, id)
		if e != nil || managed != (id != unrelated) {
			t.Fatalf("managed alias check: %v %v", managed, e)
		}
	}
	for _, endpoint := range []string{"https://EXAMPLE.invalid:443/computer#alias", "https://example.invalid:443/a/../computer", "https://example.invalid/%63omputer"} {
		raw, _ := json.Marshal(map[string]string{"url": endpoint})
		if _, e := conn.Exec(ctx, "UPDATE mcp_connections SET config=$1 WHERE id=$2", raw, alias); e != nil {
			t.Fatal(e)
		}
		if managed, e := store.IsManagedComputerConnection(ctx, alias); e != nil || !managed {
			t.Fatalf("semantic alias %s: %v %v", endpoint, managed, e)
		}
		if managed, e := store.IsManagedComputerEndpoint(ctx, unrelated, raw); e != nil || !managed {
			t.Fatalf("redirect target %s: %v %v", endpoint, managed, e)
		}
	}
	if _, err = conn.Exec(ctx, "UPDATE user_runtimes SET revoked_at=now() WHERE id=$1", runtimeID); err != nil {
		t.Fatal(err)
	}
	if managed, e := store.IsManagedComputerConnection(ctx, alias); e != nil || !managed {
		t.Fatal("revocation reopened raw transport")
	}
	// The managed credential identity remains reserved at another endpoint.
	for _, id := range []string{connection, alias} {
		raw, _ := json.Marshal(map[string]any{"url": "https://" + id + ".invalid/mcp", "headers": map[string]string{"aUtHoRiZaTiOn": "Bearer synthetic-computer-only"}})
		if _, e := conn.Exec(ctx, "UPDATE mcp_connections SET config=$1 WHERE id=$2", raw, id); e != nil {
			t.Fatal(e)
		}
	}
	if managed, e := store.IsManagedComputerConnection(ctx, alias); e != nil || !managed {
		t.Fatal("credential alias reopened raw route", e)
	}
	old := r
	r.State = "unknown"
	r, err = store.SaveComputerOperation(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveComputerOperation(ctx, old); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	// Reconstructed store represents a process restart; a different run/task
	// cannot create another effect while this target's receipt is uncertain.
	restarted := postgresstore.NewWithQueries(dbsqlc.New(conn))
	retry := input
	retry.ID = uuid.NewString()
	retry.RunID = "next-run"
	retry.Fingerprint = "different task"
	got, won, err := restarted.ClaimComputerOperation(ctx, retry)
	if err != nil || won || got.ID != r.ID || got.State != "unknown" {
		t.Fatalf("restart receipt=%+v won=%v err=%v", got, won, err)
	}
	for _, field := range []string{"bot", "session", "user"} {
		foreign := r
		switch field {
		case "bot":
			foreign.BotID = uuid.NewString()
		case "session":
			foreign.SessionID = uuid.NewString()
		case "user":
			foreign.UserID = uuid.NewString()
		}
		if _, err = store.ReadComputerOperation(ctx, foreign); !errors.Is(err, memohdb.ErrNotFound) {
			t.Fatalf("%s leaked: %v", field, err)
		}
	}
	rlsc := rlsConn(t, pool, dsn)
	if _, err = rlsc.Exec(ctx, "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = rlsc.QueryRow(ctx, "SELECT count(*) FROM computer_operations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("authorized count=%d err=%v", count, err)
	}
	if _, err = rlsc.Exec(ctx, "SELECT set_config('memoh.team_id',$1,false)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err = rlsc.QueryRow(ctx, "SELECT count(*) FROM computer_operations").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cross-team count=%d err=%v", count, err)
	}
	if _, err = rlsc.Exec(ctx, "UPDATE computer_operations SET state='completed' WHERE id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	got, err = store.ReadComputerOperation(ctx, r)
	if err != nil || got.State != "unknown" {
		t.Fatal("foreign tenant changed receipt")
	}
}
