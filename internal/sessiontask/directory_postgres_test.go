package sessiontask

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
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
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
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

func TestDirectoryPostgresExactRunReferences(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
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
	root, child, other, target := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type)VALUES($1,$2,$3,'chat'),($4,$5,$3,'chat')", root, bot, user, target, expert)
	for _, id := range []string{child, other} {
		exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,parent_session_id,type)VALUES($1,$2,$3,$4,'subagent')", id, bot, user, root)
	}
	refs := map[string]dbstore.BotTaskRunReference{}
	addRun := func(b, s, invocation string) string {
		id, turn := uuid.NewString(), uuid.NewString()
		input, _ := json.Marshal(map[string]string{"bot_id": b, "thread_id": s, "agent_id": "fixture"})
		exec("INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,fencing_token)VALUES($1,$2,$3,$4,$5,1,'completed',$6,'synthetic',1)", id, b, s, invocation, turn, input)
		refs[id] = dbstore.BotTaskRunReference{BotID: b, SessionID: s, RunID: id, TurnID: turn, InvocationID: invocation}
		return id
	}
	old := addRun(bot, child, "subagent:old")
	newer := addRun(bot, child, "subagent:newer")
	historical := addRun(bot, other, "subagent:old")
	delegation := uuid.NewString()
	delegatedRun := addRun(expert, target, "bot-delegation:"+delegation)
	exec("INSERT INTO bot_delegations(id,user_id,source_bot_id,source_session_id,source_root_id,target_bot_id,target_session_id,request_id,run_id,fingerprint,source_selection,state)VALUES($1,$2,$3,$4,$4,$5,$6,$7,$8,'synthetic','native','unknown')", delegation, user, bot, root, expert, target, uuid.NewString(), delegatedRun)
	schedule, fire := uuid.NewString(), uuid.NewString()
	exec("INSERT INTO schedule(id,bot_id,name,description,pattern,command,enabled)VALUES($1,$2,'Exact fire','','0 0 * * *','synthetic',false)", schedule, bot)
	scheduledRun := addRun(bot, root, "schedule:"+schedule+":"+fire)
	exec("INSERT INTO schedule_logs(id,schedule_id,bot_id,session_id,status,started_at)VALUES($1,$2,$3,$4,'error',now())", fire, schedule, bot, root)
	computer, opaque := uuid.NewString(), uuid.NewString()
	for _, row := range []struct{ id, run string }{{computer, old}, {opaque, "provider/run:not-a-uuid"}} {
		exec("INSERT INTO computer_operations(id,bot_id,session_id,user_id,target_id,run_id,fingerprint,target_fingerprint,state)VALUES($1,$2,$3,$4,$5,$6,'synthetic','synthetic','unknown')", row.id, bot, child, user, uuid.NewString(), row.run)
	}
	// A later execution fence must not make the historical execution disappear.
	exec("UPDATE bot_sessions SET runtime_fencing_token=100 WHERE id=ANY($1::uuid[])", []string{child, other, target, root})
	store := postgresstore.NewWithQueries(dbsqlc.New(tx))
	for _, id := range []string{old, newer, historical, delegatedRun, scheduledRun} {
		want := refs[id]
		got, err := store.ReadBotTaskRun(ctx, want.BotID, want.SessionID, id)
		if err != nil || got != want {
			t.Fatalf("exact run %s: %+v %v", id, got, err)
		}
	}
	expected := map[string]string{old: old, newer: newer, historical: historical, delegation: delegatedRun, fire: scheduledRun, computer: old, opaque: "provider/run:not-a-uuid"}
	rows, err := store.ListBotTaskDirectoryCandidates(ctx, dbstore.SessionTaskFilter{BotID: bot, UserID: user, SnapshotAt: time.Now().UTC(), Limit: 100})
	if err != nil || len(rows) != len(expected) {
		t.Fatalf("candidates %+v %v", rows, err)
	}
	for _, row := range rows {
		if row.RunID != expected[row.RecordID] {
			t.Fatalf("task identity changed %+v", row)
		}
	}
	incoming, err := store.ListBotTaskDirectoryCandidates(ctx, dbstore.SessionTaskFilter{BotID: expert, UserID: user, SnapshotAt: time.Now().UTC(), Limit: 100})
	if err != nil || len(incoming) != 1 || incoming[0].RecordID != delegation || incoming[0].RunID != delegatedRun {
		t.Fatalf("incoming %+v %v", incoming, err)
	}
	for _, ref := range []dbstore.BotTaskRunReference{{BotID: expert, SessionID: child, RunID: old}, {BotID: bot, SessionID: other, RunID: old}, {BotID: bot, SessionID: child, RunID: uuid.NewString()}, {BotID: bot, SessionID: child, RunID: "provider/run:not-a-uuid"}} {
		if _, err := store.ReadBotTaskRun(ctx, ref.BotID, ref.SessionID, ref.RunID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("invalid association %+v: %v", ref, err)
		}
	}
	exec("UPDATE session_runs SET error_code='history_reset' WHERE run_id=$1", old)
	if _, err := store.ReadBotTaskRun(ctx, bot, child, old); !errors.Is(err, db.ErrNotFound) {
		t.Fatal("reset run is still navigable", err)
	}
	rows, err = store.ListBotTaskDirectoryCandidates(ctx, dbstore.SessionTaskFilter{BotID: bot, UserID: user, SnapshotAt: time.Now().UTC(), Limit: 100})
	if err != nil || len(rows) != len(expected) {
		t.Fatal("invalid link removed historical tasks", err)
	}
	t.Log("exact old/new/cross-session references survive fence changes; schedule occurrence, bidirectional delegation and computer TEXT IDs retain original identity; wrong bot/session/run and reset run rejected; rollback only")
}
