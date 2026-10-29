package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

type busyOccurrenceCreator struct {
	*occurrenceCreator
	occupy atomic.Bool
}

func (c *busyOccurrenceCreator) CreateScheduleSession(ctx context.Context, spec SessionSpec) (string, error) {
	id, err := c.occurrenceCreator.CreateScheduleSession(ctx, spec)
	if err != nil || !c.occupy.Load() {
		return id, err
	}
	run := uuid.NewString()
	_, _, err = ledger.NewPostgres(sqlc.New(c.pool), c.pool).Admit(ctx, ledger.AdmitParams{RunID: run, TurnID: run, BotID: spec.BotID, SessionID: id, InvocationID: "occupying:" + id, Input: []byte(`{}`), InputFingerprint: "busy-fixture"})
	return id, err
}

type busyOccurrenceTrigger struct {
	*occurrenceTrigger
	onBusy func()
}

func (c *busyOccurrenceTrigger) TriggerSchedule(ctx context.Context, bot string, p TriggerPayload, token string) (TriggerResult, error) {
	result, err := c.occurrenceTrigger.TriggerSchedule(ctx, bot, p, token)
	if errors.Is(err, ledger.ErrSessionBusy) {
		if c.onBusy != nil {
			c.onBusy()
		}
		return TriggerResult{}, fmt.Errorf("%w: %w", ErrSessionBusy, err)
	}
	return result, err
}

