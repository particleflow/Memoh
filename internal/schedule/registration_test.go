package schedule

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/toolcontext"
)

// No cron.Start, database, network, device or model. Store rows are synthetic.
type registrationTestQueries struct {
	timezoneQueries
	row        sqlc.Schedule
	increments int
	created    []sqlc.CreateScheduleLogRow
	completed  []sqlc.CompleteScheduleLogParams
}

func (q *registrationTestQueries) ListEnabledSchedules(context.Context) ([]sqlc.Schedule, error) {
	if !q.row.Enabled {
		return nil, nil
	}
	return []sqlc.Schedule{q.row}, nil
}

func (q *registrationTestQueries) IncrementScheduleCalls(context.Context, pgtype.UUID) (sqlc.Schedule, error) {
	q.increments++
	q.row.CurrentCalls++
	return q.row, nil
}

func (q *registrationTestQueries) CreateScheduleLog(_ context.Context, p sqlc.CreateScheduleLogParams) (sqlc.CreateScheduleLogRow, error) {
	r := sqlc.CreateScheduleLogRow{ID: mustParse(uuid.NewString()), ScheduleID: p.ScheduleID, BotID: p.BotID, SessionID: p.SessionID}
	q.created = append(q.created, r)
	return r, nil
}

func (q *registrationTestQueries) CompleteScheduleLog(_ context.Context, p sqlc.CompleteScheduleLogParams) (sqlc.ScheduleLog, error) {
	q.completed = append(q.completed, p)
	return sqlc.ScheduleLog{ID: p.ID, ScheduleID: q.row.ID, BotID: q.row.BotID, Status: p.Status}, nil
}

type registrationTestTrigger struct {
	payloads []TriggerPayload
	cause    error
	observe  func(context.Context)
}

func (p *registrationTestTrigger) TriggerSchedule(ctx context.Context, _ string, v TriggerPayload, _ string) (TriggerResult, error) {
	if p.observe != nil {
		p.observe(ctx)
	}
	p.payloads = append(p.payloads, v)
	return TriggerResult{}, p.cause
}

func registrationTestService(q *registrationTestQueries, p *registrationTestTrigger) *Service {
	return &Service{queries: q, triggerer: p, cron: cron.New(), jobs: map[string]cron.EntryID{}, defaultLocation: time.UTC, logger: slog.New(slog.DiscardHandler), jwtSecret: "synthetic-schedule-probe-only"}
}

func registrationTestFixture() *registrationTestQueries {
	q := &registrationTestQueries{row: runTestSchedule()}
	q.row.RunTarget = RunTargetExistingSession
	q.row.TargetSessionID = mustParse(execTestSessionID)
	q.bot = sqlc.GetBotByIDRow{ID: q.row.BotID, OwnerUserID: mustParse(runTestOwnerID), Timezone: pgtype.Text{String: "UTC", Valid: true}}
	q.sessions = map[string]sqlc.BotSession{execTestSessionID: {ID: mustParse(execTestSessionID), BotID: q.row.BotID}}
	return q
}

func TestBootstrapReplacesRegisteredSchedule(t *testing.T) {
	q := registrationTestFixture()
	svc := registrationTestService(q, &registrationTestTrigger{})
	if err := svc.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := len(svc.cron.Entries())
	tracked := len(svc.jobs)
	svc.removeJob(q.row.ID.String())
	after := len(svc.cron.Entries())
	t.Logf("duplicate Bootstrap: entries=%d tracked=%d; remove: remaining=%d tracked=%d", before, tracked, after, len(svc.jobs))
	if before != 1 || after != 0 {
		t.Fatalf("duplicate registration leaves an orphan cron entry: before=%d after=%d", before, after)
	}
}

