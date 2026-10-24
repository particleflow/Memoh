package transferclient

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func budgetFixture(t *testing.T) (*DiskQueue, QueueStatus, QueueStatus, func(string) string, func()) {
	t.Helper()
	limits := releaseLimits
	limits.Jobs = 2
	limits.Bytes = 64 << 10
	q, job, env, _, _ := releaseFixture(t, limits)
	ready, err := q.Inspect(job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := OpenHostConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	source, err := cfg.OpenSource("result-one")
	if err != nil {
		t.Fatal(err)
	}
	job.DeliveryID, job.SnapshotID = "unknown-budget", "unknown-budget"
	err = q.Admit(t.Context(), "upload", job, source)
	_ = source.Close()
	_ = cfg.Close()
	if err != nil {
		t.Fatal(err)
	}
	r, f, err := q.load(job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	r.Job.Phase = "create_unknown"
	err = q.save(f, r)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	return q, ready, r, env, copyFlatLayout(t, q)
}

func TestBudgetMigrationPreservesIdentityAndUnknown(t *testing.T) {
	q, ready, unknown, env, backup := budgetFixture(t)
	old := q.limits
	original, _ := q.root.ReadFile("locator")
	_ = q.Close()
	// Ordinary upgrade with no policy change still opens the exact old queue.
	q, err := OpenHerdrQueue(env, "fixture", old)
	if err != nil {
		t.Fatal(err)
	}
	_ = q.Close()
	limits := old
	limits.Bytes = 8 << 30 // Parameter only: no multi-GiB allocation or claim.
	if opened, err := OpenHerdrQueue(env, "fixture", limits); !errors.Is(err, ErrVersion) {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatal("ordinary open changed budget", err)
	}
	for range 2 {
		if err := MigrateHerdrBudget(t.Context(), env, "fixture", limits); err != nil {
			t.Fatal(err)
		}
	}
	q, err = OpenHerdrQueue(env, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []QueueStatus{ready, unknown} {
		got, err := q.Inspect(want.Job.DeliveryID)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("migration changed delivery", err)
		}
		if _, err := q.root.Lstat(queueName(want.Job.SnapshotID + ".snapshot")); err != nil {
			t.Fatal("payload removed", err)
		}
	}
	actual, _ := q.root.ReadFile("locator")
	original[len(`{"Version":`)] = '4'
	if !bytes.Equal(actual, original) {
		t.Fatal("locator identity rewritten")
	}
	// Ready can commit after migration; released same-ID replay needs no source.
	_, client := newFixture(t)
	if err := q.RunOne(t.Context(), ready.Job.DeliveryID, client, releaseBinding(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := q.ReleaseConfirmed(t.Context(), ready.Job.DeliveryID); err != nil {
		t.Fatal(err)
	}
	if err := q.Admit(t.Context(), "upload", queuedHistoryJob(ready), nil); err != nil {
		t.Fatal("source-less same-ID recovery", err)
	}
	_ = q.Close()
	if opened, err := OpenHerdrQueue(env, "fixture", old); !errors.Is(err, ErrVersion) {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatal("old budget accepted", err)
	}
	if err := MigrateHerdrBudget(t.Context(), env, "fixture", old); !errors.Is(err, ErrVersion) {
		t.Fatal("decrease accepted", err)
	}
	limits.Bytes = 16 << 30
	if err := MigrateHerdrBudget(t.Context(), env, "fixture", limits); err != nil {
		t.Fatal("second bounded increase", err)
	}
	backup()
}

func TestBudgetMigrationRejectsBusyMismatchAndCapacity(t *testing.T) {
	q, _, _, env, backup := budgetFixture(t)
	defer func() { _ = q.Close() }()
	old, _ := q.root.ReadFile("locator")
	limits := q.limits
	limits.Bytes *= 2
	worker, err := q.worker()
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateHerdrBudget(t.Context(), env, "fixture", limits); !errors.Is(err, ErrBusy) {
		t.Fatal("active writer not fenced", err)
	}
	unlockClose(worker)
	changed := limits
	changed.Workers = 2
	for _, bad := range []struct {
		revision string
		limits   QueueLimits
	}{{"other", limits}, {"fixture", changed}} {
		if err := MigrateHerdrBudget(t.Context(), env, bad.revision, bad.limits); !errors.Is(err, ErrVersion) {
			t.Fatal("identity changed", err)
		}
	}
	actual, _ := q.root.ReadFile("locator")
	if !bytes.Equal(actual, old) {
		t.Fatal("rejection modified locator")
	}
	job := newJob([]byte("fixture"))
	job.DeliveryID, job.SnapshotID, job.LocatorRevision = "over-budget", "over-budget", "fixture"
	job.Spec.SizeBytes = q.limits.Bytes + 1
	if err := q.Admit(t.Context(), "upload", job, nil); !errors.Is(err, ErrCapacity) || LifecycleFailure(err).ErrorClass != "capacity" {
		t.Fatal("capacity not explicit", err)
	}
	backup()
}

func TestBudgetMigrationResumesIncompleteAdmission(t *testing.T) {
	env, _, _ := producerFixture(t, "https://fixture.invalid")
	q, err := OpenHerdrQueue(env, "fixture", releaseLimits)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := OpenHostConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	source, err := cfg.OpenSource("result-one")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	job := newJob([]byte("fixture"))
	job.SnapshotID, job.LocatorRevision = job.DeliveryID, "fixture"
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := q.Admit(ctx, "upload", job, source); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	before, err := q.Inspect(job.DeliveryID)
	if !errors.Is(err, ErrAdmissionPending) || before.Admitted {
		t.Fatal("missing partial admission", err)
	}
	_ = q.Close()
	limits := releaseLimits
	limits.Bytes = 8 << 30
	if err := MigrateHerdrBudget(t.Context(), env, "fixture", limits); err != nil {
		t.Fatal(err)
	}
	q, err = OpenHerdrQueue(env, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	got, err := q.Inspect(job.DeliveryID)
	if !errors.Is(err, ErrAdmissionPending) || !reflect.DeepEqual(got, before) {
		t.Fatal("partial identity changed", err)
	}
	if err := q.Admit(t.Context(), "upload", job, source); err != nil {
		t.Fatal("same-ID resume", err)
	}
	got, err = q.Inspect(job.DeliveryID)
	if err != nil || !got.Admitted || got.Seal == nil || got.Job != before.Job {
		t.Fatal("resume recreated job", err)
	}
}

func TestBudgetMigrationProcessCrashAndTornTail(t *testing.T) {
	limits := releaseLimits
	limits.Jobs, limits.Bytes = 2, 8<<30
	if path := os.Getenv("MEMOH_BUDGET_FIXTURE_ROOT"); path != "" {
		q, err := openQueueLayoutMode(func(string) string { return path }, "fixture", limits, false, false, true, func(stage string) {
			if stage == os.Getenv("MEMOH_BUDGET_FIXTURE_STAGE") {
				os.Exit(85)
			}
		})
		if q != nil {
			_ = q.Close()
		}
		t.Fatal("crash hook missing", err)
	}
	for _, stage := range []string{"budget_after_base", "budget_after_gate", "budget_after_publication", "budget_after_commit"} {
		t.Run(stage, func(t *testing.T) {
			q, ready, unknown, env, backup := budgetFixture(t)
			_ = q.Close()
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestBudgetMigrationProcessCrashAndTornTail$") //nolint:gosec // Isolated current test executable, no user command.
			cmd.Env = append(os.Environ(), "MEMOH_BUDGET_FIXTURE_ROOT="+env("HERDR_PLUGIN_STATE_DIR"), "MEMOH_BUDGET_FIXTURE_STAGE="+stage)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 85 {
				t.Fatalf("child: %v %s", err, out)
			}
			if err := MigrateHerdrBudget(t.Context(), env, "fixture", limits); err != nil {
				t.Fatal(err)
			}
			q, err = OpenHerdrQueue(env, "fixture", limits)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []QueueStatus{ready, unknown} {
				got, err := q.Inspect(want.Job.DeliveryID)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatal("crash changed identity", err)
				}
			}
			f, err := q.file("capacity-updates", os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			stat, _ := f.Stat()
			_, err = f.WriteAt([]byte{0, 1, 2}, stat.Size())
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
			_ = q.Close()
			// Read-only opens leave a torn tail unchanged; writable reopen repairs it.
			q, err = openHerdrQueue(env, "fixture", limits, true)
			if err != nil {
				t.Fatal(err)
			}
			info, _ := q.root.Stat("capacity-updates")
			if info.Size() != stat.Size()+3 {
				t.Fatal("read-only repair")
			}
			_ = q.Close()
			if err := MigrateHerdrBudget(t.Context(), env, "fixture", limits); err != nil {
				t.Fatal(err)
			}
			q, err = OpenHerdrQueue(env, "fixture", limits)
			if err != nil {
				t.Fatal(err)
			}
			f, err = q.file("capacity-updates", os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteAt([]byte{255}, 36)
			_ = f.Close()
			_ = q.Close()
			if err != nil {
				t.Fatal(err)
			}
			if q, err := OpenHerdrQueue(env, "fixture", limits); !errors.Is(err, ErrStore) {
				if q != nil {
					_ = q.Close()
				}
				t.Fatal("complete corruption accepted", err)
			}
			backup()
		})
	}
}
