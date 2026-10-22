package agenvo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures follow Pi 1.2.0 session-manager.ts appendCompaction/branchWithSummary
// and session-context-edit.test.ts's no-retained-entries compaction case. They
// exercise our real host envelope, not an installed official Pi runtime.
func TestBatchHistoryOfficialSummaryForms(t *testing.T) {
	for _, tc := range []struct {
		name, tail, want string
	}{
		{"compaction-self", `{"type":"compaction","summary":"synthetic summary","firstKeptEntryId":"tail","tokensBefore":100}`, "verified"},
		{"compaction-retained", `{"type":"compaction","summary":"synthetic summary","firstKeptEntryId":"entry1","tokensBefore":100}`, "verified"},
		{"compaction-missing-kept", `{"type":"compaction","summary":"synthetic summary","firstKeptEntryId":"absent","tokensBefore":100}`, "unconfirmed"},
		{"branch-root", `{"type":"branch_summary","parentId":null,"fromId":"entry1","summary":"synthetic branch summary"}`, "verified"},
		{"branch-from-reset-root", `{"type":"branch_summary","parentId":null,"fromId":"root","summary":"synthetic branch summary"}`, "verified"},
		{"branch-earlier-parent", `{"type":"branch_summary","fromId":"entry1","summary":"synthetic branch summary"}`, "verified"},
		{"branch-missing-fields", `{"type":"branch_summary","parentId":null}`, "corrupt"},
		{"branch-missing-summary", `{"type":"branch_summary","parentId":null,"fromId":"entry1"}`, "corrupt"},
		{"branch-missing-from", `{"type":"branch_summary","parentId":null,"summary":"synthetic"}`, "corrupt"},
		{"branch-unavailable-source", `{"type":"branch_summary","parentId":null,"fromId":"other-session-entry","summary":"synthetic"}`, "unconfirmed"},
		{"branch-empty-summary-with-parent", `{"type":"branch_summary","fromId":"entry1","summary":""}`, "verified"},
		{"branch-empty-root-summary", `{"type":"branch_summary","parentId":null,"fromId":"entry1","summary":""}`, "unconfirmed"},
		{"detached-setup-leaf", `{"type":"session_info","parentId":null,"name":"synthetic new branch"}`, "unconfirmed"},
		{"new-user-branch", `{"type":"message","parentId":null,"message":{"role":"user","content":"synthetic new branch","timestamp":1792627200000}}`, "verified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, dir, run := historyFixture(t)
			first, err := run(p)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, ".memoh-computer", "sessions", p.PiSessionID, first.History.Evidence.File)
			original := readBatchFixture(t, file)
			entry := map[string]any{"id": "tail", "parentId": "entry1", "timestamp": "2026-10-22T00:00:00.000Z"}
			if err = json.Unmarshal([]byte(tc.tail), &entry); err != nil {
				t.Fatal(err)
			}
			line, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			withTail := append(append([]byte(nil), original...), append(line, '\n')...)
			if err = os.WriteFile(file, withTail, 0o600); err != nil {
				t.Fatal(err)
			}
			p.Attempt = strings.Repeat("e", 32)
			p.History = &BatchHistoryExpectation{Version: 1, Mode: "resume", Evidence: first.History.Evidence}
			r, err := run(p)
			if err != nil || r.History.Before != tc.want || r.Started != (tc.want == "verified") {
				t.Fatalf("summary validation: %+v %v", r, err)
			}
			actual := readBatchFixture(t, file)
			if tc.want == "verified" {
				if r.History.After != "verified" || !strings.HasPrefix(string(actual), string(withTail)) {
					t.Fatal("valid branch/compaction failed or old bytes changed")
				}
			} else if string(actual) != string(withTail) || r.History.Evidence != nil || r.History.After != "not_started" {
				t.Fatal("unconfirmed history was changed or given a new checkpoint")
			}
			wantCalls := 1
			if tc.want == "verified" {
				wantCalls++
			}
			if strings.Count(string(readBatchFixture(t, filepath.Join(dir, "calls"))), "\n") != wantCalls {
				t.Fatal("unexpected Pi dispatch count")
			}
		})
	}
}

func TestBatchHistoryUserOnlyFirstTurnCanResume(t *testing.T) {
	p, _, dir, run := historyFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "user-only"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := run(p)
	if err != nil || first.History.After != "created" {
		t.Fatalf("user-only first turn: %+v %v", first, err)
	}
	p.Attempt = strings.Repeat("e", 32)
	p.History = &BatchHistoryExpectation{Version: 1, Mode: "resume", Evidence: first.History.Evidence}
	second, err := run(p)
	if err != nil || !second.Started || second.History.Before != "verified" || second.History.After != "verified" {
		t.Fatalf("user-only resume: %+v %v", second, err)
	}
	// A file checkpoint is not evidence of an assistant response or completion.
	file := filepath.Join(dir, ".memoh-computer", "sessions", p.PiSessionID, first.History.Evidence.File)
	if strings.Contains(string(readBatchFixture(t, file)), `"role":"assistant"`) {
		t.Fatal("fixture unexpectedly contained an assistant message")
	}
}

func TestBatchHistoryResourceLimitIsUnconfirmed(t *testing.T) {
	p, _, dir, run := historyFixture(t)
	first, err := run(p)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, ".memoh-computer", "sessions", p.PiSessionID, first.History.Evidence.File)
	const size = (64 << 20) + 1
	if err = os.Truncate(file, size); err != nil {
		t.Fatal(err)
	}
	p.Attempt = strings.Repeat("e", 32)
	p.History = &BatchHistoryExpectation{Version: 1, Mode: "resume", Evidence: first.History.Evidence}
	r, err := run(p)
	if err != nil || r.Started || r.History.Before != "unconfirmed" {
		t.Fatalf("resource limit reported damage or started Pi: %+v %v", r, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Size() != size {
		t.Fatal("resource-limited history was changed", err)
	}
	if strings.Count(string(readBatchFixture(t, filepath.Join(dir, "calls"))), "\n") != 1 {
		t.Fatal("resource-limited history dispatched Pi")
	}
}
