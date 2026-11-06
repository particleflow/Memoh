//go:build integration

package db_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/team"
)

func TestSessionTasksPostgresOwnedTreeAndStableCursor(t *testing.T) {
	ctx := t.Context()
	pool := freshMigratedDB(t)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	// These read/list operations must not alter the schema. New independent
	// migrations can advance the starting version without changing that contract.
	var startingVersion int
	var startingDirty bool
	if err := conn.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&startingVersion, &startingDirty); err != nil || startingVersion < 170 || startingDirty {
		t.Fatalf("initial schema=%d dirty=%v err=%v", startingVersion, startingDirty, err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	user, other, bot, otherBot := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{user, other} {
		exec("INSERT INTO users(id,username) VALUES($1::uuid,$1::text)", id)
		exec("INSERT INTO team_members(user_id) VALUES($1)", id)
	}
	for _, id := range []string{bot, otherBot} {
		exec("INSERT INTO bots(id,owner_user_id,name) VALUES($1::uuid,$2,$1::text)", id, user)
	}
	root, child, grand, foreignUser, foreignBot, hiddenGrand, sibling := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	for _, r := range []struct{ id, bot, user, parent, kind string }{
		{root, bot, user, "", "chat"},
		{child, bot, user, root, "subagent"},
		{grand, bot, user, child, "subagent"},
		{foreignUser, bot, other, root, "subagent"},
		{foreignBot, otherBot, user, root, "subagent"},
		{hiddenGrand, bot, user, foreignUser, "subagent"},
		{sibling, bot, user, "", "chat"},
	} {
		exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,parent_session_id,type,created_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6)", r.id, r.bot, r.user, r.parent, r.kind, at)
	}
	wanted := map[string]bool{child: true, grand: true}
	for _, r := range []struct{ session, owner string }{{root, user}, {child, user}, {grand, user}, {foreignUser, user}, {hiddenGrand, user}, {sibling, user}, {root, other}} {
		id := uuid.NewString()
		exec("INSERT INTO computer_operations(id,bot_id,session_id,user_id,target_id,run_id,fingerprint,target_fingerprint,created_at) VALUES($1::uuid,$2,$3,$4,$5,'run',$1::text,'registration',$6)", id, bot, r.session, r.owner, uuid.NewString(), at)
		if (r.session == root || r.session == child || r.session == grand) && r.owner == user {
			wanted[id] = true
		}
	}
	s := postgresstore.NewWithQueries(dbsqlc.New(conn))
	filter := dbstore.SessionTaskFilter{BotID: bot, UserID: user, ParentID: root, SnapshotAt: time.Now().UTC(), Limit: 2}
	seen := map[string]bool{}
	for {
		rows, err := s.ListSessionTaskCandidates(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			if !wanted[r.ID] || seen[r.ID] {
				t.Fatalf("foreign/duplicate row: %+v", r)
			}
			seen[r.ID] = true
			filter.Before = r
		}
		// Late status updates do not move records between pages.
		exec("UPDATE computer_operations SET updated_at=now(),state='unknown' WHERE session_id=$1", grand)
	}
	if len(seen) != len(wanted) {
		t.Fatalf("seen=%v wanted=%v", seen, wanted)
	}
	filter.Before = dbstore.SessionTaskCandidate{}
	filter.Limit = 100
	for _, field := range []string{"user", "bot", "parent", "team"} {
		f := filter
		switch field {
		case "user":
			f.UserID = uuid.NewString()
		case "bot":
			f.BotID = otherBot
		case "parent":
			f.ParentID = sibling
		case "team":
			exec("SELECT set_config('memoh.team_id',$1,false)", uuid.NewString())
		}
		rows, e := s.ListSessionTaskCandidates(ctx, f)
		if e != nil {
			t.Fatal(e)
		}
		if field != "parent" && len(rows) != 0 {
			t.Fatalf("%s leaked %v", field, rows)
		}
		for _, r := range rows {
			if wanted[r.ID] {
				t.Fatal("foreign root leaked task")
			}
		}
		exec("SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	}
	filter.Type = "computer_operation"
	filter.ID = child
	if rows, e := s.ListSessionTaskCandidates(ctx, filter); e != nil || len(rows) != 0 {
		t.Fatal("type discriminator ignored", rows, e)
	}
	filter.Type = ""
	filter.ID = ""
	// Exercise the projection with the real persisted session service too.
	// Direct ownership must not be rewritten to the requested aggregation root.
	service := sessiontask.NewService(s, thread.NewService(slog.Default(), postgresstore.NewQueries(dbsqlc.New(conn)), nil), nil, nil, "synthetic-parent-cursor")
	t.Run("direct-parent-projection", func(t *testing.T) {
		for _, item := range []struct{ query, id, parent string }{
			{root, child, root}, {root, grand, child}, {child, grand, child},
		} {
			got, err := service.Get(ctx, bot, user, item.query, sessiontask.Subagent, item.id)
			if err != nil || got.ParentSessionID != item.query || got.DirectParentSessionID != item.parent || got.SessionID != item.id || got.TaskID != item.id {
				t.Fatalf("persisted relation: %+v %v", got, err)
			}
		}
		exec("SELECT set_config('memoh.team_id',$1,false)", uuid.NewString())
		got, err := service.Get(ctx, bot, user, root, sessiontask.Subagent, grand)
		if !errors.Is(err, sessiontask.ErrUnavailable) || got.DirectParentSessionID != "" || got.SessionID != "" {
			t.Fatalf("cross-team projection leaked: %+v %v", got, err)
		}
		exec("SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	})
	exec("UPDATE bot_sessions SET deleted_at=now() WHERE id=$1", child)
	rows, err := s.ListSessionTaskCandidates(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SessionID != root {
		t.Fatalf("deleted ancestor exposed subtree: %v", rows)
	}
	if got, err := service.Get(ctx, bot, user, root, sessiontask.Subagent, grand); !errors.Is(err, sessiontask.ErrUnavailable) || got.DirectParentSessionID != "" {
		t.Fatalf("deleted ancestor exposed relation: %+v %v", got, err)
	}
	var version int
	var dirty bool
	if err := conn.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != startingVersion || dirty {
		t.Fatalf("schema=%d dirty=%v err=%v", version, dirty, err)
	}
}

func TestSessionTasksPostgresLatestChildRunAndMessage(t *testing.T) {
	ctx := t.Context()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var version int
	var dirty bool
	if err := tx.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || dirty || version < 173 {
		t.Fatalf("requires migrated test DB >=173: %d %v %v", version, dirty, err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("SELECT set_config('memoh.team_id',$1,true)", team.DefaultTeamID)
	user, bot, root, child := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO users(id,username) VALUES($1::uuid,$1::text)", user)
	exec("INSERT INTO team_members(user_id) VALUES($1)", user)
	exec("INSERT INTO bots(id,owner_user_id,name) VALUES($1::uuid,$2,$1::text)", bot, user)
	exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type) VALUES($1,$2,$3,'chat')", root, bot, user)
	exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,parent_session_id,type) VALUES($1,$2,$3,$4,'subagent')", child, bot, user, root)
	s := postgresstore.NewWithQueries(dbsqlc.New(tx))
	addRun := func(session, state string, pos int) string {
		t.Helper()
		id := uuid.NewString()
		exec("INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,created_at) VALUES($1::uuid,$2,$3,($1::uuid)::text,$1::uuid,$4::bigint,$5,'{}','synthetic',now()+($4::bigint * interval '1 second'))", id, bot, session, pos, state)
		return id
	}
	parent := addRun(root, "completed", 1)
	if _, err := s.ReadSubagentRun(ctx, bot, child); !errors.Is(err, db.ErrNotFound) {
		t.Fatal("parent run substituted", parent, err)
	}
	first := addRun(child, "completed", 2)
	exec(`INSERT INTO bot_history_messages(bot_id,session_id,run_id,role,content,turn_id,turn_position,turn_message_seq,turn_visible) VALUES($1,$2,$3,'assistant','{"role":"assistant","content":"CHILD_FIRST"}',$3,2,2,true)`, bot, child, first)
	r, err := s.ReadSubagentRun(ctx, bot, child)
	if err != nil || r.RunID != first || r.State != "completed" || !strings.Contains(string(r.Content), "CHILD_FIRST") {
		t.Fatal(r, err)
	}
	second := addRun(child, "accepted", 3)
	r, err = s.ReadSubagentRun(ctx, bot, child)
	if err != nil || r.RunID != second || r.State != "accepted" || len(r.Content) != 0 {
		t.Fatal("old result reused", r, err)
	}
	exec(`INSERT INTO bot_history_messages(bot_id,session_id,run_id,role,content,turn_id,turn_position,turn_message_seq,turn_visible) VALUES($1,$2,$3,'assistant','{"role":"assistant","content":"CHILD_SECOND"}',$3,3,2,true)`, bot, child, second)
	exec("UPDATE session_runs SET state='completed' WHERE run_id=$1", second)
	// A new reader after completion needs no in-memory task manager.
	s = postgresstore.NewWithQueries(dbsqlc.New(tx))
	r, err = s.ReadSubagentRun(ctx, bot, child)
	if err != nil || r.RunID != second || r.State != "completed" || !strings.Contains(string(r.Content), "CHILD_SECOND") {
		t.Fatal(r, err)
	}
	exec("UPDATE bot_history_messages SET turn_visible=false WHERE run_id=$1", second)
	r, err = s.ReadSubagentRun(ctx, bot, child)
	if err != nil || len(r.Content) != 0 {
		t.Fatal("hidden message leaked", r, err)
	}
	if _, err := s.ReadSubagentRun(ctx, uuid.NewString(), child); !errors.Is(err, db.ErrNotFound) {
		t.Fatal("foreign bot", err)
	}
	exec("SELECT set_config('memoh.team_id',$1,true)", uuid.NewString())
	if _, err := s.ReadSubagentRun(ctx, bot, child); !errors.Is(err, db.ErrNotFound) {
		t.Fatal("foreign team", err)
	}
	exec("SELECT set_config('memoh.team_id',$1,true)", team.DefaultTeamID)
	exec("UPDATE session_runs SET error_code='history_reset' WHERE run_id=$1", second)
	if _, err := s.ReadSubagentRun(ctx, bot, child); !errors.Is(err, db.ErrNotFound) {
		t.Fatal("reset fell back to older run", err)
	}
}
