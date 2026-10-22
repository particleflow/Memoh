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

// Preserve a byte-for-byte copy of the entire original flat layout (including
// payload evidence) outside the live queue. It is never fed back as new state.
func copyFlatLayout(t *testing.T, q *DiskQueue) func() {
	t.Helper()
	saved, err := os.OpenRoot(configFixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = saved.Close() })
	dir, err := q.root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	original := map[string][]byte{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			t.Fatal("fixture is not the original flat layout", e.Name())
		}
		raw, err := q.root.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		f, err := openPrivateFile(saved, e.Name(), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(raw)
		if err == nil {
			err = f.Sync()
		}
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		original[e.Name()] = bytes.Clone(raw)
	}
	return func() {
		t.Helper()
		for name, want := range original {
			got, err := saved.ReadFile(name)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("original backup changed", name, err)
			}
		}
	}
}

func legacyHistoryFixture(t *testing.T) (*DiskQueue, QueueStatus, func(string) string, []byte) {
	t.Helper()
	q, job, env, _, client := releaseFixture(t, releaseLimits)
	if err := q.RunOne(t.Context(), job.DeliveryID, client, releaseBinding(), time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	q.releaseHook = func(stage string) {
		if stage == "after_finish" {
			cancel()
		}
	}
	if err := q.ReleaseConfirmed(ctx, job.DeliveryID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	q.releaseHook = nil
	r, err := q.Inspect(job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	f, err := q.file("locator", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte{'2'}, int64(len(`{"Version":`))); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	raw, err := q.root.ReadFile("locator")
	if err != nil {
		t.Fatal(err)
	}
	return q, r, env, raw
}

func TestTerminalHistoryExplicitMigrationAndBackup(t *testing.T) {
	q, r, env, old := legacyHistoryFixture(t)
	// Unknown admission and its original payload must remain in the active layout.
	unknown := newJob([]byte("fixture"))
	unknown.DeliveryID, unknown.SnapshotID, unknown.LocatorRevision = "unknown-legacy", "unknown-legacy", "fixture"
	// This setup handle predates the fixture-only version downgrade. Production
	// migrations require the host to stop all such old handles first.
	cfg, err := OpenHostConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	source, err := cfg.OpenSource("result-one")
	if err != nil {
		t.Fatal(err)
	}
	err = q.Admit(t.Context(), "upload", unknown, source)
	_ = source.Close()
	_ = cfg.Close()
	if err != nil {
		t.Fatal(err)
	}
	before, err := q.Inspect(unknown.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	verifyBackup := copyFlatLayout(t, q)
	for _, readonly := range []bool{false, true} {
		if opened, err := openHerdrQueue(env, "fixture", releaseLimits, readonly); !errors.Is(err, ErrVersion) {
			if opened != nil {
				_ = opened.Close()
			}
			t.Fatal("implicit upgrade", err)
		}
	}
	_ = q.Close()
	for range 2 {
		if err := MigrateHerdrQueue(t.Context(), env, "fixture", releaseLimits); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := OpenHerdrQueue(env, "fixture", releaseLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	saved, err := reopened.root.ReadFile("locator-v2.backup")
	if err != nil || !bytes.Equal(saved, old) {
		t.Fatal("locator backup", err)
	}
	got, err := reopened.Inspect(r.Job.DeliveryID)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("migrated identity", err)
	}
	if err := reopened.Admit(t.Context(), "upload", queuedHistoryJob(r), nil); err != nil {
		t.Fatal("source-less replay", err)
	}
	if _, err := reopened.root.Lstat(queueName(r.Job.DeliveryID + ".journal")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("not archived", err)
	}
	got, err = reopened.Inspect(unknown.DeliveryID)
	if err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal("unknown changed", err)
	}
	if _, err := reopened.root.Lstat(queueName(unknown.DeliveryID + ".snapshot")); err != nil {
		t.Fatal("unknown payload removed", err)
	}
	verifyBackup()
}

func TestTerminalHistoryMigrationRejectsBusyAndMismatch(t *testing.T) {
	for _, kind := range []string{"busy", "revision", "limits", "backup"} {
		t.Run(kind, func(t *testing.T) {
			q, _, env, old := legacyHistoryFixture(t)
			verifyBackup := copyFlatLayout(t, q)
			revision, limits := "fixture", releaseLimits
			want := ErrVersion
			switch kind {
			case "busy":
				worker, err := q.worker()
				if err != nil {
					t.Fatal(err)
				}
				defer unlockClose(worker)
				want = ErrBusy
			case "revision":
				revision = "different"
			case "limits":
				limits.Bytes++
			case "backup":
				f, err := q.file("locator-v2.backup", os.O_CREATE|os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.Write([]byte("corrupt backup"))
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
				want = ErrStore
			}
			if err := MigrateHerdrQueue(t.Context(), env, revision, limits); !errors.Is(err, want) {
				t.Fatal(kind, err)
			}
			actual, err := q.root.ReadFile("locator")
			if err != nil || !bytes.Equal(actual, old) {
				t.Fatal("rejected migration changed locator", err)
			}
			verifyBackup()
		})
	}
}

func TestTerminalHistoryMigrationProcessCrash(t *testing.T) {
	if path := os.Getenv("MEMOH_MIGRATION_FIXTURE_ROOT"); path != "" {
		q, err := openQueueLayout(func(string) string { return path }, "fixture", releaseLimits, false, true, func(stage string) {
			if stage == os.Getenv("MEMOH_MIGRATION_FIXTURE_STAGE") {
				os.Exit(85)
			}
		})
		if q != nil {
			_ = q.Close()
		}
		t.Fatal("hook not reached", err)
	}
	for _, stage := range []string{"locator_after_backup", "locator_after_write", "locator_after_sync"} {
		t.Run(stage, func(t *testing.T) {
			q, r, env, old := legacyHistoryFixture(t)
			verifyBackup := copyFlatLayout(t, q)
			_ = q.Close()
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTerminalHistoryMigrationProcessCrash$") //nolint:gosec // Current isolated test executable and fixed action.
			cmd.Env = append(os.Environ(), "MEMOH_MIGRATION_FIXTURE_ROOT="+env("HERDR_PLUGIN_STATE_DIR"), "MEMOH_MIGRATION_FIXTURE_STAGE="+stage)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 85 {
				t.Fatalf("child: %v %s", err, out)
			}
			if err := MigrateHerdrQueue(t.Context(), env, "fixture", releaseLimits); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenHerdrQueue(env, "fixture", releaseLimits)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()
			got, err := reopened.Inspect(r.Job.DeliveryID)
			if err != nil || !reflect.DeepEqual(got, r) {
				t.Fatal("migration crash lost identity", err)
			}
			raw, err := reopened.root.ReadFile("locator-v2.backup")
			if err != nil || !bytes.Equal(raw, old) {
				t.Fatal("backup lost", err)
			}
			verifyBackup()
		})
	}
}

func TestTerminalHistoryMigrationFiniteBatch(t *testing.T) {
	q, r, env, _ := legacyHistoryFixture(t)
	for i := range historyReclaimBatch + 1 {
		next := historicalCopy(r, string(rune('a'+i))+"-legacy")
		putTerminal(t, q, next)
	}
	verifyBackup := copyFlatLayout(t, q)
	_ = q.Close()
	if err := MigrateHerdrQueue(t.Context(), env, "fixture", releaseLimits); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenHerdrQueue(env, "fixture", releaseLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	heads, err := reopened.admissions()
	if err != nil || len(heads) != 2 {
		t.Fatal("unbounded maintenance batch", len(heads), err)
	}
	if err := reopened.ReclaimConfirmed(t.Context()); err != nil {
		t.Fatal(err)
	}
	heads, err = reopened.admissions()
	if err != nil || len(heads) != 0 {
		t.Fatal("finite recovery did not drain", len(heads), err)
	}
	verifyBackup()
}

// Keep the exact original journal frame accessible during target creation.
func TestTerminalHistoryConcurrentIdentityAndPermanentLock(t *testing.T) {
	q, base, env := terminalFixture(t)
	r := historicalCopy(base, "concurrent-history")
	putTerminal(t, q, r)
	other, err := OpenHerdrQueue(env, "fixture", releaseLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	lock, err := q.lock(r.Job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := lock.Stat()
	unlockClose(lock)
	if err != nil {
		t.Fatal(err)
	}
	entered, proceed := make(chan struct{}), make(chan struct{})
	q.historyHook = func(stage string) {
		if stage == "after_target_sync" {
			close(entered)
			<-proceed
		}
	}
	done := make(chan error, 1)
	go func() { done <- q.ReleaseConfirmed(t.Context(), r.Job.DeliveryID) }()
	<-entered
	if _, err := other.Inspect(r.Job.DeliveryID); !errors.Is(err, ErrBusy) {
		t.Error("inspect crossed job lock", err)
	}
	if err := other.Admit(t.Context(), "upload", queuedHistoryJob(r), nil); !errors.Is(err, ErrBusy) {
		t.Error("admit crossed migration lock", err)
	}
	if err := other.RequestCancel(r.Job.DeliveryID); !errors.Is(err, ErrBusy) {
		t.Error("cancel crossed job lock", err)
	}
	close(proceed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	lock, err = other.lock(r.Job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := lock.Stat()
	unlockClose(lock)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("lock identity changed", err)
	}
	got, err := other.Inspect(r.Job.DeliveryID)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("concurrent identity", err)
	}
	// No consumer can append a new state to historical terminal records.
	if err := other.RequestCancel(r.Job.DeliveryID); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	got, err = other.Inspect(r.Job.DeliveryID)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("late cancel mutated history", err)
	}
}
