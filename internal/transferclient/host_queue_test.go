//go:build linux || darwin

package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func openQueue(t *testing.T, path string, limits QueueLimits) *DiskQueue {
	t.Helper()
	q, err := OpenHerdrQueue(func(key string) string {
		if key != "HERDR_PLUGIN_STATE_DIR" {
			t.Fatal("unexpected locator")
		}
		return path
	}, "revision-1", limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

func queueLimits() QueueLimits {
	return QueueLimits{Jobs: 8, Bytes: 32 << 20, JournalBytes: 1 << 20, Workers: 1}
}

func admitSource(t *testing.T, q *DiskQueue, data []byte) (Job, *os.File) {
	t.Helper()
	j := newJob(data)
	j.SnapshotID = j.DeliveryID
	f, err := os.CreateTemp(t.TempDir(), "closed-output")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := q.Admit(t.Context(), "upload", j, f); err != nil {
		t.Fatal(err)
	}
	return j, f
}

func inspectQueue(t *testing.T, q *DiskQueue, id string) QueueStatus {
	t.Helper()
	r, err := q.Inspect(id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func runQueue(t *testing.T, q *DiskQueue, c *Client, associate *AssociationBinding) QueueStatus {
	t.Helper()
	r := inspectQueue(t, q, "delivery-1")
	now := time.Now()
	if r.Job.NextAttempt.After(now) {
		now = r.Job.NextAttempt
	}
	if r.AssociationDue.After(now) {
		now = r.AssociationDue
	}
	if err := q.RunOne(t.Context(), "delivery-1", c, associate, now); err != nil {
		t.Fatal(err)
	}
	return inspectQueue(t, q, "delivery-1")
}

func TestDiskQueueRestartSnapshotAndOutbox(t *testing.T) {
	data := []byte{0, 0xff, 1, 2, 3, 4, 0x80, 6, 7}
	path := t.TempDir()
	q := openQueue(t, path, queueLimits())
	_, source := admitSource(t, q, data)
	if _, err := source.WriteAt([]byte("changed!!"), 0); err != nil {
		t.Fatal(err)
	}
	f, c := newFixture(t)
	runQueue(t, q, c, nil)
	f.setDrop("PATCH /bots/bot-1/media/uploads/receipt-1")
	r := runQueue(t, q, c, nil)
	if r.Job.Offset != 0 || r.Job.NextAttempt.IsZero() {
		t.Fatal("lost patch checkpoint")
	}
	_ = q.Close()
	q = openQueue(t, path, queueLimits())
	for n := 0; n < 8 && inspectQueue(t, q, "delivery-1").Job.Phase != "ready"; n++ {
		runQueue(t, q, c, nil)
	}
	if !bytes.Equal(fixtureReady(f), data) {
		t.Fatal("producer mutation changed frozen bytes")
	}
	if err := q.RunOne(t.Context(), "delivery-1", c, nil, time.Now()); !errors.Is(err, ErrBinding) {
		t.Fatal("missing registrar did not fail", err)
	}
	calls, associated := 0, map[string]Reference{}
	register := func(_ context.Context, direction string, ref Reference, _ io.Reader) error {
		calls++
		if direction != "upload" || ref.ContentHash != digest(data) {
			t.Fatal("wrong reference")
		}
		associated[ref.DeliveryID] = ref // Fixture's atomic authorization+idempotent association.
		if calls == 1 {
			return ErrTransport
		} // Lost registration response.
		return nil
	}
	binding := &AssociationBinding{Commit: register, Resolve: func(_ context.Context, _ string, ref Reference, _ bool) (AssociationOutcome, error) {
		calls++
		if associated[ref.DeliveryID] != ref {
			return OutcomeAbsent, nil
		}
		return OutcomeCommitted, nil
	}}
	r = runQueue(t, q, c, binding)
	if r.Associated || r.AssociationDue.IsZero() {
		t.Fatal("outbox acknowledged lost response")
	}
	_ = q.Close()
	q = openQueue(t, path, queueLimits())
	r = runQueue(t, q, c, binding)
	if !r.Associated || calls != 2 || len(associated) != 1 {
		t.Fatal("outbox replay not idempotent")
	}
	if err := q.RequestCancel("delivery-1"); !errors.Is(err, ErrInvalid) {
		t.Fatal("associated reference canceled")
	}
	if err := q.Retire("delivery-1"); err != nil {
		t.Fatal("acknowledged cleanup", err)
	}
	if _, err := q.root.Lstat("delivery-1.snapshot"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("snapshot not released")
	}
	if len(associated) != 1 {
		t.Fatal("local cleanup changed registered result")
	}
}

func TestDiskQueueAdmissionAndJournalFailures(t *testing.T) {
	for _, mode := range []string{"hash", "quota", "torn", "corrupt", "symlink", "hardlink", "locator", "revision", "journal-full"} {
		t.Run(mode, func(t *testing.T) {
			path := t.TempDir()
			limits := queueLimits()
			if mode == "quota" {
				limits.Bytes = 3
			}
			q := openQueue(t, path, limits)
			data := []byte("synthetic bytes")
			if mode == "hash" || mode == "quota" {
				j := newJob(data)
				j.SnapshotID = j.DeliveryID
				f, err := os.CreateTemp(t.TempDir(), "producer")
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = f.Close() }()
				_, _ = f.Write(data)
				if mode == "hash" {
					j.Spec.SHA256 = digest([]byte("other"))
				}
				err = q.Admit(t.Context(), "upload", j, f)
				want := ErrSnapshot
				if mode == "quota" {
					want = ErrCapacity
				}
				if !errors.Is(err, want) {
					t.Fatal("admission accepted", err)
				}
				return
			}
			admitSource(t, q, data)
			journalPath := filepath.Join(path, "deliveries-v1", "delivery-1.journal")
			snapshotPath := filepath.Join(path, "deliveries-v1", "delivery-1.snapshot")
			switch mode {
			case "torn":
				before, _ := os.Stat(journalPath)
				f, _ := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // Fixed basename under test-owned TempDir.
				_, _ = f.Write([]byte{1, 2, 3})
				_ = f.Close()
				_ = q.Close()
				q = openQueue(t, path, limits)
				inspectQueue(t, q, "delivery-1")
				after, _ := os.Stat(journalPath)
				if before.Size() != after.Size() {
					t.Fatal("torn tail not repaired")
				}
				return
			case "corrupt":
				f, _ := os.OpenFile(journalPath, os.O_WRONLY, 0o600) //nolint:gosec // Fixed basename under test-owned TempDir.
				_, _ = f.WriteAt([]byte{0}, 40)
				_ = f.Close()
				if _, err := q.Inspect("delivery-1"); !errors.Is(err, ErrStore) {
					t.Fatal("corrupt full frame accepted", err)
				}
				return
			case "locator":
				_ = q.Close()
				moved := filepath.Join(t.TempDir(), "moved")
				if err := os.Rename(path, moved); err != nil {
					t.Fatal(err)
				}
				if other, err := OpenHerdrQueue(func(string) string { return moved }, "revision-1", limits); err == nil {
					_ = other.Close()
					t.Fatal("silent relocation")
				}
				return
			case "revision":
				if other, err := OpenHerdrQueue(func(string) string { return path }, "revision-2", limits); err == nil {
					_ = other.Close()
					t.Fatal("silent revision change")
				}
				return
			case "symlink":
				_ = os.Remove(snapshotPath)
				if err := os.Symlink(journalPath, snapshotPath); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(snapshotPath, filepath.Join(t.TempDir(), "link")); err != nil {
					t.Fatal(err)
				}
			case "journal-full":
				r, f, err := q.load("delivery-1")
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = f.Close() }()
				for i := 0; i < 3000; i++ {
					err = q.save(f, r)
					if err != nil {
						break
					}
				}
				stat, statErr := f.Stat()
				if err != nil || statErr != nil || stat.Size() > limits.JournalBytes {
					t.Fatal("journal did not compact within its bound", err, statErr)
				}
				return
			}
			_, c := newFixture(t)
			if err := q.RunOne(t.Context(), "delivery-1", c, nil, time.Now()); !errors.Is(err, ErrStore) {
				t.Fatal("unsafe snapshot accepted", err)
			}
		})
	}
}

func TestDiskQueueCancelUnknownAndRevokedAssociation(t *testing.T) {
	for _, mode := range []string{"cancel", "unknown", "revoke", "budget", "snapshot-change"} {
		t.Run(mode, func(t *testing.T) {
			q := openQueue(t, t.TempDir(), queueLimits())
			admitSource(t, q, []byte("example"))
			f, c := newFixture(t)
			if mode == "cancel" {
				if err := q.RequestCancel("delivery-1"); err != nil {
					t.Fatal(err)
				}
				r := runQueue(t, q, c, nil)
				if r.Job.Phase != "canceled" || f.count("POST /bots/bot-1/media/uploads") != 0 {
					t.Fatal("queued cancel uploaded")
				}
				return
			}
			if mode == "snapshot-change" {
				if err := q.root.Chmod("delivery-1.snapshot", 0o600); err != nil {
					t.Fatal(err)
				}
				file, err := q.file("delivery-1.snapshot", os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = file.WriteAt([]byte("!"), 0)
				_ = file.Close()
				r := runQueue(t, q, c, nil)
				if r.Job.Phase != "blocked" || f.count("POST /bots/bot-1/media/uploads") != 0 {
					t.Fatal("corrupt snapshot reached network")
				}
				return
			}
			if mode == "unknown" {
				f.setDrop("POST /bots/bot-1/media/uploads")
				runQueue(t, q, c, nil)
				r := runQueue(t, q, c, nil)
				if r.Job.Phase != "create_unknown" || f.count("POST /bots/bot-1/media/uploads") != 1 {
					t.Fatal("unknown create repeated")
				}
				return
			}
			for n := 0; n < 8 && inspectQueue(t, q, "delivery-1").Job.Phase != "ready"; n++ {
				runQueue(t, q, c, nil)
			}
			calls := 0
			register := func(context.Context, string, Reference, io.Reader) error {
				calls++
				if mode == "budget" {
					return ErrTransport
				}
				return ErrAuthorization
			}
			binding := &AssociationBinding{Commit: register, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
				calls++
				return "", ErrTransport
			}}
			for range 14 {
				runQueue(t, q, c, binding)
			}
			r := inspectQueue(t, q, "delivery-1")
			want := 1
			if mode == "budget" {
				want = 12
			}
			if r.Associated || !r.AssociationBlocked || calls != want {
				t.Fatal("outbox authorization or retry bound", r, calls)
			}
		})
	}
}

func TestBlockedDownloadCanCancelWithoutNetwork(t *testing.T) {
	q := openQueue(t, t.TempDir(), queueLimits())
	j := newJob([]byte("synthetic input"))
	j.SnapshotID = j.DeliveryID
	if err := q.Admit(t.Context(), "download", j, nil); err != nil {
		t.Fatal(err)
	}
	f, c := newFixture(t)
	f.setStatus(http.StatusForbidden)
	r := runQueue(t, q, c, nil)
	if r.Job.Phase != "blocked" {
		t.Fatal("revoked download not blocked")
	}
	if err := q.RequestCancel(j.DeliveryID); err != nil {
		t.Fatal(err)
	}
	r = runQueue(t, q, c, nil)
	if r.Job.Phase != "canceled" {
		t.Fatal("local cancellation ignored")
	}
	if err := q.Retire(j.DeliveryID); err != nil {
		t.Fatal(err)
	}
}

func TestDiskQueueDownloadRangeHashAndReauthorization(t *testing.T) {
	for _, mode := range []string{"resume", "revoke", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			data := []byte("synthetic NAS to PC bytes")
			revoked := false
			ranged := false
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != syntheticAuth || revoked {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if r.Header.Get("Range") == "bytes=3-" {
					ranged = true
				}
				w.Header().Set("ETag", `"`+digest(data)+`"`)
				http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
			}))
			defer srv.Close()
			c, err := New(srv.URL, srv.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
			if err != nil {
				t.Fatal(err)
			}
			q := openQueue(t, t.TempDir(), queueLimits())
			j := newJob(data)
			j.SnapshotID = j.DeliveryID
			if err := q.Admit(t.Context(), "download", j, nil); err != nil {
				t.Fatal(err)
			}
			r, journal, err := q.load(j.DeliveryID)
			if err != nil {
				t.Fatal(err)
			}
			file, err := q.file(j.DeliveryID+".part", os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = file.Write(data[:3])
			_ = file.Sync()
			r.Job.Offset = 3
			if err := q.save(journal, r); err != nil {
				t.Fatal(err)
			}
			_ = journal.Close()
			// An unacknowledged tail must be truncated before Range resume.
			_, _ = file.Write([]byte("unacknowledged"))
			_ = file.Close()
			r = runQueue(t, q, c, nil)
			if r.Job.Phase != "ready" || r.Associated || !ranged {
				t.Fatal("download did not resume privately")
			}
			if mode == "revoke" {
				revoked = true
			}
			if mode == "corrupt" {
				file, _ = q.file(j.DeliveryID+".part", os.O_WRONLY, 0o600)
				_, _ = file.WriteAt([]byte("!"), 0)
				_ = file.Close()
			}
			called := false
			err = q.RunOne(t.Context(), j.DeliveryID, c, commitOnlyFixture(t, func(context.Context, string, Reference, io.Reader) error { called = true; return nil }), time.Now())
			if mode == "resume" {
				if err != nil || !called || !inspectQueue(t, q, j.DeliveryID).Associated {
					t.Fatal("verified download association", err)
				}
			} else if err != nil || called || !inspectQueue(t, q, j.DeliveryID).AssociationBlocked {
				t.Fatal("invalid download reached association", err)
			}
		})
	}
}

