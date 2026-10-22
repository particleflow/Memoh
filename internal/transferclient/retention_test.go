package transferclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

var releaseLimits = QueueLimits{Jobs: 1, Bytes: 16384, JournalBytes: 65536, Workers: 1}

// The NAS here is a TLS protocol fixture and the association is a synthetic
// durable acknowledgement. Storage, snapshots, locks and process exit are real.
func releaseFixture(t *testing.T, limits QueueLimits) (*DiskQueue, Job, func(string) string, *fixture, *Client) {
	t.Helper()
	getenv, closed, _ := producerFixture(t, "https://fixture.invalid")
	q, err := OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	source, err := cfg.OpenSource(closed.Artifact.SourceRef)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	job := newJob([]byte("fixture"))
	job.SnapshotID, job.LocatorRevision = job.DeliveryID, "fixture"
	if err := q.Admit(t.Context(), "upload", job, source); err != nil {
		t.Fatal(err)
	}
	f, client := newFixture(t)
	for range 12 {
		r, err := q.Inspect(job.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		if r.Job.Phase == "ready" {
			return q, job, getenv, f, client
		}
		if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("ready not reached")
	return nil, Job{}, nil, nil, nil
}

func releaseBinding() *AssociationBinding {
	return &AssociationBinding{
		Commit: func(context.Context, string, Reference, io.Reader) error { return nil },
		Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
			return OutcomeCommitted, nil
		},
	}
}

func assertReleased(t *testing.T, q *DiskQueue, job Job) {
	t.Helper()
	r, err := q.Inspect(job.DeliveryID)
	if err != nil || !confirmedDelivery(r) || r.PayloadState != PayloadReleased || r.Job.ReceiptID != "receipt-1" {
		t.Fatal("receipt lost", r.PayloadState, err)
	}
	for _, suffix := range []string{".snapshot", ".tree", ".admission", ".part", ".release"} {
		if _, err := q.root.Lstat(queueName(job.DeliveryID + suffix)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("cache retained", suffix, err)
		}
	}
	st, err := q.root.Stat(queueName(job.DeliveryID + ".journal"))
	if err != nil || st.Size() > releaseReceiptBytes {
		t.Fatal("unbounded receipt", err)
	}
	if err := q.Admit(t.Context(), "upload", job, nil); err != nil {
		t.Fatal("same ID cannot resume without source", err)
	}
	changed := job
	changed.Spec.SHA256 = digest([]byte("changed"))
	if err := q.Admit(t.Context(), "upload", changed, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("retained ID accepted changed content", err)
	}
}

func TestQueueReleaseConfirmedFreesJobAndByteCapacity(t *testing.T) {
	for _, jobs := range []int{1, 2} {
		t.Run(strconv.Itoa(jobs), func(t *testing.T) {
			limits := releaseLimits
			limits.Jobs = jobs // Two jobs exhaust bytes rather than job count.
			q, job, getenv, f, client := releaseFixture(t, limits)
			next := job
			next.DeliveryID, next.SnapshotID = "next", "next"
			if err := q.Admit(t.Context(), "upload", next, nil); !errors.Is(err, ErrCapacity) {
				t.Fatal("capacity not reserved", err)
			}
			r, err := q.invokeHerdr(t.Context(), HerdrAction{Kind: "advance", DeliveryID: job.DeliveryID}, nil, client, releaseBinding(), time.Now())
			if err != nil || r.PayloadState != PayloadReleased {
				t.Fatal("finite action did not release", err)
			}
			assertReleased(t, q, job)
			// Read the user-owned source after release and use it to admit new work.
			cfg, err := OpenHostConfig(getenv)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cfg.Close() }()
			source, err := cfg.OpenSource("result-one")
			if err != nil {
				t.Fatal("user source deleted", err)
			}
			defer func() { _ = source.Close() }()
			data, err := io.ReadAll(source)
			if err != nil || string(data) != "fixture" {
				t.Fatal("user source changed", err)
			}
			if _, err = source.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			if err := q.Admit(t.Context(), "upload", next, source); err != nil {
				t.Fatal("capacity not released", err)
			}
			if err := q.RunOne(t.Context(), job.DeliveryID, client, releaseBinding(), time.Now()); err != nil {
				t.Fatal(err)
			}
			if f.count("POST /bots/bot-1/media/uploads") != 1 {
				t.Fatal("duplicate receipt")
			}
			if err := q.Retire(job.DeliveryID); err != nil {
				t.Fatal(err)
			}
			assertReleased(t, q, job) // Explicit retirement cannot erase the proof either.
		})
	}
}

