package sessiontask

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
)

func TestPersistedCompletionProjection(t *testing.T) {
	raw, err := os.ReadFile("testdata/compact-result.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, outcome, process string
		content                bool
		change                 func(*persistedPiFacts)
	}{
		{"success", "succeeded", "exited", true, func(*persistedPiFacts) {}},
		{"failed", "failed", "exited", true, func(b *persistedPiFacts) { b.Result.Outcome = "failed"; b.Result.StopReason = "error" }},
		{"aborted", "aborted", "exited", true, func(b *persistedPiFacts) { b.Result.Outcome = "aborted" }},
		{"incomplete", "incomplete", "exited", true, func(b *persistedPiFacts) { b.Result.Outcome = "incomplete"; b.Result.StopReason = "length" }},
		{"invalid_protocol", "invalid_protocol", "exited", false, func(b *persistedPiFacts) { b.Result = &agenvo.PiJSONResult{Outcome: "invalid_protocol"} }},
		{"output_limit", "output_limit", "exited", false, func(b *persistedPiFacts) { b.Result = &agenvo.PiJSONResult{Outcome: "output_limit"} }},
		{"not_started", "not_started", "not_started", false, func(b *persistedPiFacts) { b.Receipt.Started = false; b.Receipt.Completion = nil; b.Result = nil }},
		{"empty_final", "succeeded", "exited", false, func(b *persistedPiFacts) { b.Result.Output = "" }},
		{"truncated", "succeeded", "exited", true, func(b *persistedPiFacts) { b.Result.Truncated = true }},
		{"legacy", "unknown", "unknown", false, func(b *persistedPiFacts) { b.Progress.CompletionVersion = 0; b.Receipt.Completion = nil }},
		{"future_version", "unknown", "unknown", false, func(b *persistedPiFacts) { b.Progress.CompletionVersion = 2 }},
		{"missing_receipt", "unknown", "unknown", false, func(b *persistedPiFacts) { b.Receipt = nil }},
		{"wrong_attempt", "unknown", "unknown", false, func(b *persistedPiFacts) { b.Receipt.Attempt = strings.Repeat("f", 32) }},
		{"wrong_session", "unknown", "unknown", false, func(b *persistedPiFacts) { b.Receipt.PiSessionID = strings.Repeat("f", 64) }},
		{"no_exit", "unknown", "unknown", false, func(b *persistedPiFacts) { b.Receipt.Exited = false }},
		{"pending_result", "unknown", "exited", false, func(b *persistedPiFacts) { b.Result = nil }},
		{"missing_completion", "unknown", "exited", false, func(b *persistedPiFacts) { b.Receipt.Completion = nil }},
		{"bad_success", "unknown", "exited", false, func(b *persistedPiFacts) { b.Receipt.ExitCode = 7 }},
		{"oversized", "unknown", "exited", false, func(b *persistedPiFacts) { b.Result.Output = strings.Repeat("a", 2049) }},
		{"forged_outcome", "unknown", "exited", false, func(b *persistedPiFacts) { b.Result.Outcome = "running" }},
		{"too_few_records", "unknown", "exited", false, func(b *persistedPiFacts) { b.Result.Records = 7 }},
		{"invalid_with_text", "unknown", "exited", false, func(b *persistedPiFacts) { b.Result.Outcome = "invalid_protocol" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			var d struct {
				Kind  string            `json:"kind"`
				Batch *persistedPiFacts `json:"batch"`
			}
			if err := json.Unmarshal(raw, &d); err != nil {
				t.Fatal(err)
			}
			tc.change(d.Batch)
			r := f.operations[f.op]
			r.State, r.CancelRequested = "completed", true
			r.Details, _ = json.Marshal(d)
			f.operations[f.op] = r
			v, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
			if err != nil || v.PiResult != tc.outcome || v.ProcessState != tc.process || v.ContentAvailable != tc.content || v.AttachmentsAvailable || v.BusinessState != "unknown" || !*v.CancelRequested {
				t.Fatalf("%+v: %v", v, err)
			}
			if tc.content {
				if v.ResultSummary == nil || v.ResultSummary.Text != d.Batch.Result.Output || v.ResultSummary.Truncated != d.Batch.Result.Truncated || v.ResultSummary.Source != "pi_final_assistant" {
					t.Fatal(v.ResultSummary)
				}
			} else if v.ResultSummary != nil {
				t.Fatal("unavailable content supplied")
			}
			page, err := f.service.List(t.Context(), "bot", "user", f.root, 100, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range page.Items {
				if item.TaskID == f.op {
					a, _ := json.Marshal(v)
					b, _ := json.Marshal(item)
					if string(a) != string(b) {
						t.Fatal("list/detail disagree")
					}
				}
			}
		})
	}
}

func TestResultProjectionWhitelistAndNoWrite(t *testing.T) {
	f := newFixture()
	r := f.operations[f.op]
	r.State = "completed"
	var err error
	r.Details, err = os.ReadFile("testdata/compact-result.json")
	if err != nil {
		t.Fatal(err)
	}
	f.operations[f.op] = r
	before, _ := json.Marshal(f.operations)
	page, err := f.service.List(t.Context(), "bot", "user", f.root, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(page)
	for _, forbidden := range []string{"SECRET", "thinking", "stdout", "stderr", "sha256", "artifact_hash", "request_hash", "pane_id", "workspace_id", "stop_reason", "records"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("leaked %s", forbidden)
		}
	}
	if !strings.Contains(string(encoded), "Synthetic final answer") {
		t.Fatal("final summary missing")
	}
	after, _ := json.Marshal(f.operations)
	if string(before) != string(after) {
		t.Fatal("GET changed operation")
	}
	// Neither claimed success nor an exit code can override an uncompleted row.
	r.State = "submitted"
	f.operations[f.op] = r
	v, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
	if err != nil || v.PiResult != "unknown" || v.ResultSummary != nil {
		t.Fatal(v, err)
	}
}

func TestLateCompletionIsLiveWithoutChangingPaginationKeys(t *testing.T) {
	f := newFixture()
	// Guarantee the operation lies after the first page, regardless of UUIDs.
	f.addOperation(f.root, "pc-b", f.rows[0].CreatedAt.Add(1))
	first, err := f.service.List(t.Context(), "bot", "user", f.root, 1, "")
	if err != nil || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	r := f.operations[f.op]
	r.State, r.Revision = "completed", 2
	r.Details, err = os.ReadFile("testdata/compact-result.json")
	if err != nil {
		t.Fatal(err)
	}
	f.operations[f.op] = r
	next, err := f.service.List(t.Context(), "bot", "user", f.root, 100, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range next.Items {
		if v.TaskID == first.Items[0].TaskID {
			t.Fatal("duplicate across pages")
		}
		if v.TaskID == f.op {
			found = true
			if v.PiResult != "succeeded" || !v.ContentAvailable || v.Revision != "2" {
				t.Fatal(v)
			}
		}
	}
	if !found {
		t.Fatal("late completion disappeared")
	}
}