func TestDiskQueueProcessCrashReleasesLock(t *testing.T) {
	if path := os.Getenv("MEMOH_QUEUE_CRASH_FIXTURE"); path != "" {
		q := openQueue(t, path, queueLimits())
		lock, err := q.lock("delivery-1")
		if err != nil {
			t.Fatal(err)
		}
		_ = lock
		f, err := q.file("delivery-1.journal", os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte{1, 2, 3})
		_ = f.Sync()
		// Intentionally omit unlock/Close: abrupt process exit, not a mocked lock.
		os.Exit(23)
	}
	path := t.TempDir()
	q := openQueue(t, path, queueLimits())
	admitSource(t, q, []byte("example"))
	lock, err := q.lock("delivery-1")
	if err != nil {
		t.Fatal(err)
	}
	other := openQueue(t, path, queueLimits())
	_, c := newFixture(t)
	if err := other.RunOne(t.Context(), "delivery-1", c, nil, time.Now()); !errors.Is(err, ErrBusy) {
		t.Fatal("parallel job lock", err)
	}
	unlockClose(lock)
	worker, err := q.worker()
	if err != nil {
		t.Fatal(err)
	}
	if err := other.RunOne(t.Context(), "delivery-1", c, nil, time.Now()); !errors.Is(err, ErrBusy) {
		t.Fatal("global worker limit", err)
	}
	unlockClose(worker)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDiskQueueProcessCrashReleasesLock$") //nolint:gosec // Execute this test binary only, with a fixed subtest selector.
	cmd.Env = append(os.Environ(), "MEMOH_QUEUE_CRASH_FIXTURE="+path)
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatal("crash fixture failed", err)
	}
	if _, err := q.Inspect("delivery-1"); err != nil {
		t.Fatal("OS lock not released or torn tail not recovered", err)
	}
}

