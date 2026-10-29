package schedule

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

func TestScheduleOccurrenceResumePostgres(t *testing.T) {
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
	for _, scenario := range []string{"captured_callback_after_resume", "resume_commit_rechecks_waiter", "admitted_fire_continues", "unknown_due_stays_unique", "rename_keeps_registration"} {
		t.Run(scenario, func(t *testing.T) {
			q, user, _, plan := busyOccurrenceFixture(t, ctx, pool)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, e := pool.Exec(ctx, query, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`UPDATE schedule SET max_calls=NULL WHERE id=$1`, plan)
			read := func() sqlc.Schedule {
				t.Helper()
				r, e := q.GetScheduleByID(ctx, toUUID(plan))
				if e != nil {
					t.Fatal(e)
				}
				return r
			}
			creator := &occurrenceCreator{pool: pool}
			trigger := &occurrenceTrigger{pool: pool}
			service := func() *Service {
				return &Service{queries: postgresstore.NewQueriesWithPool(pool, q), triggerer: trigger, sessionCreator: creator, resultHooks: &resultHooksFixture{user: user}, logger: slog.New(slog.DiscardHandler), jwtSecret: "isolated-fixture", cron: cron.New(), jobs: map[string]cron.EntryID{}}
			}
			a := service()
			row := read()
			if e := a.scheduleJob(ctx, row); e != nil {
				t.Fatal(e)
			}
			old := a.cron.Entries()[0]
			due := old.Schedule.(*dueOnce).due
			setEnabled := func(enabled bool) {
				t.Helper()
				if _, e := a.Update(ctx, plan, UpdateRequest{Enabled: &enabled}); e != nil {
					t.Fatal(e)
				}
			}
			assertCounts := func(want int) {
				t.Helper()
				var logs int
				if e := pool.QueryRow(ctx, `SELECT count(*) FROM schedule_logs WHERE schedule_id=$1`, plan).Scan(&logs); e != nil {
					t.Fatal(e)
				}
				if int(creator.calls.Load()) != want || int(trigger.calls.Load()) != want || int(read().CurrentCalls) != want || logs != want {
					t.Fatalf("%s: creators=%d triggers=%d count=%d fires=%d, want %d", scenario, creator.calls.Load(), trigger.calls.Load(), read().CurrentCalls, logs, want)
				}
			}
			switch scenario {
			case "captured_callback_after_resume":
				setEnabled(false)
				setEnabled(true)
				fresh := a.cron.Entries()[0]
				old.Job.Run()
				// A different service with the pre-pause row must also be denied.
				if e := service().runScheduleOccurrence(ctx, toSchedule(row), due.Add(time.Minute)); e != nil {
					t.Fatal(e)
				}
				assertCounts(0)
				if a.jobs[plan] != fresh.ID {
					t.Fatal("late callback replaced resumed registration")
				}
				fresh.Job.Run()
				assertCounts(1)
				// Recreated service uses the persisted current registration. Same due
				// still has exactly one fire, regardless of process or generation.
				if e := service().runScheduleOccurrence(ctx, toSchedule(read()), fresh.Schedule.(*dueOnce).due); e != nil {
					t.Fatal(e)
				}
				assertCounts(1)
			case "resume_commit_rechecks_waiter":
				tx, e := pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
				if _, e = sqlc.New(tx).DisableSchedule(ctx, toUUID(plan)); e != nil {
					t.Fatal(e)
				}
				// Restore enabled inside the same uncommitted transaction: the
				// waiter initially sees enabled=true, and true again after commit.
				if _, e = tx.Exec(ctx, `UPDATE schedule SET enabled=true WHERE id=$1`, plan); e != nil {
					t.Fatal(e)
				}
				done := make(chan error, 1)
				go func() { done <- service().runScheduleOccurrence(ctx, toSchedule(row), due) }()
				for {
					var waiting int
					if e = tx.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted AND transactionid=pg_current_xact_id()::text::xid`).Scan(&waiting); e != nil {
						t.Fatal(e)
					}
					if waiting > 0 {
						break
					}
					select {
					case e = <-done:
						t.Fatalf("callback failed to wait for resume: %v", e)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(5 * time.Millisecond):
					}
				}
				if e = tx.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				if e = <-done; e != nil {
					t.Fatal(e)
				}
				assertCounts(0)
				if e = service().runScheduleOccurrence(ctx, toSchedule(read()), due); e != nil {
					t.Fatal(e)
				}
				assertCounts(1)
			case "admitted_fire_continues":
				creator.entered, creator.release = make(chan struct{}, 1), make(chan struct{})
				var release sync.Once
				defer release.Do(func() { close(creator.release) })
				done := make(chan error, 1)
				go func() { done <- a.runScheduleOccurrence(ctx, toSchedule(row), due) }()
				select {
				case <-creator.entered:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				var identity string
				query := `SELECT jsonb_build_array(id,result_origin,claim_deadline,scheduled_for)::text FROM schedule_logs WHERE schedule_id=$1`
				if e := pool.QueryRow(ctx, query, plan).Scan(&identity); e != nil {
					t.Fatal(e)
				}
				setEnabled(false)
				setEnabled(true)
				release.Do(func() { close(creator.release) })
				if e := <-done; e != nil {
					t.Fatal(e)
				}
				assertCounts(1)
				var after, result string
				if e := pool.QueryRow(ctx, query, plan).Scan(&after); e != nil || after != identity {
					t.Fatal("original fire/origin/budget changed", e)
				}
				if e := pool.QueryRow(ctx, `SELECT result_text FROM schedule_logs WHERE schedule_id=$1`, plan).Scan(&result); e != nil || result != "synthetic saved result" {
					t.Fatal("original result lost", e)
				}
			case "unknown_due_stays_unique":
				fire, e := q.ClaimScheduleOccurrence(ctx, sqlc.ClaimScheduleOccurrenceParams{ScheduleID: row.ID, ScheduledFor: pgtype.Timestamptz{Time: due, Valid: true}})
				if e != nil {
					t.Fatal(e)
				}
				exec(`UPDATE schedule_logs SET claim_state='unknown' WHERE id=$1`, fire.ID)
				setEnabled(false)
				setEnabled(true)
				if e = service().runScheduleOccurrence(ctx, toSchedule(read()), due); e != nil {
					t.Fatal(e)
				}
				var state string
				if e = pool.QueryRow(ctx, `SELECT claim_state FROM schedule_logs WHERE id=$1`, fire.ID).Scan(&state); e != nil || state != "unknown" || creator.calls.Load() != 0 || trigger.calls.Load() != 0 || read().CurrentCalls != 1 {
					t.Fatal("resume reopened unknown", state, e)
				}
			case "rename_keeps_registration":
				name := "renamed without new authorization"
				if _, e := a.Update(ctx, plan, UpdateRequest{Name: &name}); e != nil {
					t.Fatal(e)
				}
				old.Job.Run()
				assertCounts(1)
			}
			t.Log("real PostgreSQL; manually released cron callbacks; no cron started or model called")
		})
	}
}