func TestQueueReleaseNeverTreatsUnknownOrCancelAsDelivered(t *testing.T) {
	q, job, _, _, client := releaseFixture(t, releaseLimits)
	binding := releaseBinding()
	binding.Commit = func(context.Context, string, Reference, io.Reader) error { return context.DeadlineExceeded }
	if err := q.RunOne(t.Context(), job.DeliveryID, client, binding, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := q.RequestCancel(job.DeliveryID); err != nil {
		t.Fatal(err)
	}
	if err := q.ReleaseConfirmed(t.Context(), job.DeliveryID); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown released", err)
	}
	if err := q.ReclaimConfirmed(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := q.root.Stat(queueName(job.DeliveryID + ".snapshot")); err != nil {
		t.Fatal("unknown bytes deleted", err)
	}
	if err := q.capacity(7); !errors.Is(err, ErrCapacity) {
		t.Fatal("unknown capacity released", err)
	}
	// NAS commit won before cancellation. Resolving the original ID may release.
	r, err := q.invokeHerdr(t.Context(), HerdrAction{Kind: "advance", DeliveryID: job.DeliveryID}, nil, client, binding, time.Now().Add(time.Hour))
	if err != nil || !r.Job.CancelRequested || !r.Associated {
		t.Fatal("commit winner lost", err)
	}
	assertReleased(t, q, job)
}

func TestQueueReleaseProcessCrash(t *testing.T) {
	if path := os.Getenv("MEMOH_RELEASE_FIXTURE_ROOT"); path != "" {
		q, err := OpenHerdrQueue(func(key string) string {
			if key == "HERDR_PLUGIN_STATE_DIR" {
				return path
			}
			return ""
		}, "fixture", releaseLimits)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = q.Close() }()
		q.releaseHook = func(stage string) {
			if stage == os.Getenv("MEMOH_RELEASE_FIXTURE_STAGE") {
				os.Exit(83)
			}
		}
		if err := q.ReleaseConfirmed(t.Context(), "delivery-1"); err != nil {
			t.Fatal(err)
		}
		t.Fatal("crash hook not reached")
	}
	for _, stage := range []string{"after_intent", "after_payload", "after_receipt", "after_truncate", "after_compact", "after_finish"} {
		t.Run(stage, func(t *testing.T) {
			q, job, getenv, _, client := releaseFixture(t, releaseLimits)
			if err := q.RunOne(t.Context(), job.DeliveryID, client, releaseBinding(), time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := q.Close(); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestQueueReleaseProcessCrash$") //nolint:gosec // Current test executable, fixed test name; only isolated fixture paths in environment.
			cmd.Env = append(os.Environ(), "MEMOH_RELEASE_FIXTURE_ROOT="+getenv("HERDR_PLUGIN_STATE_DIR"), "MEMOH_RELEASE_FIXTURE_STAGE="+stage)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 83 {
				t.Fatalf("crash child: %v %s", err, out)
			}
			reopened, err := OpenHerdrQueue(getenv, "fixture", releaseLimits)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()
			if err := reopened.ReclaimConfirmed(t.Context()); err != nil {
				t.Fatal("restart", err)
			}
			assertReleased(t, reopened, job)
			if err := reopened.capacity(7); err != nil {
				t.Fatal("capacity leaked", err)
			}
		})
	}
}

func TestQueueReleaseReceiptCatalogBound(t *testing.T) {
	q, job, _, _, client := releaseFixture(t, releaseLimits)
	if _, err := q.invokeHerdr(t.Context(), HerdrAction{Kind: "advance", DeliveryID: job.DeliveryID}, nil, client, releaseBinding(), time.Now()); err != nil {
		t.Fatal(err)
	}
	r, err := q.Inspect(job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	// Populate bounded completed metadata only; payload storage is never allocated.
	for n := 1; n < maxQueueRecords; n++ {
		saved := r
		saved.Job.DeliveryID, saved.Job.SnapshotID = fmt.Sprintf("retained-%04d", n), fmt.Sprintf("retained-%04d", n)
		ref := *r.Job.Ready
		ref.DeliveryID = saved.Job.DeliveryID
		saved.Job.Ready = &ref
		f, err := q.file(saved.Job.DeliveryID+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		err = q.saveReceipt(f, saved)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := q.capacity(7); !errors.Is(err, ErrCapacity) {
		t.Fatal("receipt catalog unbounded", err)
	}
	assertReleased(t, q, job)
}

func TestQueueReleaseCanceledReservationAndLegacyCommit(t *testing.T) {
	for _, outcome := range []string{"cancel", "legacy-commit"} {
		t.Run(outcome, func(t *testing.T) {
			q, job, _, _, client := releaseFixture(t, releaseLimits)
			if err := q.ReleaseConfirmed(t.Context(), job.DeliveryID); !errors.Is(err, ErrInvalid) {
				t.Fatal("ready is not delivered", err)
			}
			if outcome == "cancel" {
				if err := q.RequestCancel(job.DeliveryID); err != nil {
					t.Fatal(err)
				}
			}
			if err := q.RunOne(t.Context(), job.DeliveryID, client, releaseBinding(), time.Now()); err != nil {
				t.Fatal(err)
			}
			if outcome == "legacy-commit" {
				r, f, err := q.load(job.DeliveryID)
				if err != nil {
					t.Fatal(err)
				}
				r.Version, r.PayloadState = 2, ""
				err = q.save(f, r)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := q.ReclaimConfirmed(t.Context()); err != nil {
				t.Fatal(err)
			}
			if outcome == "legacy-commit" {
				assertReleased(t, q, job)
				return
			}
			if err := q.capacity(7); !errors.Is(err, ErrCapacity) {
				t.Fatal("cancel auto-cleared", err)
			}
			r, err := q.Inspect(job.DeliveryID)
			if err != nil || r.Job.Phase != "canceled" || r.PayloadState != "" {
				t.Fatal("cancel changed", err)
			}
		})
	}
}
