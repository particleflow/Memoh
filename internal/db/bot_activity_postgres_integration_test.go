//go:build integration

package db_test

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	thread "github.com/felinics/memoh/internal/chat/thread"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/team"
)

func TestBotActivityPostgresUnopenedRunsScopeAndReadOnly(t *testing.T) {
	pool := freshMigratedDB(t)
	ctx := t.Context()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	user, bot, otherBot := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO users(id,username)VALUES($1::uuid,$1::text)", user)
	exec("INSERT INTO team_members(user_id)VALUES($1)", user)
	for _, id := range []string{bot, otherBot} {
		exec("INSERT INTO bots(id,owner_user_id,name)VALUES($1::uuid,$2,$1::text)", id, user)
	}
	root, agent, unopened, terminal, reset, foreign := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	at := time.Now().Add(-time.Minute).UTC()
	for _, row := range []struct {
		id, bot, parent, kind, state string
		fence                        int64
	}{{root, bot, "", "chat", "running", 1}, {agent, bot, root, "subagent", "waiting_decision", 1}, {unopened, bot, "", "chat", "accepted", 1}, {terminal, bot, "", "chat", "aborted", 1}, {reset, bot, "", "chat", "running", 1}, {foreign, otherBot, "", "chat", "running", 1}} {
		exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,parent_session_id,type,title,runtime_fencing_token)VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,'Same name',$6)", row.id, row.bot, user, row.parent, row.kind, row.fence)
		exec("INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,fencing_token,created_at)VALUES($1::uuid,$2,$3,$1::text,$4,1,$5,'{}','synthetic',$6,$7)", uuid.NewString(), row.bot, row.id, uuid.NewString(), row.state, row.fence, at)
	}
	exec("UPDATE bot_sessions SET runtime_fencing_token=2 WHERE id=$1", reset)
	registeredAgent := uuid.NewString()
	exec("INSERT INTO bot_agents(id,bot_id,name,runtime)VALUES($1,$2,'Visible Agent','codex')", registeredAgent, bot)
	exec("UPDATE bot_sessions SET bot_agent_id=$2,runtime_type='codex',session_mode='chat' WHERE id=$1", agent, registeredAgent)
	store := postgresstore.NewWithQueries(dbsqlc.New(conn))
	service := sessiontask.NewService(store, thread.NewService(slog.Default(), postgresstore.NewQueries(dbsqlc.New(conn)), nil), nil, nil, "synthetic-activity-cursor")
	var before string
	if err := conn.QueryRow(ctx, "SELECT md5(string_agg(row_to_json(r)::text,'' ORDER BY run_id)) FROM session_runs r").Scan(&before); err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{root: true, agent: true, unopened: true}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := service.ListActivity(ctx, bot, user, 1, cursor, func(s thread.Thread) bool { return s.CreatedByUserID == user })
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if !wanted[item.SessionID] || seen[item.SessionID] {
				t.Fatalf("foreign/reset/terminal/duplicate %+v", item)
			}
			seen[item.SessionID] = true
			if item.SessionID == agent && item.Name != "Visible Agent" {
				t.Fatalf("Agent name lost: %+v", item)
			}
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 3 {
		t.Fatal(seen)
	}
	// Exercise the real handler, JWT parsing and permission services over the
	// same real PG projection, without a device/model/execution dependency.
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.Default())
	e.Use(auth.JWTMiddleware("synthetic-activity-secret", func(echo.Context) bool { return false }))
	handlers.NewSessionTaskHandler(service, bots.NewService(nil, postgresstore.NewQueries(dbsqlc.New(conn))), accounts.NewService(nil, store), nil).Register(e)
	token, _, err := auth.GenerateToken(user, "synthetic-activity-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/bots/"+bot+"/activity", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
	}
	var response sessiontask.ActivityPage
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || len(response.Items) != 3 {
		t.Fatalf("HTTP %+v %v", response, err)
	}
	exec("SELECT set_config('memoh.team_id',$1,false)", uuid.NewString())
	rows, err := store.ListSessionActivityCandidates(ctx, dbstore.SessionTaskFilter{BotID: bot, UserID: user, SnapshotAt: time.Now(), Limit: 100})
	if err != nil || len(rows) != 0 {
		t.Fatalf("cross-team rows=%+v err=%v", rows, err)
	}
	exec("SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	var after string
	if err := conn.QueryRow(ctx, "SELECT md5(string_agg(row_to_json(r)::text,'' ORDER BY run_id)) FROM session_runs r").Scan(&after); err != nil || before != after {
		t.Fatal("activity changed original run rows", err)
	}
}
