package deliverymeta

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFeedbackStrictOriginalBindingAndBound(t *testing.T) {
	p := Plan{Version: 2, Task: Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: strings.Repeat("\x01", 256), Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: RootHash(`C:\Fixture`)}}
	for i := range MaxArtifacts {
		p.Artifacts = append(p.Artifacts, Artifact{SourceRef: fmt.Sprintf("file_%d", i), Mime: "text/plain"})
	}
	original := Feedback{Version: 1, Task: p.Task, PlanHash: PlanHash(p)}
	for _, file := range p.FilePlans() {
		original.Files = append(original.Files, FileFeedback{DeliveryID: file.DeliveryID(), State: "unknown", Code: "association_requires_reconciliation", Change: strings.Repeat("c", 64)})
	}
	raw, _ := json.Marshal(original)
	if len(raw) >= FeedbackMaxBytes {
		t.Fatal("maximum plan exceeds read budget", len(raw))
	}
	if _, ok := DecodeFeedback(raw, p); !ok {
		t.Fatal("valid maximum plan rejected")
	}
	page := Plan{Version: 1, Task: p.Task, Artifact: Artifact{SourceRef: "later-page", Mime: "text/plain"}}
	paged := Feedback{Version: 2, Task: p.Task, PlanHash: PlanHash(p), Page: &page, Files: []FileFeedback{{DeliveryID: page.DeliveryID(), State: "unknown", Code: "awaiting_nas_ledger", Change: strings.Repeat("e", 64)}}}
	pagedRaw, _ := json.Marshal(paged)
	if _, ok := DecodeFeedback(pagedRaw, p); !ok {
		t.Fatal("bound later page rejected")
	}
	page.Task.Attempt = strings.Repeat("f", 32)
	pagedRaw, _ = json.Marshal(paged)
	if _, ok := DecodeFeedback(pagedRaw, p); ok {
		t.Fatal("foreign page cleared original scope")
	}
	for name, mutate := range map[string]func(*Feedback){
		"operation":       func(f *Feedback) { f.Task.OperationID = uuid.NewString() },
		"bot":             func(f *Feedback) { f.Task.BotID = uuid.NewString() },
		"user":            func(f *Feedback) { f.Task.UserID = uuid.NewString() },
		"session":         func(f *Feedback) { f.Task.SessionID = uuid.NewString() },
		"target":          func(f *Feedback) { f.Task.TargetID = uuid.NewString() },
		"run":             func(f *Feedback) { f.Task.RunID = "foreign" },
		"attempt":         func(f *Feedback) { f.Task.Attempt = strings.Repeat("d", 32) },
		"generation":      func(f *Feedback) { f.Task.Generation = strings.Repeat("d", 32) },
		"root":            func(f *Feedback) { f.Task.RootHash = RootHash(`C:\Other`) },
		"plan":            func(f *Feedback) { f.PlanHash = strings.Repeat("d", 64) },
		"duplicate":       func(f *Feedback) { f.Files[1] = f.Files[0] },
		"missing":         func(f *Feedback) { f.Files = f.Files[:1] },
		"delivered":       func(f *Feedback) { f.Files[0].State = "delivered" },
		"raw-error":       func(f *Feedback) { f.Files[0].Code = "PRIVATE-RAW-ERROR" },
		"retry-unknown":   func(f *Feedback) { f.Files[0].Retryable = true },
		"unknown-backoff": func(f *Feedback) { f.Files[0].RetryAt = time.Now() },
	} {
		t.Run(name, func(t *testing.T) {
			var f Feedback
			_ = json.Unmarshal(raw, &f)
			mutate(&f)
			bad, _ := json.Marshal(f)
			if _, ok := DecodeFeedback(bad, p); ok {
				t.Fatal("unbound receipt accepted")
			}
		})
	}
	for _, bad := range [][]byte{nil, append(raw, []byte(` {}`)...), []byte(strings.Repeat(" ", FeedbackMaxBytes)), append([]byte(`{"reference":"PRIVATE",`), raw[1:]...)} {
		if _, ok := DecodeFeedback(bad, p); ok {
			t.Fatal("unbounded/unknown fields accepted")
		}
	}
	for _, bad := range []string{"../escape", strings.Repeat("a", 31), strings.Repeat("A", 32)} {
		if _, ok := AttemptDirectory(bad); ok {
			t.Fatal("unsafe attempt")
		}
	}
}