func TestScheduleOccurrenceBusyPostgres(t *testing.T) {
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
	t.Run("busy_does_not_spend_max_calls_or_disappear", func(t *testing.T) {
		q, user, bot, plan := busyOccurrenceFixture(t, ctx, pool)
		creator := &busyOccurrenceCreator{occurrenceCreator: &occurrenceCreator{pool: pool}}
		creator.occupy.Store(true)
		trigger := &busyOccurrenceTrigger{occurrenceTrigger: &occurrenceTrigger{pool: pool}}
		s := &Service{queries: postgresstore.NewQueriesWithPool(pool, q), triggerer: trigger, sessionCreator: creator, resultHooks: &resultHooksFixture{user: user}, logger: slog.New(slog.DiscardHandler), jwtSecret: "isolated-fixture", cron: cron.New(), jobs: map[string]cron.EntryID{}}
		row, e := q.GetScheduleByID(ctx, toUUID(plan))
		if e != nil {
			t.Fatal(e)
		}
		due := time.Date(2026, 10, 29, 17, 0, 0, 0, time.UTC)
		if e = s.runScheduleOccurrence(ctx, toSchedule(row), due); !errors.Is(e, ErrSessionBusy) {
			t.Fatalf("expected actual native-ledger busy, got %v", e)
		}
		after, e := q.GetScheduleByID(ctx, toUUID(plan))
		if e != nil {
			t.Fatal(e)
		}
		if len(s.jobs) != 1 {
			t.Fatal("automatic limit stop was not re-armed after refund")
		}
		if after.CurrentCalls != 0 || !after.Enabled {
			t.Fatalf("busy consumed unexecuted quota: calls=%d enabled=%v", after.CurrentCalls, after.Enabled)
		}
		var fire, state, status, code string
		var deadline time.Time
		var completed bool
		if e = pool.QueryRow(ctx, `SELECT id,claim_state,status,error_code,claim_deadline,completed_at IS NOT NULL FROM schedule_logs WHERE schedule_id=$1`, plan).Scan(&fire, &state, &status, &code, &deadline, &completed); e != nil {
			t.Fatal(e)
		}
		if state != "busy" || status != "error" || code != "session_runtime.session_busy" || !completed {
			t.Fatalf("busy fire lost or misrepresented: %s %s %s %v", state, status, code, completed)
		}
		if time.Until(deadline) < 119*time.Minute {
			t.Fatal("original budget changed")
		}
		var n int
		if e = pool.QueryRow(ctx, `SELECT count(*) FROM session_runs WHERE bot_id=$1 AND invocation_id=$2`, bot, "schedule:"+plan+":"+fire).Scan(&n); e != nil || n != 0 {
			t.Fatal("busy claimed own run", n, e)
		}
		if e = s.runScheduleOccurrence(ctx, toSchedule(row), due); e != nil {
			t.Fatal(e)
		}
		if creator.calls.Load() != 1 || trigger.calls.Load() != 1 {
			t.Fatal("same fire replayed")
		}
		creator.occupy.Store(false)
		if e = s.runScheduleOccurrence(ctx, toSchedule(row), due.Add(time.Minute)); e != nil {
			t.Fatal(e)
		}
		after, e = q.GetScheduleByID(ctx, toUUID(plan))
		if e != nil || after.CurrentCalls != 1 || after.Enabled {
			t.Fatal("next due did not use exactly the released allowance", after.CurrentCalls, after.Enabled, e)
		}
		if e = s.runScheduleOccurrence(ctx, toSchedule(row), due.Add(2*time.Minute)); e != nil {
			t.Fatal(e)
		}
		if creator.calls.Load() != 2 || trigger.calls.Load() != 2 {
			t.Fatal("max_calls admitted another execution")
		}
		if e = pool.QueryRow(ctx, `SELECT count(*) FROM schedule_logs WHERE schedule_id=$1`, plan).Scan(&n); e != nil || n != 2 {
			t.Fatal("busy history lost", n, e)
		}
		t.Log("real competing native admission -> busy, no own run; quota refunded, automatic max_calls stop released; original fire persists as known non-execution; same due never retried; next due executes once and limit remains enforced")
	})
	for _, scenario := range []string{"explicit_pause_before_refund", "explicit_pause_after_refund", "pause_commit_while_refund_waits", "pause_waits_for_refund_commit", "budget_expired_before_busy_receipt", "unknown_wins", "original_run_exists", "concurrent_refund"} {
		t.Run(scenario, func(t *testing.T) {
			q, user, bot, plan := busyOccurrenceFixture(t, ctx, pool)
			creator := &busyOccurrenceCreator{occurrenceCreator: &occurrenceCreator{pool: pool}}
			creator.occupy.Store(true)
			trigger := &busyOccurrenceTrigger{occurrenceTrigger: &occurrenceTrigger{pool: pool}}
			s := &Service{queries: postgresstore.NewQueriesWithPool(pool, q), triggerer: trigger, sessionCreator: creator, resultHooks: &resultHooksFixture{user: user}, logger: slog.New(slog.DiscardHandler), jwtSecret: "isolated-fixture", cron: cron.New(), jobs: map[string]cron.EntryID{}}
			row, e := q.GetScheduleByID(ctx, toUUID(plan))
			if e != nil {
				t.Fatal(e)
			}
			due := time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			trigger.onBusy = func() { close(entered); <-release }
			defer once.Do(func() { close(release) })
			done := make(chan error, 1)
			go func() { done <- s.runScheduleOccurrence(ctx, toSchedule(row), due) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var fire, session string
			var deadline time.Time
			var origin []byte
			if e = pool.QueryRow(ctx, `SELECT id,session_id,claim_deadline,result_origin FROM schedule_logs WHERE schedule_id=$1`, plan).Scan(&fire, &session, &deadline, &origin); e != nil {
				t.Fatal(e)
			}
			pause := func() {
				t.Helper()
				off := false
				if _, e := s.Update(ctx, plan, UpdateRequest{Enabled: &off}); e != nil {
					t.Fatal(e)
				}
			}
			waitLock := func(tx pgx.Tx) {
				t.Helper()
				for {
					var n int
					if e := tx.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted AND transactionid=pg_current_xact_id()::text::xid`).Scan(&n); e != nil {
						t.Fatal(e)
					}
					if n > 0 {
						return
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(5 * time.Millisecond):
					}
				}
			}
			switch scenario {
			case "explicit_pause_before_refund":
				pause()
			case "pause_commit_while_refund_waits":
				tx, e := pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
				if _, e = sqlc.New(tx).DisableSchedule(ctx, toUUID(plan)); e != nil {
					t.Fatal(e)
				}
				once.Do(func() { close(release) })
				waitLock(tx)
				if e = tx.Commit(ctx); e != nil {
					t.Fatal(e)
				}
			case "pause_waits_for_refund_commit":
				tx, e := pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
				if _, e = sqlc.New(tx).RefundBusyScheduleOccurrence(ctx, sqlc.RefundBusyScheduleOccurrenceParams{ScheduleID: toUUID(plan), BotID: toUUID(bot), FireID: toUUID(fire)}); e != nil {
					t.Fatal(e)
				}
				paused := make(chan error, 1)
				go func() { off := false; _, e := s.Update(ctx, plan, UpdateRequest{Enabled: &off}); paused <- e }()
				waitLock(tx)
				if e = tx.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				if e = <-paused; e != nil {
					t.Fatal(e)
				}
			case "budget_expired_before_busy_receipt":
				deadline = time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
				if _, e = pool.Exec(ctx, `UPDATE schedule_logs SET claim_deadline=$2 WHERE id=$1`, fire, deadline); e != nil {
					t.Fatal(e)
				}
			case "unknown_wins":
				if _, e = pool.Exec(ctx, `UPDATE schedule_logs SET claim_state='unknown' WHERE id=$1`, fire); e != nil {
					t.Fatal(e)
				}
			case "original_run_exists":
				if _, e = pool.Exec(ctx, `UPDATE session_runs SET state='completed' WHERE session_id=$1`, session); e != nil {
					t.Fatal(e)
				}
				run := uuid.NewString()
				if _, fresh, e := ledger.NewPostgres(q, pool).Admit(ctx, ledger.AdmitParams{RunID: run, TurnID: run, BotID: bot, SessionID: session, InvocationID: "schedule:" + plan + ":" + fire, Input: []byte(`{}`), InputFingerprint: "original-proof"}); e != nil || !fresh {
					t.Fatal(fresh, e)
				}
			case "concurrent_refund":
				var wg sync.WaitGroup
				var wins atomic.Int32
				for range 8 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, e := q.RefundBusyScheduleOccurrence(ctx, sqlc.RefundBusyScheduleOccurrenceParams{ScheduleID: toUUID(plan), BotID: toUUID(bot), FireID: toUUID(fire)})
						if e == nil {
							wins.Add(1)
						} else if !errors.Is(e, pgx.ErrNoRows) {
							t.Error(e)
						}
					}()
				}
				wg.Wait()
				if wins.Load() != 1 {
					t.Fatal("refund not CAS", wins.Load())
				}
			}
			once.Do(func() { close(release) })
			if e = <-done; !errors.Is(e, ErrSessionBusy) {
				t.Fatal(e)
			}
			if scenario == "explicit_pause_after_refund" {
				pause()
			}
			after, e := q.GetScheduleByID(ctx, toUUID(plan))
			if e != nil {
				t.Fatal(e)
			}
			var state string
			var afterDeadline time.Time
			var afterOrigin []byte
			if e = pool.QueryRow(ctx, `SELECT claim_state,claim_deadline,result_origin FROM schedule_logs WHERE id=$1`, fire).Scan(&state, &afterDeadline, &afterOrigin); e != nil {
				t.Fatal(e)
			}
			if !deadline.Equal(afterDeadline) || string(origin) != string(afterOrigin) {
				t.Fatal("settlement altered budget/origin")
			}
			if scenario == "unknown_wins" || scenario == "original_run_exists" {
				if after.CurrentCalls != 1 || after.Enabled || state == "busy" {
					t.Fatal("uncertain/admitted fire refunded", after.CurrentCalls, after.Enabled, state)
				}
			} else {
				if after.CurrentCalls != 0 || state != "busy" {
					t.Fatal("known busy count not refunded", after.CurrentCalls, state)
				}
				if scenario != "concurrent_refund" && scenario != "budget_expired_before_busy_receipt" && (after.Enabled || after.LimitDisabled) {
					t.Fatal("refund overrode explicit pause")
				}
			}
			if e = s.runScheduleOccurrence(ctx, toSchedule(row), due); e != nil {
				t.Fatal(e)
			}
			if trigger.calls.Load() != 1 || creator.calls.Load() != 1 {
				t.Fatal("settled or unknown fire replayed")
			}
			t.Log("retained exact fire/session/origin/deadline; no repeated admission;", scenario)
		})
	}
}

func busyOccurrenceFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*sqlc.Queries, string, string, string) {
	t.Helper()
	user, bot, plan := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO users(id,username,is_active) VALUES($1,$2,true)`, user, "busy-"+user)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for _, item := range []struct{ q, id string }{{`DELETE FROM bots WHERE id=$1`, bot}, {`DELETE FROM team_members WHERE user_id=$1`, user}, {`DELETE FROM users WHERE id=$1`, user}} {
			if _, e := pool.Exec(cleanup, item.q, item.id); e != nil {
				t.Error(e)
			}
		}
	})
	exec(`INSERT INTO team_members(user_id,role) VALUES($1,'member')`, user)
	exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,$3)`, bot, user, "busy-"+bot)
	exec(`INSERT INTO schedule(id,bot_id,name,description,pattern,command,enabled,result_origin,max_calls,max_run_seconds) VALUES($1,$2,'busy fixture','isolated','0 0 1 1 *','no model',true,'{"version":1}',1,7200)`, plan, bot)
	return sqlc.New(pool), user, bot, plan
}
