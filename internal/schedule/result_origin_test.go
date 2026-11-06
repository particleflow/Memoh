package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

type resultHooksFixture struct {
	calls, wakes int
	user         string
	err          error
}

func (h *resultHooksFixture) AuthorizeScheduleResultOrigin(context.Context, string, []byte) (string, error) {
	h.calls++
	return h.user, h.err
}
func (h *resultHooksFixture) WakeScheduleResults(context.Context) { h.wakes++ }

type originQueries struct {
	*registrationTestQueries
	origin []byte
}

func (q *originQueries) CreateScheduleLog(ctx context.Context, p sqlc.CreateScheduleLogParams) (sqlc.CreateScheduleLogRow, error) {
	q.origin = append([]byte(nil), p.ResultOrigin...)
	return q.registrationTestQueries.CreateScheduleLog(ctx, p)
}

func (q *originQueries) GetScheduleByID(context.Context, pgtype.UUID) (sqlc.Schedule, error) {
	return q.row, nil
}

type originSessionCreator struct {
	fakeSessionCreator
	owner string
}

func (s *originSessionCreator) CreateScheduleSession(ctx context.Context, p SessionSpec) (string, error) {
	s.owner = p.OwnerUserID
	return s.fakeSessionCreator.CreateScheduleSession(ctx, p)
}

func TestScheduleResultTrustedCreation(t *testing.T) {
	q := &createQueries{}
	s := newExecutionService(t, &q.executionQueries, nil)
	s.queries = q
	s.parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	h := &resultHooksFixture{user: runTestOwnerID}
	s.SetResultDeliveryHooks(h)
	var req CreateRequest
	if err := json.Unmarshal([]byte(`{"name":"fixture","pattern":"0 0 * * *","command":"synthetic","enabled":false,"result_origin":{"version":1}}`), &req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), execTestBotID, req); err != nil {
		t.Fatal(err)
	}
	if h.calls != 0 || len(q.created.ResultOrigin) != 0 {
		t.Fatal("public JSON forged trusted origin")
	}
	raw := []byte(`{"version":1}`)
	ctx := WithResultOrigin(t.Context(), raw)
	raw[0] = 'x'
	if _, err := s.Create(ctx, execTestBotID, req); err != nil {
		t.Fatal(err)
	}
	if h.calls != 1 || string(q.created.ResultOrigin) != `{"version":1}` {
		t.Fatal("origin not atomically persisted or defensively copied")
	}
	h.err = errors.New("revoked")
	q.created = nil
	if _, err := s.Create(ctx, execTestBotID, req); !errors.Is(err, h.err) || q.created != nil {
		t.Fatal("revoked origin persisted")
	}
}

func TestScheduleResultOriginalUserAndLegacy(t *testing.T) {
	for _, opted := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "contact"}[opted], func(t *testing.T) {
			q := &originQueries{registrationTestQueries: registrationTestFixture()}
			q.row.RunTarget = RunTargetNewSession
			q.row.TargetSessionID = pgtype.UUID{}
			if opted {
				q.row.ResultOrigin = []byte(`{"version":1}`)
			}
			trigger := &registrationTestTrigger{}
			s := registrationTestService(q.registrationTestQueries, trigger)
			s.queries = q
			creator := &originSessionCreator{}
			s.sessionCreator = creator
			h := &resultHooksFixture{user: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}
			s.SetResultDeliveryHooks(h)
			if err := s.runSchedule(t.Context(), toSchedule(q.row)); err != nil {
				t.Fatal(err)
			}
			want := runTestOwnerID
			if opted {
				want = h.user
			}
			if creator.owner != want || len(trigger.payloads) != 1 || trigger.payloads[0].OwnerUserID != want {
				t.Fatal("wrong execution principal")
			}
			if opted && (h.calls != 1 || string(q.origin) != string(q.row.ResultOrigin)) {
				t.Fatal("missing fire origin")
			}
			if !opted && (h.calls != 0 || len(q.origin) != 0) {
				t.Fatal("legacy auto-enrolled")
			}
			if h.wakes != 1 {
				t.Fatal("missing completion wake")
			}
		})
	}
}

func TestScheduleResultDeniedAndPinnedExecution(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "pinned", true: "revoked"}[denied], func(t *testing.T) {
			q := &originQueries{registrationTestQueries: registrationTestFixture()}
			q.row.ResultOrigin = []byte(`{"version":1}`)
			h := &resultHooksFixture{user: runTestOwnerID}
			if denied {
				h.err = errors.New("revoked")
			}
			trigger := &registrationTestTrigger{}
			s := registrationTestService(q.registrationTestQueries, trigger)
			s.queries = q
			s.SetResultDeliveryHooks(h)
			if err := s.runSchedule(t.Context(), toSchedule(q.row)); err == nil {
				t.Fatal("invalid contact schedule executed")
			}
			if len(trigger.payloads) != 0 || len(q.completed) != 1 || q.completed[0].Status != "error" || string(q.origin) != string(q.row.ResultOrigin) {
				t.Fatal("denial not retained without execution")
			}
		})
	}
}

func TestScheduleResultUpdateCannotPinMainChat(t *testing.T) {
	q := &originQueries{registrationTestQueries: registrationTestFixture()}
	q.row.ResultOrigin = []byte(`{"version":1}`)
	s := registrationTestService(q.registrationTestQueries, &registrationTestTrigger{})
	s.queries = q
	// The embedded fake deliberately has no UpdateSchedule: reaching persistence
	// would panic, proving rejection happens before any mutation.
	_, err := s.Update(t.Context(), q.row.ID.String(), UpdateRequest{Execution: &ExecutionConfig{RunTarget: RunTargetExistingSession, TargetSessionID: execTestSessionID}})
	if err == nil {
		t.Fatal("contact update allowed existing session")
	}
}
