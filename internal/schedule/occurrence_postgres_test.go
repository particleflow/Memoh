package schedule

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	osexec "os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

type occurrenceCreator struct {
	pool    *pgxpool.Pool
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (*occurrenceCreator) CreateSession(context.Context, string, string) (string, error) {
	return "", errors.New("unexpected legacy creator")
}

func (c *occurrenceCreator) CreateScheduleSession(ctx context.Context, s SessionSpec) (string, error) {
	c.calls.Add(1)
	if c.entered != nil {
		c.entered <- struct{}{}
		<-c.release
	}
	id := uuid.NewString()
	_, err := c.pool.Exec(ctx, `INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type) VALUES($1,$2,$3,'schedule')`, id, s.BotID, s.OwnerUserID)
	return id, err
}

type occurrenceTrigger struct {
	pool  *pgxpool.Pool
	calls atomic.Int32
}

func (c *occurrenceTrigger) TriggerSchedule(ctx context.Context, bot string, p TriggerPayload, _ string) (TriggerResult, error) {
	c.calls.Add(1)
	q := sqlc.New(c.pool)
	id := uuid.NewString()
	_, fresh, err := ledger.NewPostgres(q, c.pool).Admit(ctx, ledger.AdmitParams{RunID: id, TurnID: id, BotID: bot, SessionID: p.SessionID, InvocationID: "schedule:" + p.ID + ":" + p.FireID, Input: []byte(`{}`), InputFingerprint: "occurrence-test"})
	if err != nil {
		return TriggerResult{}, err
	}
	if !fresh {
		return TriggerResult{}, errors.New("unexpected duplicate admission")
	}
	_, err = c.pool.Exec(ctx, `UPDATE session_runs SET state='completed' WHERE run_id=$1`, id)
	return TriggerResult{Status: "ok", Text: "synthetic saved result"}, err
}

func TestScheduleOccurrencePostgresClaimAndRecovery(t *testing.T) {
	dsn := os.Getenv("MEMOH_SCHEDULE_OCCURRENCE_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN required")
	}
	ctx := t.Context()
	open := func() *pgxpool.Pool {
		p, e := db.OpenPostgresDSN(ctx, dsn)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	pool := open()
	defer func() { pool.Close() }()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	user, bot, plan := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO users(id,username,is_active) VALUES($1,$2,true)`, user, "occurrence-"+user)
	exec(`INSERT INTO team_members(user_id,role) VALUES($1,'member')`, user)
	exec(`INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'occurrence-test')`, bot, user)
	defer func() {
		exec(`DELETE FROM bots WHERE id=$1`, bot)
		exec(`DELETE FROM team_members WHERE user_id=$1`, user)
		exec(`DELETE FROM users WHERE id=$1`, user)
	}()
	exec(`INSERT INTO schedule(id,bot_id,name,description,pattern,command,enabled,result_origin) VALUES($1,$2,'fixture','isolated','0 0 1 1 *','no model',true,'{"version":1}')`, plan, bot)
	q := sqlc.New(pool)
	row, err := q.GetScheduleByID(ctx, toUUID(plan))
	if err != nil {
		t.Fatal(err)
	}
	creator := &occurrenceCreator{pool: pool, entered: make(chan struct{}, 16), release: make(chan struct{})}
	trigger := &occurrenceTrigger{pool: pool}
	var release sync.Once
	defer release.Do(func() { close(creator.release) })
	newService := func() *Service {
		return &Service{queries: postgresstore.NewQueriesWithPool(pool, sqlc.New(pool)), triggerer: trigger, sessionCreator: creator, resultHooks: &resultHooksFixture{user: user}, logger: slog.New(slog.DiscardHandler), jwtSecret: "isolated-fixture", cron: cron.New(), jobs: map[string]cron.EntryID{}}
	}
	a, b := newService(), newService()
	due := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	done := make(chan error, 16)
	for i := range 16 {
		go func() {
			<-start
			s := a
			if i%2 != 0 {
				s = b
			}
			done <- s.runScheduleOccurrence(ctx, toSchedule(row), due)
		}()
	}
	close(start)
	select {
	case <-creator.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no claimant")
	}
	for range 15 {
		select {
		case e := <-done:
			if e != nil {
				t.Fatal(e)
			}
		case <-creator.entered:
			release.Do(func() { close(creator.release) })
			for range 16 {
				select {
				case <-done:
				case <-time.After(time.Second):
				}
			}
			t.Fatal("same due occurrence created multiple execution contexts")
		case <-time.After(5 * time.Second):
			t.Fatal("duplicate callback did not return without admission")
		}
	}
	if creator.calls.Load() != 1 || trigger.calls.Load() != 0 {
		t.Fatal("duplicate callback crossed pre-admission barrier")
	}
	release.Do(func() { close(creator.release) })
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if creator.calls.Load() != 1 || trigger.calls.Load() != 1 {
		t.Fatal("multiple execution admissions")
	}
	var count int
	if e := pool.QueryRow(ctx, `SELECT current_calls FROM schedule WHERE id=$1`, plan).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate counting", count, e)
	}
	// Reopen DB connections and service after original admission; same due must
	// retain the same fire and must not touch the executor again.
	pool.Close()
	pool = open()
	q = sqlc.New(pool)
	creator.pool = pool
	trigger.pool = pool
	if e := newService().runScheduleOccurrence(ctx, toSchedule(row), due); e != nil {
		t.Fatal(e)
	}
	if creator.calls.Load() != 1 || trigger.calls.Load() != 1 {
		t.Fatal("restart replayed occurrence")
	}
	claim := func(at time.Time) sqlc.ClaimScheduleOccurrenceRow {
		t.Helper()
		r, e := q.ClaimScheduleOccurrence(ctx, sqlc.ClaimScheduleOccurrenceParams{ScheduleID: toUUID(plan), ScheduledFor: pgtype.Timestamptz{Time: at, Valid: true}})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	// A separate Go test process commits the real claim then blocks before
	// session creation. Kill it to preserve the precise crash boundary.
	cmd := osexec.CommandContext(ctx, os.Args[0], "-test.run=^TestScheduleOccurrenceClaimProcess$") //nolint:gosec // G204: only the running test binary and a constant test selector; no user input.
	cmd.Env = append(os.Environ(), "MEMOH_OCCURRENCE_CHILD_PLAN="+plan, "MEMOH_OCCURRENCE_CHILD_DUE="+due.Add(time.Minute).Format(time.RFC3339Nano))
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	committed := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "CLAIM:") {
				committed <- strings.TrimPrefix(scanner.Text(), "CLAIM:")
				return
			}
		}
	}()
	var orphan sqlc.ClaimScheduleOccurrenceRow
	select {
	case id := <-committed:
		orphan.ID = toUUID(id)
	case <-time.After(10 * time.Second):
		t.Fatal("child did not commit fire claim")
	}
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	if e = cmd.Wait(); e == nil {
		t.Fatal("expected killed claim process")
	}

	live := claim(due.Add(2 * time.Minute))
	// The original execution budget protects still-live claims. No shorter
	// heuristic lease or restarted instance is evidence of a crashed owner.
	if recovered, e := q.RecoverUnadmittedScheduleOccurrences(ctx); e != nil || len(recovered) != 0 {
		t.Fatal("live claim prematurely recovered", e)
	}
	exec(`UPDATE schedule_logs SET claim_deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, orphan.ID)
	var priorSession string
	if e := pool.QueryRow(ctx, `SELECT id FROM bot_sessions WHERE bot_id=$1 AND type='schedule'`, bot).Scan(&priorSession); e != nil {
		t.Fatal(e)
	}
	if _, e := q.BindScheduleOccurrenceSession(ctx, sqlc.BindScheduleOccurrenceSessionParams{FireID: orphan.ID, BotID: toUUID(bot), SessionID: toUUID(priorSession)}); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("expired claimant bound before recovery", e)
	}
	pool.Close()
	pool = open()
	q = sqlc.New(pool)
	creator.pool = pool
	trigger.pool = pool
	var wg sync.WaitGroup
	var recovered atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids, e := q.RecoverUnadmittedScheduleOccurrences(ctx)
			if e != nil {
				t.Error(e)
			}
			for range ids {
				recovered.Add(1)
			}
		}()
	}
	wg.Wait()
	if recovered.Load() != 1 {
		t.Fatal("uncertain recovery not CAS", recovered.Load())
	}
	var state string
	if e := pool.QueryRow(ctx, `SELECT claim_state FROM schedule_logs WHERE id=$1`, orphan.ID).Scan(&state); e != nil || state != "unknown" {
		t.Fatal(state, e)
	}
	if e := newService().runScheduleOccurrence(ctx, toSchedule(row), due.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	if creator.calls.Load() != 1 || trigger.calls.Load() != 1 {
		t.Fatal("unknown replayed")
	}
	// An orphan owner cannot bind after expiry, even if it wakes up late.
	if _, e := q.BindScheduleOccurrenceSession(ctx, sqlc.BindScheduleOccurrenceSessionParams{FireID: orphan.ID, BotID: toUUID(bot), SessionID: toUUID(priorSession)}); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("expired owner was not fenced", e)
	}
	// Original admission exists but fire completion was lost: inspect its exact
	// invocation instead of relabeling or re-executing it.
	child := uuid.NewString()
	exec(`INSERT INTO bot_sessions(id,bot_id,created_by_user_id,type) VALUES($1,$2,$3,'schedule')`, child, bot, user)
	if _, e := q.BindScheduleOccurrenceSession(ctx, sqlc.BindScheduleOccurrenceSessionParams{FireID: live.ID, BotID: toUUID(bot), SessionID: toUUID(child)}); e != nil {
		t.Fatal(e)
	}
	run := uuid.NewString()
	_, fresh, e := ledger.NewPostgres(q, pool).Admit(ctx, ledger.AdmitParams{RunID: run, TurnID: run, BotID: bot, SessionID: child, InvocationID: "schedule:" + plan + ":" + live.ID.String(), Input: []byte(`{}`), InputFingerprint: "original-admission"})
	if e != nil || !fresh {
		t.Fatal(e)
	}
	exec(`UPDATE schedule_logs SET claim_deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, live.ID)
	if ids, e := q.RecoverUnadmittedScheduleOccurrences(ctx); e != nil || len(ids) != 0 {
		t.Fatal("existing admission ignored", ids, e)
	}
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM session_runs WHERE bot_id=$1`, bot).Scan(&count); e != nil || count != 2 {
		t.Fatal("original admission duplicated", count, e)
	}
	t.Log("16 callbacks / 2 service instances: 1 fire, 1 count, 1 native ledger admission; reopen repeats 0; separate claimant process SIGKILL before binding; no-run expiry: 8 recovery calls, 1 unknown; live claims and original admission retained; 0 models/timers")
}

// Invoked only by the parent test. There are no timers, models or actions.
func TestScheduleOccurrenceClaimProcess(t *testing.T) {
	plan := os.Getenv("MEMOH_OCCURRENCE_CHILD_PLAN")
	if plan == "" {
		t.Skip("child-only crash fixture")
	}
	due, e := time.Parse(time.RFC3339Nano, os.Getenv("MEMOH_OCCURRENCE_CHILD_DUE"))
	if e != nil {
		t.Fatal(e)
	}
	pool, e := db.OpenPostgresDSN(t.Context(), os.Getenv("MEMOH_SCHEDULE_OCCURRENCE_TEST_DSN"))
	if e != nil {
		t.Fatal(e)
	}
	q := sqlc.New(pool)
	row, e := q.ClaimScheduleOccurrence(t.Context(), sqlc.ClaimScheduleOccurrenceParams{ScheduleID: toUUID(plan), ScheduledFor: pgtype.Timestamptz{Time: due, Valid: true}})
	if e != nil {
		t.Fatal(e)
	}
	fmt.Println("CLAIM:" + row.ID.String())
	select {}
}
