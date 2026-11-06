package sessiontask

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	thread "github.com/felinics/memoh/internal/chat/thread"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func TestDirectParentProjectionAcrossPagesAndQueryRoots(t *testing.T) {
	f := newFixture() // Root -> child -> grandchild; every title is identical.
	rootOperation := f.addOperation(f.root, "pc-a", time.Now().Add(-time.Second))
	before, _ := json.Marshal(f.operations)
	want := map[string]string{f.child: f.root, f.grandchild: f.child, f.op: f.child, rootOperation: ""}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := f.service.List(t.Context(), "bot", "user", f.root, 1, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			parent, ok := want[item.TaskID]
			if !ok || seen[item.TaskID] || item.ParentSessionID != f.root || item.DirectParentSessionID != parent {
				t.Fatalf("wrong or duplicate relation: %+v", item)
			}
			seen[item.TaskID] = true
			if item.TaskID == f.op && item.SessionID != f.grandchild {
				t.Fatal("operation owner was rewritten")
			}
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != len(want) {
		t.Fatal("missing task", seen)
	}
	for _, root := range []string{f.root, f.child} {
		item, err := f.service.Get(t.Context(), "bot", "user", root, ComputerOperation, f.op)
		if err != nil || item.ParentSessionID != root || item.DirectParentSessionID != f.child || item.SessionID != f.grandchild || item.TaskID != f.op {
			t.Fatalf("query root changed ownership: %+v %v", item, err)
		}
	}
	after, _ := json.Marshal(f.operations)
	if string(before) != string(after) {
		t.Fatal("projection changed original operations")
	}
}

func TestDirectParentProjectionDoesNotExposeUnavailableAncestors(t *testing.T) {
	for _, scenario := range []string{"missing", "other_owner", "other_bot", "orphan", "cycle"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture()
			child := f.sessions[f.child]
			switch scenario {
			case "missing":
				delete(f.sessions, f.child)
			case "other_owner":
				child.CreatedByUserID = "other"
			case "other_bot":
				child.BotID = "other"
			case "orphan":
				child.ParentThreadID = uuid.NewString()
			case "cycle":
				child.ParentThreadID = f.grandchild
			}
			if scenario != "missing" {
				f.sessions[f.child] = child
			}
			item, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
			if !errors.Is(err, ErrUnavailable) || item.DirectParentSessionID != "" || item.SessionID != "" {
				t.Fatalf("unavailable relation leaked: %+v %v", item, err)
			}
			page, err := f.service.List(t.Context(), "bot", "user", f.root, 100, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 0 {
				t.Fatalf("hidden subtree exposed: %+v", page)
			}
		})
	}
	// An unrelated orphan must not be attached to the query root by its name.
	f := newFixture()
	id := uuid.NewString()
	f.sessions[id] = thread.Thread{ID: id, BotID: "bot", CreatedByUserID: "user", Type: Subagent, Title: "SECRET"}
	f.rows = []dbstore.SessionTaskCandidate{{Type: Subagent, ID: id, SessionID: id, CreatedAt: time.Now().Add(-time.Second)}}
	page, err := f.service.List(t.Context(), "bot", "user", f.root, 100, "")
	if err != nil || len(page.Items) != 0 {
		t.Fatal("orphan attached by inference", page, err)
	}
}
