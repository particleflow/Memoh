package sessiontask

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Only transaction-local TEMP tables are written. No scheduler, actual plan,
// application migration or model call runs against the isolated database.
func TestDirectoryPostgresDeletedScheduleKeepsOriginalHistory(t *testing.T) {
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
	exec(`CREATE TEMP TABLE schedule (LIKE public.schedule INCLUDING ALL) ON COMMIT DROP;
 CREATE TEMP TABLE schedule_logs (LIKE public.schedule_logs INCLUDING ALL) ON COMMIT DROP;
 ALTER TABLE schedule_logs ADD FOREIGN KEY(schedule_id) REFERENCES schedule(id) ON DELETE CASCADE;
 CREATE TEMP TABLE session_runs (LIKE public.session_runs INCLUDING ALL) ON COMMIT DROP;
 CREATE TEMP TABLE bot_sessions (LIKE public.bot_sessions INCLUDING ALL) ON COMMIT DROP;`)
	// Start at the old schema even if the test database already has the migration.
	exec("ALTER TABLE schedule DROP COLUMN IF EXISTS deleted_at")
	bot, session, plan, user := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type) VALUES($1,$2,$3,'schedule')", session, bot, user)
	exec("INSERT INTO schedule(id,bot_id,name,description,pattern,command,enabled) VALUES($1,$2,'Original plan','','0 0 * * *','synthetic only',true)", plan, bot)
	fire, failed, unknown, legacy := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	run, later := uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint)
 VALUES($1,$2,$3,$4,$5,1,'completed','{}','synthetic'),($6,$2,$3,'ordinary-later-chat',$7,2,'completed','{}','synthetic')`, run, bot, session, "schedule:"+plan+":"+fire, uuid.NewString(), later, uuid.NewString())
	exec(`INSERT INTO schedule_logs(id,schedule_id,bot_id,session_id,status,completed_at)
 VALUES($1,$2,$3,$4,'ok',now()),($5,$2,$3,NULL,'error',now()),($6,$2,$3,NULL,'ok',NULL),($7,$2,$3,$4,'ok',NULL)`, fire, plan, bot, session, failed, unknown, legacy)
	// A run in a different Bot with a matching invocation is never a valid link.
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint)
 VALUES($1,$2,$3,$4,$5,1,'completed','{}','synthetic')`, uuid.NewString(), uuid.NewString(), uuid.NewString(), "schedule:"+plan+":"+legacy, uuid.NewString())
	migration := func(direction string) {
		t.Helper()
		data, err := os.ReadFile("../../db/postgres/migrations/0180_schedule_history_retention." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		exec(string(data))
	}
	migration("up")
	migration("up")
	exec("UPDATE schedule SET run_target='existing_session',target_session_id=$1 WHERE id=$2", session, plan)
	queries := dbsqlc.New(tx)
	store := postgresstore.NewWithQueries(queries)
	filter := dbstore.SessionTaskFilter{BotID: bot, UserID: user, SnapshotAt: time.Now().Add(time.Minute), Limit: 100}
	read := func() []dbstore.BotTaskDirectoryCandidate {
		t.Helper()
		rows, err := store.ListBotTaskDirectoryCandidates(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	checkSessionLists := func(want int) {
		t.Helper()
		rows, err := queries.ListSessionsByBotPaged(ctx, dbsqlc.ListSessionsByBotPagedParams{BotID: db.ParseUUIDOrEmpty(bot), Types: []string{"schedule"}, HideScheduleRuns: true, LimitCount: 10})
		if err != nil || len(rows) != want {
			t.Fatalf("Bot session list got=%d want=%d error=%v", len(rows), want, err)
		}
		owned, err := queries.ListSessionsByBotAndCreatedByUserPaged(ctx, dbsqlc.ListSessionsByBotAndCreatedByUserPagedParams{BotID: db.ParseUUIDOrEmpty(bot), CreatedByUserID: db.ParseUUIDOrEmpty(user), Types: []string{"schedule"}, HideScheduleRuns: true, LimitCount: 10})
		if err != nil || len(owned) != want {
			t.Fatalf("owned session list got=%d want=%d error=%v", len(owned), want, err)
		}
	}
	checkSessionLists(1)
	before := read()
	if len(before) != 4 {
		t.Fatalf("expected four original fires, got %+v", before)
	}
	for _, row := range before {
		wantState, wantRun := "unknown", ""
		if row.ID == fire {
			wantState, wantRun = "completed", run
		}
		if row.ID == failed {
			wantState = "failed"
		}
		if row.Type != "schedule_fire" || row.ID != row.RecordID || row.ScheduleID != plan || row.SourceBotID != bot || row.State != wantState || row.RunID != wantRun {
			t.Fatalf("invented identity/state or latest/foreign run fallback: %+v", row)
		}
	}
	id := db.ParseUUIDOrEmpty(plan)
	if _, err := queries.DisableSchedule(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got := read(); !reflect.DeepEqual(got, before) {
		t.Fatalf("disabled history changed: %+v", got)
	}
	if err := queries.DeleteSchedule(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got := read(); !reflect.DeepEqual(got, before) {
		t.Fatalf("deleted history changed: %+v", got)
	}
	checkSessionLists(0)
	var enabled, archived bool
	var calls int
	if err := tx.QueryRow(ctx, "SELECT enabled,deleted_at IS NOT NULL,current_calls FROM schedule WHERE id=$1", plan).Scan(&enabled, &archived, &calls); err != nil {
		t.Fatal(err)
	}
	if enabled || !archived || calls != 0 {
		t.Fatalf("archive state enabled=%v archived=%v calls=%d", enabled, archived, calls)
	}
	if err := queries.DeleteSchedule(ctx, id); err != nil {
		t.Fatal(err)
	}
	for name, query := range map[string]func() error{
		"get":           func() error { _, err := queries.GetScheduleByID(ctx, id); return err },
		"late callback": func() error { _, err := queries.IncrementScheduleCalls(ctx, id); return err },
		"disable":       func() error { _, err := queries.DisableSchedule(ctx, id); return err },
		"reenable": func() error {
			_, err := queries.UpdateSchedule(ctx, dbsqlc.UpdateScheduleParams{ID: id, Name: "Reenabled", Description: "", Pattern: "0 0 * * *", Enabled: true, Command: "synthetic", RunTarget: "new_session"})
			return err
		},
	} {
		if err := query(); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("%s accepted deleted plan: %v", name, err)
		}
	}
	if rows, err := queries.ListSchedulesByBot(ctx, db.ParseUUIDOrEmpty(bot)); err != nil || len(rows) != 0 {
		t.Fatalf("deleted configuration listed: %+v %v", rows, err)
	}
	if rows, err := queries.ListEnabledSchedules(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("restart can register deleted plan: %+v %v", rows, err)
	}
	ref, err := store.ReadBotTaskRun(ctx, bot, session, run)
	if err != nil || ref.RunID != run || ref.InvocationID != "schedule:"+plan+":"+fire {
		t.Fatalf("original execution link lost: %+v %v", ref, err)
	}
	foreign := filter
	foreign.BotID = uuid.NewString()
	if rows, err := store.ListBotTaskDirectoryCandidates(ctx, foreign); err != nil || len(rows) != 0 {
		t.Fatalf("history leaked to wrong Bot: %+v %v", rows, err)
	}
	exec("SELECT set_config('memoh.team_id',$1,true)", uuid.NewString())
	if got := read(); len(got) != 0 {
		t.Fatalf("history leaked to wrong team: %+v", got)
	}
	exec("SELECT set_config('memoh.team_id',$1,true)", team.DefaultTeamID)
	// A previously admitted fire can still store its late outcome after deletion.
	if _, err := queries.CompleteScheduleLog(ctx, dbsqlc.CompleteScheduleLogParams{ID: db.ParseUUIDOrEmpty(unknown), Status: "error", ErrorCode: "synthetic_late_failure", Usage: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	afterCompletion := read()
	if len(afterCompletion) != 4 {
		t.Fatal("late completion lost history")
	}
	for _, row := range afterCompletion {
		if row.ID == unknown && row.State != "failed" {
			t.Fatalf("late failure not retained: %+v", row)
		}
	}
	before = afterCompletion
	migration("down")
	migration("down")
	if got := read(); !reflect.DeepEqual(got, before) {
		t.Fatal("rollback deleted history")
	}
	if err := tx.QueryRow(ctx, "SELECT enabled FROM schedule WHERE id=$1", plan).Scan(&enabled); err != nil || enabled {
		t.Fatalf("rollback reenabled plan: %v %v", enabled, err)
	}
	migration("up")
	if got := read(); !reflect.DeepEqual(got, before) {
		t.Fatal("reapply deleted history")
	}
	t.Log("four original fires survive disable/delete/replay/up/down/up; exact historical run retained; unknown/failed/unbound history preserved; no latest/foreign fallback; deleted config cannot be read/reenabled/claimed/bootstrap-registered; Bot/team scope intact; late completion retained; session lists exclude archived target; TEMP transaction rolled back")
}
