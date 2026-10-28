package sessiontask

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Uses a rollback-only transaction in a previously migrated isolated database.
// No production migration, schema reset, model, runtime or device is involved.
func TestDirectoryPostgresAllOriginalSourcesAndHistory(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("SELECT set_config('memoh.team_id',$1,true)", team.DefaultTeamID)
	user, bot, expert := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO users(id,username)VALUES($1::uuid,$1::text)", user)
	exec("INSERT INTO team_members(user_id)VALUES($1)", user)
	for _, id := range []string{bot, expert} {
		exec("INSERT INTO bots(id,owner_user_id,name)VALUES($1::uuid,$2,$1::text)", id, user)
	}
	root, child, otherChild := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type,title)VALUES($1,$2,$3,'chat','ordinary conversation')", root, bot, user)
	for _, id := range []string{child, otherChild} {
		exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,parent_session_id,type,title)VALUES($1,$2,$3,$4,'subagent','Same task name')", id, bot, user, root)
	}
	at := time.Now().Add(-time.Minute).UTC()
	expected := map[string]string{}
	addRun := func(session, invocation, state string, input []byte) string {
		id := uuid.NewString()
		exec("INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,created_at)VALUES($1,$2,$3,$4,$5,1,$6,$7,'synthetic',$8)", id, bot, session, invocation, uuid.NewString(), state, input, at)
		return id
	}
	for _, r := range []struct{ session, id, state string }{{child, "bg_original", "failed"}, {child, "bg_second", "completed"}, {otherChild, "bg_original", "lost"}} {
		payload, _ := json.Marshal(map[string]string{"bot_id": bot, "thread_id": r.session, "agent_id": "original-agent", "message": "private task input"})
		expected[addRun(r.session, "subagent:"+r.id, r.state, payload)] = r.id
	}
	addRun(root, "ordinary-chat", "completed", []byte(`{}`))
	addRun(child, "subagent:unproven-prefix", "completed", []byte(`{}`))
	operation := uuid.NewString()
	expected[operation] = operation
	exec("INSERT INTO computer_operations(id,bot_id,session_id,user_id,target_id,run_id,fingerprint,target_fingerprint,state)VALUES($1,$2,$3,$4,$5,'opaque/provider/run:not-a-uuid','synthetic','synthetic','unknown')", operation, bot, root, user, uuid.NewString())
	delegation := uuid.NewString()
	expected[delegation] = delegation
	exec("INSERT INTO bot_delegations(id,user_id,source_bot_id,source_session_id,source_root_id,target_bot_id,request_id,run_id,fingerprint,source_selection,state)VALUES($1,$2,$3,$4,$4,$5,$6,$7,'synthetic','native','unknown')", delegation, user, bot, root, expert, uuid.NewString(), uuid.NewString())
	schedule := uuid.NewString()
	exec("INSERT INTO schedule(id,bot_id,name,description,pattern,command,enabled)VALUES($1,$2,'Nightly job','','0 0 * * *','synthetic',false)", schedule, bot)
	fireStates := map[string]string{}
	for _, fire := range []struct {
		completed bool
		status    string
	}{{false, "error"}, {true, "error"}, {false, "ok"}, {true, "ok"}} {
		id := uuid.NewString()
		expected[id] = id
		fireStates[id] = "unknown"
		if fire.completed {
			fireStates[id] = "completed"
			if fire.status == "error" {
				fireStates[id] = "failed"
			}
		}
		exec("INSERT INTO schedule_logs(id,schedule_id,bot_id,status,started_at,completed_at)VALUES($1,$2,$3,$6,$4,CASE WHEN $5::bool THEN $4::timestamptz ELSE NULL END)", id, schedule, bot, at, fire.completed, fire.status)
	}
	store := postgresstore.NewWithQueries(dbsqlc.New(tx))
	filter := dbstore.SessionTaskFilter{BotID: bot, UserID: user, SnapshotAt: time.Now().UTC(), Limit: 1}
	seen := map[string]bool{}
	kinds := map[string]int{}
	for {
		rows, err := store.ListBotTaskDirectoryCandidates(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		r := rows[0]
		if seen[r.RecordID] || expected[r.RecordID] != r.ID {
			t.Fatalf("lost original identity or invented task %+v", r)
		}
		seen[r.RecordID] = true
		kinds[r.Type]++
		if r.Type == ComputerOperation && r.RunID != "opaque/provider/run:not-a-uuid" {
			t.Fatalf("opaque run changed %+v", r)
		}
		if r.Type == "schedule_fire" && (r.SessionID != "" || r.RunID != "" || r.State != fireStates[r.ID]) {
			t.Fatalf("orphan fire lost %+v", r)
		}
		filter.Before = dbstore.SessionTaskCandidate{Type: r.Type, ID: r.RecordID, CreatedAt: r.CreatedAt}
	}
	if len(seen) != 9 || kinds["native_agent"] != 3 || kinds["schedule_fire"] != 4 {
		t.Fatalf("missing history %+v %+v", seen, kinds)
	}
	incoming, err := store.ListBotTaskDirectoryCandidates(ctx, dbstore.SessionTaskFilter{BotID: expert, UserID: user, SnapshotAt: time.Now().UTC(), Limit: 20})
	if err != nil || len(incoming) != 1 || incoming[0].ID != delegation {
		t.Fatalf("incoming delegation %+v %v", incoming, err)
	}
	denied, err := store.ListBotTaskDirectoryCandidates(ctx, dbstore.SessionTaskFilter{BotID: expert, UserID: uuid.NewString(), SnapshotAt: time.Now().UTC(), Limit: 20})
	if err != nil || len(denied) != 0 {
		t.Fatalf("foreign owner %+v %v", denied, err)
	}
	t.Log("all 9 original task occurrences retained; ordinary/prefix-only runs excluded; incoming delegation and opaque computer run checked")
}
