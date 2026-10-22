package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func terminalFixture(t *testing.T) (*DiskQueue, QueueStatus, func(string) string) {
	t.Helper()
	q, job, getenv, _, client := releaseFixture(t, releaseLimits)
	if err := q.RunOne(t.Context(), job.DeliveryID, client, releaseBinding(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := q.ReleaseConfirmed(t.Context(), job.DeliveryID); err != nil {
		t.Fatal(err)
	}
	r, err := q.Inspect(job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	r.Job.HostBinding = "fixture-host"
	r.Job.Task = DeviceTask{UserID: "user", SessionID: "session", TargetID: "target", RunID: "run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	return q, r, getenv
}

func historicalCopy(base QueueStatus, id string) QueueStatus {
	next := base
	next.Job.DeliveryID, next.Job.SnapshotID = id, id
	ref := *base.Job.Ready
	ref.DeliveryID = id
	next.Job.Ready = &ref
	return next
}

func putTerminal(t *testing.T, q *DiskQueue, r QueueStatus) {
	t.Helper()
	file, err := q.file(r.Job.DeliveryID+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = q.saveReceipt(file, r)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func queuedHistoryJob(r QueueStatus) Job {
	j := r.Job
	j.Phase = "queued"
	j.Ready = nil
	j.ReceiptID = ""
	j.Offset = 0
	j.ChunkBytes = 0
	j.ExpiresAt = time.Time{}
	j.Attempts = 0
	j.ErrorCode = ""
	j.NextAttempt = time.Time{}
	return j
}

func TestTerminalHistoryBeyondCatalogBoundAndOriginalID(t *testing.T) {
	q, base, getenv := terminalFixture(t)
	const total = 1152
	// Only compact synthetic acknowledged records are populated here, not 1152
	// NAS transfers. Real queue files, locks, archive synchronization and reopen.
	for n := range total {
		r := historicalCopy(base, fmt.Sprintf("historical-%04d", n))
		putTerminal(t, q, r)
		if n%16 == 15 {
			if err := q.ReclaimConfirmed(t.Context()); err != nil {
				t.Fatal(n, err)
			}
			if records, err := q.admissions(); err != nil || len(records) != 0 {
				t.Fatal("history leaked into active enumeration", len(records), err)
			}
		}
	}
	if err := q.capacity(7); err != nil {
		t.Fatal("completed history blocked active capacity", err)
	}
	reopened, err := OpenHerdrQueue(getenv, "fixture", releaseLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	// Remove only the isolated producer source to prove replay does not depend on it.
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	source, err := cfg.OpenSource("result-one")
	if err != nil {
		t.Fatal(err)
	}
	_ = source.Close()
	if err := cfg.sources.Remove("result-one"); err != nil {
		t.Fatal(err)
	}
	_ = cfg.Close()
	for n := range total {
		want := historicalCopy(base, fmt.Sprintf("historical-%04d", n))
		got, err := reopened.Inspect(want.Job.DeliveryID)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("full historical identity changed", n, err)
		}
		if err := reopened.Admit(t.Context(), "upload", queuedHistoryJob(want), nil); err != nil {
			t.Fatal("source-less ID replay", n, err)
		}
	}
	want := historicalCopy(base, "historical-0000")
	conflict := queuedHistoryJob(want)
	conflict.Task.Attempt = strings.Repeat("c", 32)
	if err := reopened.Admit(t.Context(), "upload", conflict, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("cross-attempt replay", err)
	}
	conflict = queuedHistoryJob(want)
	conflict.Spec.SHA256 = digest([]byte("different"))
	if err := reopened.Admit(t.Context(), "upload", conflict, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("changed hash replay", err)
	}
	dir, err := q.root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	locks, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) > 136 {
		t.Fatal("active directory grows with history", len(locks))
	}
}

func TestTerminalHistoryUnknownIsNeverArchived(t *testing.T) {
	q, job, _, _, client := releaseFixture(t, releaseLimits)
	binding := releaseBinding()
	binding.Commit = func(context.Context, string, Reference, io.Reader) error { return context.DeadlineExceeded }
	if err := q.RunOne(t.Context(), job.DeliveryID, client, binding, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := q.Inspect(job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.ReclaimConfirmed(t.Context()); err != nil {
		t.Fatal(err)
	}
	raw, file, found, err := q.readHistory(job.DeliveryID)
	if file != nil {
		_ = file.Close()
	}
	if err != nil || found || len(raw) != 0 {
		t.Fatal("unknown became history", err)
	}
	after, err := q.Inspect(job.DeliveryID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("unknown evidence changed", err)
	}
	if _, err := q.root.Stat(queueName(job.DeliveryID + ".snapshot")); err != nil {
		t.Fatal("unknown payload lost", err)
	}
}

func TestTerminalHistoryPartialAndConflictingCopies(t *testing.T) {
	for _, mode := range []string{"partial", "different-ref", "different-attempt", "corrupt", "source-missing-partial"} {
		t.Run(mode, func(t *testing.T) {
			q, base, _ := terminalFixture(t)
			r := historicalCopy(base, "duplicate")
			putTerminal(t, q, r)
			frame, err := historyFrame(r)
			if err != nil {
				t.Fatal(err)
			}
			root, closeRoot, err := q.historyBucket(r.Job.DeliveryID, true)
			if err != nil {
				t.Fatal(err)
			}
			defer closeRoot()
			switch mode {
			case "partial", "source-missing-partial":
				frame = frame[:41]
			case "different-ref":
				other := r
				ref := *r.Job.Ready
				ref.ContentHash = digest([]byte("changed"))
				other.Job.Spec.SHA256 = ref.ContentHash
				other.Job.Ready = &ref
				frame, _ = historyFrame(other)
			case "different-attempt":
				other := r
				other.Job.Task.Attempt = strings.Repeat("d", 32)
				frame, _ = historyFrame(other)
			case "corrupt":
				frame[4] ^= 1
			}
			name := queueName(r.Job.DeliveryID + ".journal")
			target, err := openPrivateFile(root, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = target.Write(frame)
			_ = target.Close()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "source-missing-partial" {
				if err := removeQueueFile(q.root, name); err != nil {
					t.Fatal(err)
				}
			}
			err = q.ReleaseConfirmed(t.Context(), r.Job.DeliveryID)
			if mode == "partial" {
				if err != nil {
					t.Fatal("matching partial copy not recovered", err)
				}
				got, readErr := q.Inspect(r.Job.DeliveryID)
				if readErr != nil || !reflect.DeepEqual(got, r) {
					t.Fatal("partial recovery changed identity", readErr)
				}
			} else {
				if err == nil {
					t.Fatal("conflict acknowledged")
				}
				got, readErr := root.ReadFile(name)
				if readErr != nil || !bytes.Equal(got, frame) {
					t.Fatal("conflict erased", readErr)
				}
				if mode != "source-missing-partial" {
					if _, err := q.root.Stat(name); err != nil {
						t.Fatal("source erased", err)
					}
				}
			}
		})
	}
}

func TestTerminalHistoryBucketBudgetPreservesSource(t *testing.T) {
	q, base, _ := terminalFixture(t)
	held := historicalCopy(base, "bucket-full")
	putTerminal(t, q, held)
	bucket := sha256.Sum256([]byte(held.Job.DeliveryID))
	root, closeRoot, err := q.historyBucket(held.Job.DeliveryID, true)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	used := 0
	for n := 0; ; n++ {
		id := fmt.Sprintf("f%010d", n)
		sum := sha256.Sum256([]byte(id))
		if sum[0] != bucket[0] {
			continue
		}
		frame, err := historyFrame(historicalCopy(base, id))
		if err != nil {
			t.Fatal(err)
		}
		if used+len(frame) > historyBucketBytes {
			break
		}
		f, err := openPrivateFile(root, queueName(id+".journal"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(frame)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		used += len(frame)
	}
	if err := q.ReleaseConfirmed(t.Context(), held.Job.DeliveryID); !errors.Is(err, ErrCapacity) {
		t.Fatal("history quota ignored", err)
	}
	if _, err := q.root.Stat(queueName(held.Job.DeliveryID + ".journal")); err != nil {
		t.Fatal("quota failure erased source", err)
	}
	if got, err := q.Inspect(held.Job.DeliveryID); err != nil || !reflect.DeepEqual(got, held) {
		t.Fatal("quota failure lost history", err)
	}
}

func TestTerminalHistoryProcessCrash(t *testing.T) {
	if path := os.Getenv("MEMOH_HISTORY_FIXTURE_ROOT"); path != "" {
		q, err := OpenHerdrQueue(func(string) string { return path }, "fixture", releaseLimits)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = q.Close() }()
		q.historyHook = func(stage string) {
			if stage == os.Getenv("MEMOH_HISTORY_FIXTURE_STAGE") {
				os.Exit(84)
			}
		}
		if err := q.ReleaseConfirmed(t.Context(), "crash-history"); err != nil {
			t.Fatal(err)
		}
		t.Fatal("crash hook not reached")
	}
	for _, stage := range []string{"before_target_sync", "after_target_sync", "after_source_remove", "after_source_sync"} {
		t.Run(stage, func(t *testing.T) {
			q, base, getenv := terminalFixture(t)
			r := historicalCopy(base, "crash-history")
			putTerminal(t, q, r)
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTerminalHistoryProcessCrash$") //nolint:gosec // Current isolated test executable and fixed test action.
			cmd.Env = append(os.Environ(), "MEMOH_HISTORY_FIXTURE_ROOT="+getenv("HERDR_PLUGIN_STATE_DIR"), "MEMOH_HISTORY_FIXTURE_STAGE="+stage)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 84 {
				t.Fatalf("crash child: %v %s", err, out)
			}
			reopened, err := OpenHerdrQueue(getenv, "fixture", releaseLimits)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()
			if err := reopened.ReleaseConfirmed(t.Context(), r.Job.DeliveryID); err != nil {
				t.Fatal(err)
			}
			got, err := reopened.Inspect(r.Job.DeliveryID)
			if err != nil || !reflect.DeepEqual(got, r) {
				t.Fatal("crash lost identity", err)
			}
			if err := reopened.Admit(t.Context(), "upload", queuedHistoryJob(r), nil); err != nil {
				t.Fatal("replay after crash", err)
			}
		})
	}
}
