package sessiontask

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	dbstore "github.com/felinics/memoh/internal/db/store"
)

func TestSubagentUsesOwnRunAndVisibleText(t *testing.T) {
	f := newFixture()
	f.runs = map[string]dbstore.SubagentRun{f.root: {BotID: "bot", SessionID: f.root, RunID: "parent", State: "completed"}}
	read := func() Task {
		t.Helper()
		v, err := f.service.Get(t.Context(), "bot", "user", f.root, Subagent, f.child)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := read(); v.Status != "unknown" || v.ContentAvailable {
		t.Fatal("parent substituted", v)
	}
	for _, state := range []string{"accepted", "running", "waiting_decision", "finishing", "completed", "failed", "aborted", "lost"} {
		f.runs[f.child] = dbstore.SubagentRun{BotID: "bot", SessionID: f.child, RunID: "child-own", State: state, UpdatedAt: time.Now(), Content: json.RawMessage(`{"role":"assistant","content":[{"type":"reasoning","text":"PRIVATE"},{"type":"text","text":"visible child output"},{"type":"tool-result","text":"PRIVATE"}]}`)}
		v := read()
		if v.Status != state || v.RunID != "child-own" || !v.ContentAvailable || v.ResultSummary.Text != "visible child output" || v.ResultSummary.Source != "subagent_assistant" || v.BusinessState != "unknown" {
			t.Fatalf("%s: %+v", state, v)
		}
	}
	// A later run with no persisted assistant must not inherit a prior result.
	f.runs[f.child] = dbstore.SubagentRun{BotID: "bot", SessionID: f.child, RunID: "next", State: "accepted"}
	if v := read(); v.Status != "accepted" || v.ContentAvailable || v.ResultSummary != nil {
		t.Fatal(v)
	}
	// Defense against a store returning an unrelated run.
	r := f.runs[f.child]
	r.SessionID = f.root
	f.runs[f.child] = r
	if _, err := f.service.Get(t.Context(), "bot", "user", f.root, Subagent, f.child); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestSubagentTextProjectionIsBoundedAndNotRawLogs(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"role":"tool","content":"PRIVATE"}`, `{"role":"assistant","source":"internal_feedback","content":"PRIVATE"}`, `{"role":"assistant","content":[{"type":"reasoning","text":"PRIVATE"}]}`, `{"role":"assistant","content":{"arbitrary":"PRIVATE"}}`, strings.Repeat("x", 1024*1024+1)} {
		if v := visibleSubagentText([]byte(raw)); v != nil {
			t.Fatalf("unexpected projection %+v", v)
		}
	}
	raw, _ := json.Marshal(map[string]any{"role": "assistant", "content": strings.Repeat("文", 10000)})
	v := visibleSubagentText(raw)
	if v == nil || !v.Truncated || len(v.Text) > 16*1024 || !utf8.ValidString(v.Text) {
		t.Fatal(v)
	}
}
