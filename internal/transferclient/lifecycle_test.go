package transferclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var lifecycleLimits = QueueLimits{Jobs: 8, Bytes: 1 << 20, JournalBytes: 65536, Workers: 1}

func lifecycleFixture(t *testing.T) (func(string) string, *DiskQueue, string) {
	t.Helper()
	env, _, _ := producerFixture(t, "https://fixture.invalid")
	q, err := OpenHerdrQueue(env, "fixture", lifecycleLimits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	cfg, err := OpenHostConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	binding := cfg.Binding
	_ = cfg.Close()
	return env, q, binding
}

func lifecycleAdmit(t *testing.T, q *DiskQueue, id, binding string) {
	t.Helper()
	j := newJob([]byte("fixture"))
	j.Task = DeviceTask{UserID: "user", SessionID: "session", TargetID: "target", RunID: "run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	j.DeliveryID, j.SnapshotID, j.LocatorRevision, j.HostBinding = id, id, "fixture", binding
	if err := q.Admit(t.Context(), "download", j, nil); err != nil {
		t.Fatal(err)
	}
}

func lifecycleEdit(t *testing.T, q *DiskQueue, id string, edit func(*QueueStatus)) {
	t.Helper()
	lock, err := q.lock(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockClose(lock)
	r, f, err := q.load(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	edit(&r)
	if err := q.save(f, r); err != nil {
		t.Fatal(err)
	}
}

func lifecycleFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			raw, err := os.ReadFile(path) //nolint:gosec // Enumerated files in this isolated private fixture.
			if err != nil {
				return err
			}
			stat, err := d.Info()
			if err != nil {
				return err
			}
			out[path] = string(raw) + stat.ModTime().UTC().Format(time.RFC3339Nano)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLifecycleReadOnlyMixedDueAndBackoff(t *testing.T) {
	env, q, binding := lifecycleFixture(t)
	now := time.Now().UTC()
	for _, id := range []string{"a-blocked", "b-unknown", "c-due"} {
		lifecycleAdmit(t, q, id, binding)
	}
	lifecycleEdit(t, q, "a-blocked", func(r *QueueStatus) { r.Job.Phase, r.Job.ErrorCode = "blocked", "authorization" })
	lifecycleEdit(t, q, "b-unknown", func(r *QueueStatus) { r.Job.Phase = "create_unknown" })
	before := lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))
	summary, err := LifecycleSummary(t.Context(), env, "fixture", lifecycleLimits, now)
	if err != nil || summary.State != "due" || summary.NextDueAt != nil {
		t.Fatal(summary, err)
	}
	if !reflect.DeepEqual(before, lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))) {
		t.Fatal("summary mutated state")
	}
	if id, err := q.nextDue(now); err != nil || id != "c-due" {
		t.Fatal("independent work starved", id, err)
	}
	deadline := now.Add(time.Minute)
	lifecycleEdit(t, q, "c-due", func(r *QueueStatus) { r.Job.NextAttempt = now.Add(time.Second); r.AssociationDue = deadline })
	summary, err = LifecycleSummary(t.Context(), env, "fixture", lifecycleLimits, now)
	if err != nil || summary.State != "waiting" || !summary.NextDueAt.Equal(deadline) {
		t.Fatal(summary, err)
	}
	if _, err := q.nextDue(now); !errors.Is(err, ErrNoWork) {
		t.Fatal("backoff bypass", err)
	}
	lifecycleEdit(t, q, "c-due", func(r *QueueStatus) { r.Job.Phase, r.Job.ErrorCode = "blocked", "authorization" })
	summary, err = LifecycleSummary(t.Context(), env, "fixture", lifecycleLimits, now)
	if err != nil || summary.State != "unknown" {
		t.Fatal("uncertain create hidden", summary, err)
	}
}

func TestLifecycleReadOnlyTornAndCheckpoint(t *testing.T) {
	for _, mode := range []string{"torn", "checkpoint", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			env, q, binding := lifecycleFixture(t)
			lifecycleAdmit(t, q, "one", binding)
			r, f, err := q.load("one")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "checkpoint" {
				// Exact compaction crash boundary, then release the process locks.
				func() {
					defer func() {
						if recover() != "fixture-stop" {
							t.Fatal("missing checkpoint crash")
						}
					}()
					q.journalHook = func(stage string) {
						if stage == "after_truncate" {
							panic("fixture-stop")
						}
					}
					r.Version = 4
					if err := q.compactActiveJournal(f, r); err != nil {
						t.Fatal(err)
					}
				}()
			} else {
				if _, err := f.Seek(0, 2); err != nil {
					t.Fatal(err)
				}
				data := []byte{0, 0}
				if mode == "corrupt" {
					data = bytes.Repeat([]byte{255}, 36)
				}
				if _, err := f.Write(data); err != nil {
					t.Fatal(err)
				}
				if err := f.Sync(); err != nil {
					t.Fatal(err)
				}
			}
			_ = f.Close()
			before := lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))
			summary, err := LifecycleSummary(t.Context(), env, "fixture", lifecycleLimits, time.Now())
			if mode == "corrupt" {
				if summary.State != "unknown" {
					t.Fatal(summary, err)
				}
			} else if err != nil || summary.State != "due" {
				t.Fatal(summary, err)
			}
			if !reflect.DeepEqual(before, lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))) {
				t.Fatal("summary repaired journal or witness")
			}
		})
	}
}

