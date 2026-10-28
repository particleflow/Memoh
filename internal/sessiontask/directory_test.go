package sessiontask

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/botdelegation"
	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/google/uuid"
)

type directoryFixture struct {
	*fixture
	candidates []dbstore.BotTaskDirectoryCandidate
	handoff    botdelegation.View
	denied     bool
	runs       map[string]dbstore.BotTaskRunReference
	runErr     error
}

func (f *directoryFixture) ReadBotTaskRun(_ context.Context, _, _, run string) (dbstore.BotTaskRunReference, error) {
	if f.runErr != nil {
		return dbstore.BotTaskRunReference{}, f.runErr
	}
	r, ok := f.runs[run]
	if !ok {
		return r, db.ErrNotFound
	}
	return r, nil
}

func (f *directoryFixture) ListBotTaskDirectoryCandidates(_ context.Context, q dbstore.SessionTaskFilter) ([]dbstore.BotTaskDirectoryCandidate, error) {
	rows := append([]dbstore.BotTaskDirectoryCandidate{}, f.candidates...)
	less := func(a, b dbstore.BotTaskDirectoryCandidate) bool {
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.RecordID < b.RecordID
	}
	sort.Slice(rows, func(i, j int) bool { return less(rows[j], rows[i]) })
	var out []dbstore.BotTaskDirectoryCandidate
	for _, r := range rows {
		if r.CreatedAt.After(q.SnapshotAt) || (q.ID != "" && (r.RecordID != q.ID || r.Type != q.Type)) || (q.Before.ID != "" && !less(r, dbstore.BotTaskDirectoryCandidate{Type: q.Before.Type, RecordID: q.Before.ID, CreatedAt: q.Before.CreatedAt})) {
			continue
		}
		out = append(out, r)
		if len(out) == int(q.Limit) {
			break
		}
	}
	return out, nil
}
func (f *directoryFixture) DirectoryMetadata(_ context.Context, bot, user, session, id string) (botdelegation.View, error) {
	if f.denied || bot != "bot" || user != "user" || session != f.root || id != f.handoff.ID {
		return botdelegation.View{}, botdelegation.ErrDenied
	}
	return f.handoff, nil
}
func directorySetup() (*directoryFixture, *Service) {
	f := &directoryFixture{fixture: newFixture(), runs: map[string]dbstore.BotTaskRunReference{}}
	s := NewService(f, f, dirs{f.fixture}, f, "directory-test").WithDelegationDirectory(f)
	return f, s
}
func (f *directoryFixture) native(id, state string, at time.Time) dbstore.BotTaskDirectoryCandidate {
	run := uuid.NewString()
	f.runs[run] = dbstore.BotTaskRunReference{BotID: "bot", SessionID: f.child, RunID: run, TurnID: uuid.NewString(), InvocationID: "subagent:" + id}
	return dbstore.BotTaskDirectoryCandidate{Type: "native_agent", ID: id, RecordID: run, RunID: run, SessionID: f.child, SourceSessionID: f.root, SourceBotID: "bot", State: state, Name: "Same name", CreatedAt: at}
}
func owner(s thread.Thread) (bool, error) { return s.CreatedByUserID == "user", nil }
func TestDirectoryPreservesOriginalOccurrencesAndCursorScope(t *testing.T) {
	f, s := directorySetup()
	at := time.Now().Add(-time.Minute)
	f.candidates = []dbstore.BotTaskDirectoryCandidate{f.native("bg_reused", "failed", at), f.native("bg_reused", "lost", at), f.native("bg_other", "completed", at)}
	seen := map[string]bool{}
	cursor := ""
	for {
		p, err := s.ListDirectory(t.Context(), "bot", "user", 1, cursor, owner, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range p.Items {
			if seen[v.RecordID] || v.TaskID == v.Destination.SessionID || v.Destination.RunID != v.RecordID {
				t.Fatalf("invalid task %+v", v)
			}
			seen[v.RecordID] = true
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
		if _, err = s.ListDirectory(t.Context(), "other", "user", 1, cursor, owner, true); !errors.Is(err, ErrCursor) {
			t.Fatal("cursor changed Bot", err)
		}
		if _, err = s.ListDirectory(t.Context(), "bot", "other", 1, cursor, owner, true); !errors.Is(err, ErrCursor) {
			t.Fatal("cursor changed user", err)
		}
		if _, err = s.decodeCursor(cursor, "bot", "user", activityCursorScope); !errors.Is(err, ErrCursor) {
			t.Fatal("cursor changed scope", err)
		}
	}
	if len(seen) != 3 {
		t.Fatal(seen)
	}
	row := f.candidates[0]
	f.sessions[f.root] = thread.Thread{ID: f.root, BotID: "bot", CreatedByUserID: "other", Type: "chat"}
	if _, err := s.GetDirectoryTask(t.Context(), "bot", "user", row.Type, row.RecordID, owner, true); !errors.Is(err, ErrUnavailable) {
		t.Fatal("ancestor revocation ignored", err)
	}
}
func TestDirectoryContinuesAfterAnEmptyFilteredScan(t *testing.T) {
	f, s := directorySetup()
	at := time.Now().Add(-time.Minute)
	for i := 0; i < scanBudget; i++ {
		r := f.native("bg_denied", "failed", at.Add(time.Duration(i)*time.Millisecond))
		r.SourceBotID = "other"
		f.candidates = append(f.candidates, r)
	}
	f.candidates = append(f.candidates, f.native("bg_visible", "completed", at.Add(-time.Second)))
	p, err := s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 0 || p.NextCursor == "" {
		t.Fatalf("first page %+v %v", p, err)
	}
	p, err = s.ListDirectory(t.Context(), "bot", "user", 20, p.NextCursor, owner, true)
	if err != nil || len(p.Items) != 1 || p.Items[0].TaskID != "bg_visible" || p.NextCursor != "" {
		t.Fatalf("continuation %+v %v", p, err)
	}
}
func TestDirectoryHandoffsBothDirectionsAndMissingScheduleDestination(t *testing.T) {
	f, s := directorySetup()
	at := time.Now().Add(-time.Minute)
	id := uuid.NewString()
	target := uuid.NewString()
	f.sessions[target] = thread.Thread{ID: target, BotID: "expert", CreatedByUserID: "user", Type: "chat", Title: "Delegated job"}
	f.handoff = botdelegation.View{ID: id, TargetBotID: "expert", TargetSessionID: target, State: "unknown"}
	f.candidates = []dbstore.BotTaskDirectoryCandidate{{Type: "bot_delegation", ID: id, RecordID: id, SourceBotID: "bot", SourceSessionID: f.root, OwnerUserID: "user", State: "running", CreatedAt: at}, {Type: "schedule_fire", ID: uuid.NewString(), RecordID: uuid.NewString(), SourceBotID: "bot", State: "failed", CreatedAt: at}}
	run := uuid.NewString()
	f.candidates[0].RunID = run
	f.runs[run] = dbstore.BotTaskRunReference{BotID: "expert", SessionID: target, RunID: run, TurnID: uuid.NewString(), InvocationID: "bot-delegation:" + id}
	for _, bot := range []string{"bot", "expert"} {
		p, err := s.ListDirectory(t.Context(), bot, "user", 20, "", owner, false)
		if err != nil || len(p.Items) != 1 || p.Items[0].State != "unknown" || p.Items[0].Destination.BotID != "expert" {
			t.Fatalf("handoff %s %+v %v", bot, p, err)
		}
	}
	f.denied = true
	p, err := s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 1 || p.Items[0].TaskType != "schedule_fire" || p.Items[0].Destination != nil || p.Items[0].State != "failed" {
		t.Fatalf("revoked/missing destination %+v %v", p, err)
	}
}
func TestDirectoryComputerUsesOriginalReceiptAndLiveTargetGrant(t *testing.T) {
	f, s := directorySetup()
	r := f.operations[f.op]
	f.candidates = []dbstore.BotTaskDirectoryCandidate{{Type: ComputerOperation, ID: r.ID, RecordID: r.ID, SourceBotID: "bot", SessionID: r.SessionID, RunID: "opaque/provider/run/not-uuid", State: "unknown", CreatedAt: r.CreatedAt}}
	p, err := s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 1 || p.Items[0].State != "unknown" || p.Items[0].Destination != nil {
		t.Fatalf("computer %+v %v", p, err)
	}
	run, turn := uuid.NewString(), uuid.NewString()
	f.candidates[0].RunID = run
	f.runs[run] = dbstore.BotTaskRunReference{BotID: "bot", SessionID: r.SessionID, RunID: run, TurnID: turn}
	p, err = s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 1 || p.Items[0].Destination == nil || p.Items[0].Destination.RunID != run || p.Items[0].Destination.TurnID != turn {
		t.Fatalf("native computer anchor %+v %v", p, err)
	}

	delete(f.targets, "pc-a")
	p, err = s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 0 {
		t.Fatalf("revoked computer %+v %v", p, err)
	}
}

func TestDirectoryExactRunBindingNeverFallsBackToLatest(t *testing.T) {
	for _, mismatch := range []string{"missing", "bot", "session", "run", "invocation", "turn", "read-error"} {
		t.Run(mismatch, func(t *testing.T) {
			f, s := directorySetup()
			row := f.native("original-task", "unknown", time.Now().Add(-time.Minute))
			original := f.runs[row.RunID]
			other := f.native("later-task", "completed", time.Now())
			f.candidates = []dbstore.BotTaskDirectoryCandidate{row, other}
			switch mismatch {
			case "missing":
				delete(f.runs, row.RunID)
			case "bot":
				original.BotID = "foreign"
			case "session":
				original.SessionID = "foreign"
			case "run":
				original.RunID = other.RunID
			case "invocation":
				original.InvocationID = "subagent:later-task"
			case "turn":
				original.TurnID = ""
			case "read-error":
				f.runErr = errors.New("storage unavailable")
			}
			if mismatch != "missing" {
				f.runs[row.RunID] = original
			}
			got, err := s.GetDirectoryTask(t.Context(), "bot", "user", row.Type, row.RecordID, owner, true)
			if mismatch == "read-error" {
				if !errors.Is(err, f.runErr) {
					t.Fatal("read failure concealed", err)
				}
			} else if err != nil || got.TaskID != row.ID || got.State != "unknown" || got.Destination != nil {
				t.Fatalf("invalid run became a history destination: %+v %v", got, err)
			}
		})
	}
}
