package transferclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
)

func TestProducerFeedbackRegisteredPageOutsideInitialDeclaration(t *testing.T) {
	getenv, first, write := producerFixture(t, "https://fixture.invalid")
	writeProducerContextFixture(t, getenv, first.Plan)
	second := first
	second.Artifact.SourceRef = "second-page-result"
	write("source", second.Artifact.SourceRef, []byte("fixture"))
	limits := QueueLimits{Jobs: 1, Bytes: 16384, JournalBytes: 65536, Workers: 1}
	raw, _ := json.Marshal(deliverymeta.ClosedPage{Version: 1, Files: []deliverymeta.Closed{second}})
	out, err := EnqueueProducerPage(t.Context(), getenv, "fixture", limits, bytes.NewReader(raw), nil, time.Now())
	if err != nil || len(out.Files) != 1 || !out.Files[0].Registered {
		t.Fatal("page not registered", err)
	}
	q, err := OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	r, err := q.Inspect(second.DeliveryID())
	_ = q.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := PublishProducerFeedback(t.Context(), getenv, "fixture", limits, r.Job, time.Now()); err != nil {
		t.Fatal("registered second page has unavailable feedback", err)
	}
	f := readProducerFeedbackFixture(t, getenv, first.Plan)
	if f.Version != 2 || f.Page == nil || f.Page.DeliveryID() != second.DeliveryID() || f.PlanHash != deliverymeta.PlanHash(first.Plan) {
		t.Fatal("page lost immutable context binding")
	}
	foreign := r.Job
	foreign.DeliveryID = strings.Repeat("e", 64)
	if PublishProducerFeedback(t.Context(), getenv, "fixture", limits, foreign, time.Now()) == nil {
		t.Fatal("self-reported unregistered page accepted")
	}
	foreign = r.Job
	foreign.Spec.SizeBytes++
	if PublishProducerFeedback(t.Context(), getenv, "fixture", limits, foreign, time.Now()) == nil {
		t.Fatal("page metadata conflicts with original queue record")
	}
}

func writeProducerContextFixture(t *testing.T, getenv func(string) string, plan deliverymeta.Plan) {
	t.Helper()
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	root, err := os.OpenRoot(cfg.sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	relative, ok := deliverymeta.AttemptDirectory(plan.Task.Attempt)
	if !ok {
		t.Fatal("attempt")
	}
	current := root
	for _, segment := range strings.Split(relative, "/") {
		if err := makeQueueDir(current, segment); err != nil && !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		next, err := current.OpenRoot(segment)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = next.Close() }()
		current = next
	}
	f, err := openPrivateFile(current, deliverymeta.ProducerContextName, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	raw, _ := json.Marshal(plan)
	if f.Truncate(0) != nil {
		t.Fatal("truncate")
	}
	if _, err := f.Write(raw); err != nil {
		t.Fatal(err)
	}
	if f.Sync() != nil {
		t.Fatal("sync")
	}
}

func readProducerFeedbackFixture(t *testing.T, getenv func(string) string, plan deliverymeta.Plan) *deliverymeta.Feedback {
	t.Helper()
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	relative, _ := deliverymeta.AttemptDirectory(plan.Task.Attempt)
	root, guard, err := privateHostRoot(filepath.Join(cfg.sourcePath, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { closeQueueGuard(guard); _ = root.Close() }()
	f, err := openPrivateFile(root, deliverymeta.ProducerFeedbackName, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(f)
	report, ok := deliverymeta.DecodeFeedback(raw, plan)
	if err != nil || !ok {
		t.Fatal("invalid bound feedback", err)
	}
	for _, secret := range []string{"fixture-stable-device", cfg.sourcePath, cfg.Origin, "reference", "PRIVATE-OUTPUT"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private data or success reference in feedback")
		}
	}
	return report
}

func TestProducerFeedbackScopeAndUnchangedBlock(t *testing.T) {
	getenv, closed, write := producerFixture(t, "https://fixture.invalid")
	writeProducerContextFixture(t, getenv, closed.Plan)
	limits := QueueLimits{Jobs: 1, Bytes: 16384, JournalBytes: 65536, Workers: 1}
	raw, _ := json.Marshal(closed)
	write("source", closed.ManifestName(), raw)
	action, _, err := ProducerAdmission(strings.NewReader(string(raw)), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	action.Job.HostBinding = cfg.Binding
	source, err := cfg.OpenSource(closed.Artifact.SourceRef)
	if err != nil {
		t.Fatal(err)
	}
	q, err := OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	if err := q.Admit(t.Context(), "upload", action.Job, source); err != nil {
		t.Fatal(err)
	}
	_ = source.Close()
	_ = cfg.Close()
	r, f, err := q.load(action.Job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	r.Job.Phase, r.Job.ErrorCode = "blocked", "authorization"
	err = q.save(f, r)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := PublishProducerFeedback(t.Context(), getenv, "fixture", limits, action.Job, time.Now()); err != nil {
		t.Fatal(err)
	}
	first := readProducerFeedbackFixture(t, getenv, closed.Plan)
	if first.Files[0].State != "blocked" || first.Files[0].Code != "authorization" || first.Files[0].Retryable {
		t.Fatal("denial not classified")
	}
	if err := PublishProducerFeedback(t.Context(), getenv, "fixture", limits, action.Job, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	second := readProducerFeedbackFixture(t, getenv, closed.Plan)
	if first.Hash() != second.Hash() {
		t.Fatal("unchanged block changed observation")
	}
	foreign := action.Job
	foreign.Task.Attempt = strings.Repeat("f", 32)
	if err := PublishProducerFeedback(t.Context(), getenv, "fixture", limits, foreign, time.Now()); !errors.Is(err, ErrProducerFeedback) {
		t.Fatal("foreign attempt accepted", err)
	}
	if err := q.capacity(7); !errors.Is(err, ErrCapacity) {
		t.Fatal("blocked evidence reclaimed", err)
	}
}
