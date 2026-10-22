//go:build integration

package db_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
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
	var initialVersion int
	var initialDirty bool
	if err := conn.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&initialVersion, &initialDirty); err != nil || initialVersion < 170 || initialDirty {
		t.Fatalf("initial schema=%d dirty=%v err=%v", initialVersion, initialDirty, err)
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
	exec("UPDATE bot_sessions SET deleted_at=now() WHERE id=$1", child)
	rows, err := s.ListSessionTaskCandidates(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SessionID != root {
		t.Fatalf("deleted ancestor exposed subtree: %v", rows)
	}
	var version int
	var dirty bool
	if err := conn.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != initialVersion || dirty {
		t.Fatalf("schema=%d dirty=%v err=%v", version, dirty, err)
	}
}
