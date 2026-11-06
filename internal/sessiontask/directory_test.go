package sessiontask

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/botdelegation"
	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type directoryFixture struct {
	*fixture
	candidates []dbstore.BotTaskDirectoryCandidate
	handoff    botdelegation.View
	denied     bool
	batches    map[string]dbstore.ComputerTaskBatchRecord
	batchErr   error
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

func (f *directoryFixture) ReadComputerTaskBatch(_ context.Context, id, _, _, _ string) (dbstore.ComputerTaskBatchRecord, error) {
	if f.batchErr != nil {
		return dbstore.ComputerTaskBatchRecord{}, f.batchErr
	}
	rec, ok := f.batches[id]
	if !ok {
		return rec, db.ErrNotFound
	}
	return rec, nil
}

func (f *directoryFixture) ListBotTaskDirectoryCandidates(_ context.Context, q dbstore.SessionTaskFilter) ([]dbstore.BotTaskDirectoryCandidate, error) {
	rows := append([]dbstore.BotTaskDirectoryCandidate{}, f.candidates...)
	// Mirror the SQL contract: activity order on updated_at with the created
	// time as the snapshot visibility bound.
	less := func(a, b dbstore.BotTaskDirectoryCandidate) bool {
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.Before(b.UpdatedAt)
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.RecordID < b.RecordID
	}
	sort.Slice(rows, func(i, j int) bool { return less(rows[j], rows[i]) })
	var out []dbstore.BotTaskDirectoryCandidate
	for _, r := range rows {
		if r.CreatedAt.After(q.SnapshotAt) || (q.ID != "" && (r.RecordID != q.ID || r.Type != q.Type)) || (q.Before.ID != "" && !less(r, dbstore.BotTaskDirectoryCandidate{Type: q.Before.Type, RecordID: q.Before.ID, UpdatedAt: q.Before.UpdatedAt})) {
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
	f := &directoryFixture{fixture: newFixture(), runs: map[string]dbstore.BotTaskRunReference{}, batches: map[string]dbstore.ComputerTaskBatchRecord{}}
	s := NewService(f, f, dirs{f.fixture}, f, "directory-test").WithDelegationDirectory(f).WithComputerBatchDirectory(f)
	return f, s
}

func (f *directoryFixture) native(id, state string, at time.Time) dbstore.BotTaskDirectoryCandidate {
	run := uuid.NewString()
	f.runs[run] = dbstore.BotTaskRunReference{BotID: "bot", SessionID: f.child, RunID: run, TurnID: uuid.NewString(), InvocationID: "subagent:" + id}
	return dbstore.BotTaskDirectoryCandidate{Type: "native_agent", ID: id, RecordID: run, RunID: run, SessionID: f.child, SourceSessionID: f.root, SourceBotID: "bot", State: state, Name: "Same name", CreatedAt: at, UpdatedAt: at}
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
	f.candidates = []dbstore.BotTaskDirectoryCandidate{{Type: "bot_delegation", ID: id, RecordID: id, SourceBotID: "bot", SourceSessionID: f.root, OwnerUserID: "user", State: "running", CreatedAt: at, UpdatedAt: at}, {Type: "schedule_fire", ID: uuid.NewString(), RecordID: uuid.NewString(), SourceBotID: "bot", State: "failed", CreatedAt: at, UpdatedAt: at}}
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

func TestDirectoryComputerUsesOriginalReceiptAndCurrentHistoryPermission(t *testing.T) {
	f, s := directorySetup()
	r := f.operations[f.op]
	f.candidates = []dbstore.BotTaskDirectoryCandidate{{Type: ComputerOperation, ID: r.ID, RecordID: r.ID, SourceBotID: "bot", SessionID: r.SessionID, RunID: "opaque/provider/run/not-uuid", State: "unknown", CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}}
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
	if err != nil || len(p.Items) != 1 || p.Items[0].State != "unknown" {
		t.Fatalf("retired computer history %+v %v", p, err)
	}
	f.historyDenied = true
	p, err = s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 0 {
		t.Fatalf("revoked history permission %+v %v", p, err)
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

func TestDirectoryOrdersByRecentActivity(t *testing.T) {
	f, s := directorySetup()
	at := time.Now().Add(-time.Hour).UTC()
	old := f.native("old-but-busy", "running", at)
	// Created later yet untouched since: must sort below the older, recently
	// advanced task.
	stale := f.native("new-but-idle", "completed", at.Add(30*time.Minute))
	stale.UpdatedAt = stale.CreatedAt
	old.UpdatedAt = at.Add(90 * time.Minute)
	f.candidates = []dbstore.BotTaskDirectoryCandidate{stale, old}
	p, err := s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 2 || p.Items[0].TaskID != "old-but-busy" || p.Items[1].TaskID != "new-but-idle" {
		t.Fatalf("activity order %+v %v", p, err)
	}
}

func TestDirectoryComputerBatchProgressAndBinding(t *testing.T) {
	f, s := directorySetup()
	at := time.Now().Add(-time.Minute).UTC()
	id := uuid.NewString()
	run := uuid.NewString()
	f.batches[id] = dbstore.ComputerTaskBatchRecord{ID: id, BotID: "bot", SessionID: f.root, UserID: "user", TargetID: "pc-a", RunID: run, Label: "Nightly album export", State: "blocked", Items: dbstore.JSON(`[{"spec":{"kind":"herdr_pi_task","task":"first"},"state":"succeeded"},{"spec":{"kind":"herdr_pi_task","task":"second"},"state":"in_flight"},{"spec":{"kind":"herdr_pi_task","task":"third"},"state":"queued"}]`), CreatedAt: at, UpdatedAt: at}
	f.runs[run] = dbstore.BotTaskRunReference{BotID: "bot", SessionID: f.root, RunID: run, TurnID: uuid.NewString()}
	f.candidates = []dbstore.BotTaskDirectoryCandidate{{Type: ComputerBatch, ID: id, RecordID: id, SessionID: f.root, SourceBotID: "bot", RunID: run, State: "running", Name: "Nightly album export", CreatedAt: at, UpdatedAt: at}}
	p, err := s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 1 {
		t.Fatalf("batch missing %+v %v", p, err)
	}
	item := p.Items[0]
	if item.TaskType != ComputerBatch || item.State != "blocked" || item.Progress != "1/3" || item.Destination == nil || item.Destination.RunID != run {
		t.Fatalf("batch projection %+v", item)
	}
	// A workdir/session target boundary that no longer matches the batch's
	// pinned target hides the row, mirroring the computer-operation check.
	f.sessions[f.root] = thread.Thread{ID: f.root, BotID: "bot", CreatedByUserID: "user", Type: "chat", Metadata: map[string]any{"workspace_target_id": "pc-b"}}
	p, err = s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 0 {
		t.Fatalf("foreign target boundary leaked %+v %v", p, err)
	}
	delete(f.sessions[f.root].Metadata, "workspace_target_id")
	// An unreadable item array keeps the row without a count; a store failure
	// or missing row hides it; no batch reader hides it too.
	f.batches[id] = dbstore.ComputerTaskBatchRecord{ID: id, BotID: "bot", SessionID: f.root, UserID: "user", TargetID: "pc-a", RunID: run, State: "running", Items: dbstore.JSON(`not-json`), CreatedAt: at, UpdatedAt: at}
	p, err = s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 1 || p.Items[0].Progress != "" || p.Items[0].State != "running" {
		t.Fatalf("unreadable items hid the row %+v %v", p, err)
	}
	f.batchErr = errors.New("storage unavailable")
	if _, err = s.GetDirectoryTask(t.Context(), "bot", "user", ComputerBatch, id, owner, true); !errors.Is(err, f.batchErr) {
		t.Fatal("batch read failure concealed", err)
	}
	f.batchErr = nil
	delete(f.batches, id)
	if _, err = s.GetDirectoryTask(t.Context(), "bot", "user", ComputerBatch, id, owner, true); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing batch row concealed", err)
	}
	s.batches = nil
	p, err = s.ListDirectory(t.Context(), "bot", "user", 20, "", owner, true)
	if err != nil || len(p.Items) != 0 {
		t.Fatalf("batch surfaced without a reader %+v %v", p, err)
	}
}