// Measures the adapter's full snapshot reread cost on the local test filesystem.
// Warm page-cache/CPU results are not physical disk or network throughput.
func BenchmarkDiskSnapshotReopen(b *testing.B) {
	for _, size := range []int64{1 << 20, 512 << 20} {
		b.Run(strconv.FormatInt(size>>20, 10)+"MiB", func(b *testing.B) {
			f, err := os.CreateTemp(b.TempDir(), "snapshot")
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			h := sha256.New()
			if _, err := io.CopyBuffer(io.MultiWriter(f, h), io.LimitReader(zeroReader{}, size), make([]byte, copyBytes)); err != nil {
				b.Fatal(err)
			}
			if err := f.Sync(); err != nil {
				b.Fatal(err)
			}
			spec := Spec{SizeBytes: size, SHA256: hex.EncodeToString(h.Sum(nil)), Mime: "application/octet-stream"}
			b.SetBytes(size)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := f.Seek(0, io.SeekStart); err != nil {
					b.Fatal(err)
				}
				if err := Verify(b.Context(), f, spec); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func commitOnlyFixture(t *testing.T, commit Associate) *AssociationBinding {
	t.Helper()
	return &AssociationBinding{Commit: commit, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		t.Fatal("unexpected resolve in commit-only fixture")
		return "", ErrProtocol
	}}
}
