package schedule

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/robfig/cron/v3"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

type occurrenceCaptureQueries struct {
	*registrationTestQueries
	mu  sync.Mutex
	due []time.Time
}

func (q *occurrenceCaptureQueries) ClaimScheduleOccurrence(_ context.Context, p sqlc.ClaimScheduleOccurrenceParams) (sqlc.ClaimScheduleOccurrenceRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.due = append(q.due, p.ScheduledFor.Time)
	return sqlc.ClaimScheduleOccurrenceRow{}, pgx.ErrNoRows
}

func TestScheduleOccurrenceCallbackKeepsDueIdentity(t *testing.T) {
	q := &occurrenceCaptureQueries{registrationTestQueries: registrationTestFixture()}
	q.row.ResultOrigin = []byte(`{"version":1}`)
	q.row.Pattern = "0 0 1 1 *"
	q.row.CreatedAt.Time = time.Now()
	q.row.CreatedAt.Valid = true
	s := registrationTestService(q.registrationTestQueries, &registrationTestTrigger{})
	s.queries = q
	if e := s.scheduleJob(t.Context(), q.row); e != nil {
		t.Fatal(e)
	}
	old := s.cron.Entries()[0]
	due := old.Schedule.(*dueOnce).due
	// Replace registration before invoking a captured old callback. Its due must
	// not be read from the new entry; it must not replace the newer registration.
	if e := s.scheduleJob(t.Context(), q.row); e != nil {
		t.Fatal(e)
	}
	current := s.jobs[q.row.ID.String()]
	old.Job.Run()
	old.Job.Run()
	if len(q.due) != 1 || !q.due[0].Equal(due) {
		t.Fatal("callback substituted/repeated due identity", q.due)
	}
	if s.jobs[q.row.ID.String()] != current {
		t.Fatal("old callback replaced current registration")
	}
}

func TestScheduleOccurrenceIntervalUsesPersistedAnchor(t *testing.T) {
	parsed, e := cron.ParseStandard("@every 10m")
	if e != nil {
		t.Fatal(e)
	}
	row := runTestSchedule()
	row.CreatedAt.Time = time.Date(2026, 10, 29, 12, 0, 12, 123, time.UTC)
	row.CreatedAt.Valid = true
	a := nextContactOccurrence(row, parsed, time.UTC, time.Date(2026, 10, 29, 12, 3, 0, 0, time.UTC))
	b := nextContactOccurrence(row, parsed, time.UTC, time.Date(2026, 10, 29, 12, 4, 25, 0, time.UTC))
	if !a.Equal(b) || !a.Equal(time.Date(2026, 10, 29, 12, 10, 12, 0, time.UTC)) {
		t.Fatal("restart shifted interval occurrence", a, b)
	}
	once := &dueOnce{due: a}
	if got := once.Next(a.Add(time.Hour)); !got.Equal(a) {
		t.Fatal("delayed registration changed occurrence")
	}
	if !once.Next(a).IsZero() {
		t.Fatal("one-shot native cron entry repeated")
	}
}
