package schedule

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

func TestScheduleOccurrencePausePostgres(t *testing.T) {
	dsn := os.Getenv("MEMOH_SCHEDULE_OCCURRENCE_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := db.OpenPostgresDSN(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := sqlc.New(pool)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	user, bot, plan := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO users(id,username,is_active) VALUES($1,$2,true)`, user, "pause-"+user)
	exec(`INSERT INTO team_members(user_id,role) VALUES($1,'member')`, user)
	exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,$3)`, bot, user, "pause-"+bot)
	defer func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		for _, item := range []struct{ query, id string }{
			{`DELETE FROM bots WHERE id=$1`, bot},
			{`DELETE FROM team_members WHERE user_id=$1`, user},
			{`DELETE FROM users WHERE id=$1`, user},
		} {
			if _, e := pool.Exec(cleanup, item.query, item.id); e != nil {
				t.Error(e)
			}
		}
	}()
	exec(`INSERT INTO schedule(id,bot_id,name,description,pattern,command,enabled,result_origin,max_run_seconds) VALUES($1,$2,'pause fixture','isolated','0 0 1 1 *','no model',true,'{"version":1}',7200)`, plan, bot)
	row, e := q.GetScheduleByID(ctx, toUUID(plan))
	if e != nil {
		t.Fatal(e)
	}
	creator := &occurrenceCreator{pool: pool}
	trigger := &occurrenceTrigger{pool: pool}
	service := func() *Service {
		return &Service{queries: postgresstore.NewQueriesWithPool(pool, q), triggerer: trigger, sessionCreator: creator, resultHooks: &resultHooksFixture{user: user}, logger: slog.New(slog.DiscardHandler), jwtSecret: "isolated-fixture", cron: cron.New(), jobs: map[string]cron.EntryID{}}
	}
	a, b := service(), service()
	// Capture a real cron Job before pausing; cron itself never starts.
	if e = a.scheduleJob(ctx, row); e != nil {
		t.Fatal(e)
	}
	old := a.cron.Entries()[0]
	due := old.Schedule.(*dueOnce).due
	if _, e = q.DisableSchedule(ctx, toUUID(plan)); e != nil {
		t.Fatal(e)
	}
	a.removeJob(plan)
	old.Job.Run()
	var count, logs int
	if e = pool.QueryRow(ctx, `SELECT current_calls,(SELECT count(*) FROM schedule_logs WHERE schedule_id=$1) FROM schedule WHERE id=$1`, plan).Scan(&count, &logs); e != nil {
		t.Fatal(e)
	}
	if creator.calls.Load() != 0 || trigger.calls.Load() != 0 || count != 0 || logs != 0 {
		t.Fatalf("paused captured callback admitted execution: creators=%d triggers=%d count=%d fires=%d", creator.calls.Load(), trigger.calls.Load(), count, logs)
	}

	// Pause has the row lock but is not committed. Other connection callbacks
	// must wait; after commit they must recheck enabled, not their old snapshot.
	exec(`UPDATE schedule SET enabled=true WHERE id=$1`, plan)
	row, e = q.GetScheduleByID(ctx, toUUID(plan))
	if e != nil {
		t.Fatal(e)
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, e = sqlc.New(tx).DisableSchedule(ctx, toUUID(plan)); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 8)
	for i := range 8 {
		go func() {
			s := a
			if i%2 != 0 {
				s = b
			}
			done <- s.runScheduleOccurrence(ctx, toSchedule(row), due.Add(time.Duration(i+1)*time.Minute))
		}()
	}
	// Database lock inspection is a deterministic barrier, not a timing guess.
	for {
		var waiting int
		e = tx.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted AND transactionid=pg_current_xact_id()::text::xid`).Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting > 0 {
			break
		}
		select {
		case e = <-done:
			t.Fatalf("callback did not wait for pause: %v", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	for range 8 {
		if e = <-done; e != nil {
			t.Fatal(e)
		}
	}
	if creator.calls.Load() != 0 || trigger.calls.Load() != 0 {
		t.Fatal("pause lost to stale callback snapshot")
	}
	if e = pool.QueryRow(ctx, `SELECT current_calls,(SELECT count(*) FROM schedule_logs WHERE schedule_id=$1) FROM schedule WHERE id=$1`, plan).Scan(&count, &logs); e != nil || count != 0 || logs != 0 {
		t.Fatal("paused callbacks consumed budget", count, logs, e)
	}

	// Opposite ordering: durable claim wins before pause. Preserve that grant,
	// exact fire/result origin/deadline, and let its original execution finish.
	exec(`UPDATE schedule SET enabled=true WHERE id=$1`, plan)
	row, e = q.GetScheduleByID(ctx, toUUID(plan))
	if e != nil {
		t.Fatal(e)
	}
	creator.entered = make(chan struct{}, 1)
	creator.release = make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(creator.release) })
	finished := make(chan error, 1)
	acceptedDue := due.Add(20 * time.Minute)
	go func() { finished <- a.runScheduleOccurrence(ctx, toSchedule(row), acceptedDue) }()
	select {
	case <-creator.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var fire string
	var deadline time.Time
	var origin []byte
	if e = pool.QueryRow(ctx, `SELECT id,claim_deadline,result_origin FROM schedule_logs WHERE schedule_id=$1`, plan).Scan(&fire, &deadline, &origin); e != nil {
		t.Fatal(e)
	}
	if time.Until(deadline) < 119*time.Minute {
		t.Fatal("original budget shortened")
	}
	if _, e = q.DisableSchedule(ctx, toUUID(plan)); e != nil {
		t.Fatal(e)
	}
	release.Do(func() { close(creator.release) })
	if e = <-finished; e != nil {
		t.Fatal(e)
	}
	if creator.calls.Load() != 1 || trigger.calls.Load() != 1 {
		t.Fatal("pause canceled or duplicated original admission")
	}
	// The exact native run and saved result stay attached, never latest-run.
	var runCount int
	var saved string
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM session_runs WHERE bot_id=$1 AND invocation_id=$2`, bot, "schedule:"+plan+":"+fire).Scan(&runCount); e != nil || runCount != 1 {
		t.Fatal(runCount, e)
	}
	if e = pool.QueryRow(ctx, `SELECT result_text FROM schedule_logs WHERE id=$1`, fire).Scan(&saved); e != nil || saved != "synthetic saved result" {
		t.Fatal(saved, e)
	}
	var afterDeadline time.Time
	var afterOrigin []byte
	if e = pool.QueryRow(ctx, `SELECT claim_deadline,result_origin FROM schedule_logs WHERE id=$1`, fire).Scan(&afterDeadline, &afterOrigin); e != nil || !afterDeadline.Equal(deadline) || string(afterOrigin) != string(origin) {
		t.Fatal("pause changed original identity/budget", e)
	}
	if e = b.runScheduleOccurrence(ctx, toSchedule(row), acceptedDue); e != nil {
		t.Fatal(e)
	}
	if e = b.runScheduleOccurrence(ctx, toSchedule(row), acceptedDue.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	if creator.calls.Load() != 1 || trigger.calls.Load() != 1 {
		t.Fatal("paused replay executed")
	}

	// A previously uncertain occurrence is never reopened by resume/replay.
	exec(`UPDATE schedule SET enabled=true WHERE id=$1`, plan)
	row, e = q.GetScheduleByID(ctx, toUUID(plan))
	if e != nil {
		t.Fatal(e)
	}
	unknownDue := due.Add(30 * time.Minute)
	unknown, e := q.ClaimScheduleOccurrence(ctx, sqlc.ClaimScheduleOccurrenceParams{ScheduleID: toUUID(plan), RegistrationGeneration: row.RegistrationGeneration, ScheduledFor: pgtype.Timestamptz{Time: unknownDue, Valid: true}})
	if e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE schedule_logs SET claim_state='unknown' WHERE id=$1`, unknown.ID)
	if _, e = q.DisableSchedule(ctx, toUUID(plan)); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE schedule SET enabled=true WHERE id=$1`, plan)
	row, e = q.GetScheduleByID(ctx, toUUID(plan))
	if e != nil {
		t.Fatal(e)
	}
	if e = b.runScheduleOccurrence(ctx, toSchedule(row), unknownDue); e != nil {
		t.Fatal(e)
	}
	if creator.calls.Load() != 1 || trigger.calls.Load() != 1 {
		t.Fatal("unknown replayed after pause/resume")
	}
	if _, e = q.ClaimScheduleOccurrence(ctx, sqlc.ClaimScheduleOccurrenceParams{ScheduleID: toUUID(plan), RegistrationGeneration: row.RegistrationGeneration, ScheduledFor: pgtype.Timestamptz{Time: unknownDue, Valid: true}}); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal(e)
	}
	t.Log("captured paused callback: zero fire/count/execution; 8 callbacks/2 services launched against uncommitted pause, database lock waiter observed, all denied; prior claim retains exact native run/result/origin/7200s budget; unknown not replayed; no cron started or model called")
}