func TestBootstrapDoesNotReplayStoredSchedule(t *testing.T) {
	q := registrationTestFixture()
	q.row.CurrentCalls = 7
	trigger := &registrationTestTrigger{}
	for i := 0; i < 2; i++ {
		svc := registrationTestService(q, trigger)
		if err := svc.Bootstrap(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(svc.cron.Entries()) != 1 {
			t.Fatal("missing registration")
		}
	}
	if q.increments != 0 || len(q.created) != 0 || len(trigger.payloads) != 0 || q.row.CurrentCalls != 7 {
		t.Fatal("bootstrap replayed business")
	}
	t.Log("two service instances bootstrap existing row: registrations only; 0 increments/logs/trigger calls")
}

func TestFailedFireIsNotReplayedOnBootstrap(t *testing.T) {
	q := registrationTestFixture()
	offline := errors.New("synthetic PC offline")
	trigger := &registrationTestTrigger{cause: offline}
	svc := registrationTestService(q, trigger)
	if err := svc.runSchedule(t.Context(), toSchedule(q.row)); !errors.Is(err, offline) {
		t.Fatal(err)
	}
	if len(trigger.payloads) != 1 || len(q.created) != 1 || len(q.completed) != 1 || q.completed[0].Status != "error" {
		t.Fatal("offline result not recorded once")
	}
	first := trigger.payloads[0]
	restarted := registrationTestService(q, trigger)
	if err := restarted.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(trigger.payloads) != 1 || len(q.created) != 1 {
		t.Fatal("restart replayed the failed fire")
	}
	// A deliberate future periodic occurrence is new intent, not replay of FireID.
	if err := restarted.runSchedule(t.Context(), toSchedule(q.row)); !errors.Is(err, offline) {
		t.Fatal(err)
	}
	if len(trigger.payloads) != 2 || trigger.payloads[1].FireID == first.FireID || trigger.payloads[1].SessionID != first.SessionID {
		t.Fatal("new occurrence identity/session mismatch")
	}
	t.Log("offline: 1 trigger + 1 error log; restart: 0 replay; explicit next occurrence: new FireID, same stored session")
}

func TestConcurrentBootstrapKeepsOneRegistration(t *testing.T) {
	q := registrationTestFixture()
	svc := registrationTestService(q, &registrationTestTrigger{})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := svc.Bootstrap(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := len(svc.cron.Entries()); got != 1 {
		t.Fatalf("entries=%d, want 1", got)
	}
	svc.removeJob(q.row.ID.String())
	if got := len(svc.cron.Entries()); got != 0 {
		t.Fatalf("entries after removal=%d, want 0", got)
	}
}

func TestRegisteredFireDoesNotInheritCreatorBinding(t *testing.T) {
	q := registrationTestFixture()
	type contextKey struct{}
	creator, err := toolcontext.WithComputerBinding(t.Context(), toolcontext.ComputerBinding{
		BotID: q.row.BotID.String(), UserID: runTestOwnerID, SessionID: "creator-session",
		TargetID: "creator-pc", RunID: "creator-run",
	})
	if err != nil {
		t.Fatal(err)
	}
	creator = context.WithValue(creator, contextKey{}, "creator-request")
	trigger := &registrationTestTrigger{observe: func(ctx context.Context) {
		if ctx.Value(contextKey{}) != "service-lifecycle" {
			t.Error("fire inherited creator context instead of service lifecycle")
		}
		_, err := toolcontext.ResolveComputerBinding(ctx, toolcontext.Session{
			BotID: q.row.BotID.String(), SessionID: "creator-session", UserID: runTestOwnerID,
		})
		if !errors.Is(err, toolcontext.ErrComputerBinding) {
			t.Errorf("creator binding leaked into independent fire: %v", err)
		}
	}}
	svc := registrationTestService(q, trigger)
	svc.lifecycleCtx = context.WithValue(t.Context(), contextKey{}, "service-lifecycle")
	if err := svc.scheduleJob(creator, q.row); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual registered closure without starting a timer.
	svc.cron.Entries()[0].Job.Run()
	if len(trigger.payloads) != 1 || trigger.payloads[0].OwnerUserID != runTestOwnerID || trigger.payloads[0].SessionID != execTestSessionID {
		t.Fatal("fire did not resolve the persisted owner and target session")
	}
}