func TestLifecycleEmptyBusyAndClassification(t *testing.T) {
	env, q, _ := lifecycleFixture(t)
	before := lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))
	summary, err := LifecycleSummary(t.Context(), env, "fixture", lifecycleLimits, time.Now())
	if err != nil || summary.State != "idle" || summary.NextDueAt != nil {
		t.Fatal(summary, err)
	}
	if !reflect.DeepEqual(before, lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))) {
		t.Fatal("empty summary mutated state")
	}
	lock, err := q.lock("admission")
	if err != nil {
		t.Fatal(err)
	}
	summary, err = LifecycleSummary(t.Context(), env, "fixture", lifecycleLimits, time.Now())
	unlockClose(lock)
	if !errors.Is(err, ErrBusy) || summary.ErrorClass != "busy" || summary.NextDueAt != nil {
		t.Fatal(summary, err)
	}
	for _, test := range []struct {
		err   error
		class string
	}{{ErrAuthorization, "authentication"}, {&HTTPError{Status: 403}, "authentication"}, {ErrPermission, "permission"}, {ErrVersion, "configuration"}, {ErrCapacity, "capacity"}, {ErrTransport, "transient"}, {ErrStore, "unknown"}} {
		if s := LifecycleFailure(test.err); s.ErrorClass != test.class {
			t.Fatal(s)
		}
	}
	raw, _ := json.Marshal(summary)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 4 || fields["schema"] != "memoh.delivery.lifecycle.v1" {
		t.Fatal("contract changed")
	}
}

func TestLifecycleEarliestDueAndFairRotation(t *testing.T) {
	_, q, binding := lifecycleFixture(t)
	now := time.Now().UTC()
	for _, id := range []string{"a", "b", "c"} {
		lifecycleAdmit(t, q, id, binding)
	}
	lifecycleEdit(t, q, "a", func(r *QueueStatus) { r.Job.NextAttempt = now.Add(time.Minute) })
	lifecycleEdit(t, q, "b", func(r *QueueStatus) { r.Job.NextAttempt = now.Add(-time.Second) })
	lifecycleEdit(t, q, "c", func(r *QueueStatus) { r.Job.NextAttempt = now.Add(-time.Minute) })
	if id, err := q.nextDue(now); err != nil || id != "c" {
		t.Fatal(id, err)
	}
	for _, id := range []string{"a", "b", "c"} {
		lifecycleEdit(t, q, id, func(r *QueueStatus) { r.Job.NextAttempt = time.Time{} })
	}
	for _, want := range []string{"a", "b", "c", "a"} {
		id, err := q.nextDue(now)
		if err != nil || id != want {
			t.Fatal("unfair selection", id, want, err)
		}
		lifecycleEdit(t, q, id, func(r *QueueStatus) { r.LastRun = now })
		now = now.Add(time.Second)
	}
}

func TestLifecycleInvalidTaskDoesNotStarveConfiguredWork(t *testing.T) {
	env, q, binding := lifecycleFixture(t)
	lifecycleAdmit(t, q, "a", binding)
	lifecycleAdmit(t, q, "b", binding)
	lifecycleEdit(t, q, "a", func(r *QueueStatus) { r.Job.Task = DeviceTask{} })
	if id, err := q.nextDueForBinding(time.Now(), binding); err != nil || id != "b" {
		t.Fatal(id, err)
	}
	s, err := LifecycleSummary(t.Context(), env, "fixture", lifecycleLimits, time.Now())
	if err != nil || s.State != "due" {
		t.Fatal(s, err)
	}
	lifecycleEdit(t, q, "b", func(r *QueueStatus) { r.Job.HostBinding = "old-binding" })
	if _, err := q.nextDueForBinding(time.Now(), binding); !errors.Is(err, ErrNoWork) {
		t.Fatal(err)
	}
	s, err = LifecycleSummary(t.Context(), env, "fixture", lifecycleLimits, time.Now())
	if err != nil || s.State != "blocked" || s.ErrorClass != "configuration" {
		t.Fatal(s, err)
	}
}

func TestLifecycleCorruptTailBlocksActualAdvance(t *testing.T) {
	env, q, binding := lifecycleFixture(t)
	for _, id := range []string{"a-corrupt", "b-due"} {
		lifecycleAdmit(t, q, id, binding)
	}
	f, err := q.file("a-corrupt.journal", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Seek(0, 2); err != nil {
		t.Fatal(err)
	}
	// Complete frame with a wrong checksum: the first admission frame is valid.
	frame := make([]byte, 37)
	frame[3] = 1
	frame[36] = 'x'
	if _, err = f.Write(frame); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	badBefore, err := q.root.ReadFile(queueName("a-corrupt.journal"))
	if err != nil {
		t.Fatal(err)
	}
	before := lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))
	summary, err := LifecycleSummary(t.Context(), env, "fixture", lifecycleLimits, time.Now())
	if !errors.Is(err, ErrStore) || summary.State != "unknown" || summary.ErrorClass != "unknown" || summary.NextDueAt != nil {
		t.Fatal("impossible due work advertised", summary, err)
	}
	if !reflect.DeepEqual(before, lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))) {
		t.Fatal("corrupt evidence changed")
	}
	if _, err := AdvanceProducer(t.Context(), env, "fixture", lifecycleLimits, nil, time.Now()); !errors.Is(err, ErrStore) {
		t.Fatal("actual blocker hidden", err)
	}
	bad, err := q.root.ReadFile(queueName("a-corrupt.journal"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(badBefore, bad) {
		t.Fatal("failed advance erased evidence")
	}
	other, err := q.Inspect("b-due")
	if err != nil || other.Job.Phase != "queued" || other.Job.Attempts != 0 || other.Job.ReceiptID != "" {
		t.Fatal("unexpected independent mutation", err)
	}
}
