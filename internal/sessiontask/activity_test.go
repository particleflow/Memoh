package sessiontask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	thread "github.com/felinics/memoh/internal/chat/thread"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type activityFixture struct{ *fixture }

func (f activityFixture) ListSessionActivityCandidates(ctx context.Context, q dbstore.SessionTaskFilter) ([]dbstore.SessionTaskCandidate, error) {
	return f.ListSessionTaskCandidates(ctx, q)
}

func activityFixtureForTest() activityFixture {
	f := activityFixture{newFixture()}
	f.runs = map[string]dbstore.SubagentRun{}
	f.rows = nil
	f.service.store = f
	return f
}

func (f activityFixture) run(session, kind, state string) string {
	s := f.sessions[session]
	s.Type = kind
	s.Title = "Name " + kind
	f.sessions[session] = s
	id := uuid.NewString()
	f.rows = append(f.rows, dbstore.SessionTaskCandidate{Type: sessionRun, ID: id, SessionID: session, CreatedAt: time.Now().Add(-time.Minute)})
	f.runs[session] = dbstore.SubagentRun{BotID: "bot", SessionID: session, RunID: id, State: state, Content: []byte(`{"secret":"must not be projected"}`)}
	return id
}
func allowActivity(thread.Thread) bool { return true }

type failedActivitySessions struct{ err error }

func (f failedActivitySessions) Get(context.Context, string) (thread.Thread, error) {
	return thread.Thread{}, f.err
}
func TestActivityDoesNotTurnDatabaseReadFailureIntoEmptySuccess(t *testing.T) {
	f := activityFixtureForTest()
	f.run(f.root, "chat", "running")
	failure := errors.New("synthetic database unavailable")
	f.service.sessions = failedActivitySessions{failure}
	if _, err := f.service.ListActivity(t.Context(), "bot", "user", 100, "", allowActivity); !errors.Is(err, failure) {
		t.Fatalf("database failure hidden: %v", err)
	}
}

func TestActivityDiscoversUnopenedRootsBotAgentAndTask(t *testing.T) {
	f := activityFixtureForTest()
	f.run(f.root, "chat", "running")
	f.run(f.child, Subagent, "waiting_decision")
	other := uuid.NewString()
	f.sessions[other] = thread.Thread{ID: other, BotID: "bot", CreatedByUserID: "user", Type: "chat", Title: "Other original session"}
	op := f.addOperation(other, "pc-a", time.Now().Add(-time.Minute))
	r := f.operations[op]
	r.State = "submitted"
	f.operations[op] = r
	before, _ := json.Marshal(f.operations)
	p, err := f.service.ListActivity(t.Context(), "bot", "user", 100, "", allowActivity)
	if err != nil || len(p.Items) != 3 {
		t.Fatalf("items=%+v err=%v", p, err)
	}
	kinds := map[string]bool{}
	for _, v := range p.Items {
		kinds[v.Kind] = true
		if v.SessionID == "" || v.BotID != "bot" || v.Name == "" {
			t.Fatal(v)
		}
	}
	if len(kinds) != 3 {
		t.Fatal(kinds)
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "target") || strings.Contains(string(raw), "output") {
		t.Fatal(string(raw))
	}
	after, _ := json.Marshal(f.operations)
	if string(before) != string(after) {
		t.Fatal("activity wrote an operation")
	}
}

func TestActivityPermissionAncestryAndTargetRevocation(t *testing.T) {
	for _, scenario := range []string{"session", "ancestor", "foreign-bot", "missing-parent", "cycle", "target-revoked"} {
		t.Run(scenario, func(t *testing.T) {
			f := activityFixtureForTest()
			f.run(f.grandchild, Subagent, "running")
			readable := func(s thread.Thread) bool {
				return (scenario != "session" || s.ID != f.grandchild) && (scenario != "ancestor" || s.ID != f.root)
			}
			s := f.sessions[f.grandchild]
			switch scenario {
			case "foreign-bot":
				s.BotID = "other"
			case "missing-parent":
				s.ParentThreadID = uuid.NewString()
			case "cycle":
				s.ParentThreadID = s.ID
			case "target-revoked":
				s.Metadata = map[string]any{"workspace_target_id": "revoked"}
			}
			f.sessions[s.ID] = s
			p, err := f.service.ListActivity(t.Context(), "bot", "user", 100, "", readable)
			if err != nil || len(p.Items) != 0 {
				t.Fatalf("%+v %v", p, err)
			}
		})
	}
}

func TestActivityRejectsTerminalUnknownSupersededRuns(t *testing.T) {
	for _, state := range []string{"unknown", "completed", "failed", "aborted", "lost"} {
		t.Run(state, func(t *testing.T) {
			f := activityFixtureForTest()
			f.run(f.root, "chat", state)
			p, err := f.service.ListActivity(t.Context(), "bot", "user", 100, "", allowActivity)
			if err != nil || len(p.Items) != 0 {
				t.Fatalf("%+v %v", p, err)
			}
		})
	}
	f := activityFixtureForTest()
	f.run(f.root, "chat", "running")
	r := f.runs[f.root]
	r.RunID = uuid.NewString()
	f.runs[f.root] = r
	p, err := f.service.ListActivity(t.Context(), "bot", "user", 100, "", allowActivity)
	if err != nil || len(p.Items) != 0 {
		t.Fatalf("superseded %+v %v", p, err)
	}
}

func TestActivityCursorContinuesPastInvisiblePageAndCannotCrossScope(t *testing.T) {
	f := activityFixtureForTest()
	for i := 0; i < 203; i++ {
		id := uuid.NewString()
		f.sessions[id] = thread.Thread{ID: id, BotID: "bot", CreatedByUserID: "user", Type: "chat", Title: "Same name"}
		f.run(id, "chat", "running")
	}
	page, err := f.service.ListActivity(t.Context(), "bot", "user", 100, "", func(thread.Thread) bool { return false })
	if err != nil || len(page.Items) != 0 || page.NextCursor == "" {
		t.Fatalf("%+v %v", page, err)
	}
	next, err := f.service.ListActivity(t.Context(), "bot", "user", 100, page.NextCursor, allowActivity)
	if err != nil || len(next.Items) != 3 {
		t.Fatalf("%+v %v", next, err)
	}
	for _, scope := range [][2]string{{"other", "user"}, {"bot", "other"}} {
		if _, err = f.service.ListActivity(t.Context(), scope[0], scope[1], 100, page.NextCursor, allowActivity); !errors.Is(err, ErrCursor) {
			t.Fatal(err)
		}
	}
	if _, err = f.service.decodeCursor(page.NextCursor, "bot", "user", f.root); !errors.Is(err, ErrCursor) {
		t.Fatal("activity cursor accepted by owned tasks")
	}
}
