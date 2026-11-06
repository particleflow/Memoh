//go:build linux || darwin

package transferclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestAdmissionDeadlineCanResumeSameID(t *testing.T) {
	q := openQueue(t, t.TempDir(), queueLimits())
	data := []byte("deadline admission fixture")
	j := newJob(data)
	j.SnapshotID = j.DeliveryID
	f, err := os.CreateTemp(t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := q.Admit(ctx, "upload", j, f); !errors.Is(err, context.Canceled) {
		t.Fatalf("initial interruption: %v", err)
	}
	if err := q.Admit(t.Context(), "upload", j, f); err != nil {
		t.Fatalf("same-ID recovery failed: %v", err)
	}
	if !inspectQueue(t, q, j.DeliveryID).Admitted {
		t.Fatal("not admitted")
	}
}

func TestPartialAdmissionCancelReleasesCapacity(t *testing.T) {
	limits := queueLimits()
	limits.Jobs = 1
	q := openQueue(t, t.TempDir(), limits)
	j := newJob([]byte("quota fixture"))
	j.SnapshotID = j.DeliveryID
	f, err := os.CreateTemp(t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write([]byte("quota fixture")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_ = q.Admit(ctx, "upload", j, f)
	if err := q.RequestCancel(j.DeliveryID); err != nil {
		t.Fatal(err)
	}
	if err := q.Retire(j.DeliveryID); err != nil {
		t.Fatalf("partial reservation cannot retire: %v", err)
	}
	j.DeliveryID, j.SnapshotID = "delivery-2", "delivery-2"
	if err := q.Admit(t.Context(), "upload", j, f); err != nil {
		t.Fatalf("quota not reclaimed: %v", err)
	}
}

// Deterministically reach real cancellation branches without slow sleeps. The
// process-exit variant dies inside admission, after its first durable checkpoint.
type admissionStopContext struct {
	context.Context
	calls, stop int
	crash       bool
}

func (c *admissionStopContext) Err() error {
	c.calls++
	if c.calls >= c.stop {
		if c.crash {
			os.Exit(29)
		}
		return context.DeadlineExceeded
	}
	return nil
}

func admissionSource(t *testing.T, size int64) (Job, *os.File) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(zeroReader{}, size)); err != nil {
		t.Fatal(err)
	}
	j := newJob([]byte("x"))
	j.SnapshotID = j.DeliveryID
	j.Spec.SizeBytes, j.Spec.SHA256 = size, hex.EncodeToString(h.Sum(nil))
	return j, f
}

func admissionState(t *testing.T, q *DiskQueue, job Job) admissionProgress {
	t.Helper()
	f, err := q.file(job.DeliveryID+".admission", os.O_RDONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	p, err := readAdmission(f, job)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func verifyAdmission(t *testing.T, q *DiskQueue, j Job) {
	t.Helper()
	r := inspectQueue(t, q, j.DeliveryID)
	source, closeSource, err := q.openSnapshot(r)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSource()
	h := sha256.New()
	buf := make([]byte, MaxChunkBytes)
	for offset := int64(0); offset < j.Spec.SizeBytes; {
		n := min(int64(len(buf)), j.Spec.SizeBytes-offset)
		if err := source.ReadAt(t.Context(), j.SnapshotID, j.LocatorRevision, j.Spec, buf[:n], offset); err != nil {
			t.Fatal(err)
		}
		_, _ = h.Write(buf[:n])
		offset += n
	}
	if hex.EncodeToString(h.Sum(nil)) != j.Spec.SHA256 {
		t.Fatal("resumed snapshot differs")
	}
}

func TestAdmissionBoundedLargeFileAndIdempotency(t *testing.T) {
	limits := queueLimits()
	limits.Bytes = 300 << 20
	limits.JournalBytes = 65536
	path := t.TempDir()
	q := openQueue(t, path, limits)
	j, source := admissionSource(t, 130<<20+17)
	for attempt, wantOffset := range []int64{64 << 20, 128 << 20, j.Spec.SizeBytes} {
		err := q.Admit(t.Context(), "upload", j, source)
		if attempt < 2 && !errors.Is(err, ErrAdmissionPending) || attempt == 2 && err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		p := admissionState(t, q, j)
		if p.Offset != wantOffset {
			t.Fatalf("copied %d want %d", p.Offset, wantOffset)
		}
		info, err := q.root.Stat(j.DeliveryID + ".admission")
		if err != nil || info.Size() != admissionFileBytes {
			t.Fatal("unbounded progress", err)
		}
		_ = q.Close()
		q = openQueue(t, path, limits)
	}
	verifyAdmission(t, q, j)
	before, _ := q.root.Stat(j.DeliveryID + ".journal")
	if err := q.Admit(t.Context(), "upload", j, nil); err != nil {
		t.Fatal("lost acknowledgement replay", err)
	}
	after, _ := q.root.Stat(j.DeliveryID + ".journal")
	if before.Size() != after.Size() {
		t.Fatal("admitted replay appended journal")
	}
	conflict := j
	conflict.OperationID = "other-operation"
	if err := q.Admit(t.Context(), "upload", conflict, source); !errors.Is(err, ErrInvalid) {
		t.Fatal("ID rebound", err)
	}
}

func TestAdmissionDeadlineProgressAndRetryStorageBound(t *testing.T) {
	q := openQueue(t, t.TempDir(), queueLimits())
	j, source := admissionSource(t, 21<<20+17)
	ctx := &admissionStopContext{Context: t.Context(), stop: 12}
	if err := q.Admit(ctx, "upload", j, source); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	p := admissionState(t, q, j)
	if p.Offset != 11<<20 {
		t.Fatalf("deadline discarded durable progress: %d", p.Offset)
	}
	before, _ := q.root.Stat(j.DeliveryID + ".journal")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for i := 0; i < 100; i++ {
		if err := q.Admit(canceled, "upload", j, source); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	after, _ := q.root.Stat(j.DeliveryID + ".journal")
	if after.Size() != before.Size() {
		t.Fatal("retry consumed journal quota")
	}
	// Resuming does not reread/rewrite the completed source prefix. The produced
	// snapshot must still match the immutable hash declared before mutation.
	if _, err := source.WriteAt([]byte("changed later"), 0); err != nil {
		t.Fatal(err)
	}
	if err := q.Admit(t.Context(), "upload", j, source); err != nil {
		t.Fatal(err)
	}
	verifyAdmission(t, q, j)
}

func TestAdmissionCrashAfterCheckpoint(t *testing.T) {
	if path := os.Getenv("MEMOH_ADMISSION_CRASH_FIXTURE"); path != "" {
		q := openQueue(t, path, queueLimits())
		j, source := admissionSource(t, 21<<20+17)
		_ = q.Admit(&admissionStopContext{Context: t.Context(), stop: 10, crash: true}, "upload", j, source)
		t.Fatal("process did not exit")
	}
	path := t.TempDir()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestAdmissionCrashAfterCheckpoint$") //nolint:gosec // Current test executable, fixed test name.
	cmd.Env = append(os.Environ(), "MEMOH_ADMISSION_CRASH_FIXTURE="+path)
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 29 {
		t.Fatalf("unexpected helper result %v", err)
	}
	q := openQueue(t, path, queueLimits())
	j, source := admissionSource(t, 21<<20+17)
	p := admissionState(t, q, j)
	data, _ := q.root.Stat(j.DeliveryID + ".snapshot")
	if p.Offset != 8<<20 || data.Size() != 9<<20 {
		t.Fatalf("missing unacknowledged tail: checkpoint %d file %d", p.Offset, data.Size())
	}
	if err := q.Admit(t.Context(), "upload", j, source); err != nil {
		t.Fatal(err)
	}
	verifyAdmission(t, q, j)
}

func TestAdmissionTornSlotAndChangedSource(t *testing.T) {
	for _, mode := range []string{"torn-latest", "changed-tail", "tree-tamper", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			q := openQueue(t, t.TempDir(), queueLimits())
			j, source := admissionSource(t, 21<<20+17)
			if err := q.Admit(&admissionStopContext{Context: t.Context(), stop: 12}, "upload", j, source); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			p := admissionState(t, q, j)
			switch mode {
			case "torn-latest":
				f, err := q.file(j.DeliveryID+".admission", os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteAt([]byte{0xff}, int64(p.Sequence%2)*admissionSlotBytes+40)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
				if old := admissionState(t, q, j); old.Offset != 8<<20 {
					t.Fatalf("did not recover preceding slot: %d", old.Offset)
				}
			case "changed-tail":
				_, err := source.WriteAt([]byte("changed tail"), 15<<20)
				if err != nil {
					t.Fatal(err)
				}
			case "tree-tamper":
				f, err := q.file(j.DeliveryID+".tree", os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteAt(make([]byte, 32), 0)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "cancel":
				if err := q.RequestCancel(j.DeliveryID); err != nil {
					t.Fatal(err)
				}
				if err := q.Admit(t.Context(), "upload", j, source); !errors.Is(err, ErrInvalid) {
					t.Fatal("canceled admission resumed", err)
				}
				if err := q.Retire(j.DeliveryID); err != nil {
					t.Fatal(err)
				}
				if _, err := q.root.Stat(j.DeliveryID + ".admission"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("progress not retired")
				}
				return
			}
			err := q.Admit(t.Context(), "upload", j, source)
			if mode == "torn-latest" {
				if err != nil {
					t.Fatal(err)
				}
				verifyAdmission(t, q, j)
			} else {
				if !errors.Is(err, ErrSnapshot) {
					t.Fatal("tampered source/tree admitted", err)
				}
				if err := q.RequestCancel(j.DeliveryID); err != nil {
					t.Fatal(err)
				}
				if err := q.Retire(j.DeliveryID); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAdmissionTreeAndReadonlyFinalizationResume(t *testing.T) {
	for _, stop := range []int{9, 15} {
		t.Run(strconv.Itoa(stop), func(t *testing.T) {
			q := openQueue(t, t.TempDir(), queueLimits())
			j, source := admissionSource(t, 7<<20+17)
			if err := q.Admit(&admissionStopContext{Context: t.Context(), stop: stop}, "upload", j, source); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			p := admissionState(t, q, j)
			if stop == 9 && p.Stage != "tree" || stop == 15 && p.Stage != "tree" {
				t.Fatal("did not reach tree", p.Stage)
			}
			if err := q.Admit(t.Context(), "upload", j, source); err != nil {
				t.Fatal(err)
			}
			verifyAdmission(t, q, j)
		})
	}
	// Crash boundary after sealed checkpoint + readonly attributes, before final
	// journal append. Restore precisely that durable journal prefix.
	q := openQueue(t, t.TempDir(), queueLimits())
	j, source := admissionSource(t, 3<<20+17)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_ = q.Admit(canceled, "upload", j, source)
	initial, _ := q.root.Stat(j.DeliveryID + ".journal")
	if err := q.Admit(t.Context(), "upload", j, source); err != nil {
		t.Fatal(err)
	}
	f, err := q.file(j.DeliveryID+".journal", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(initial.Size()); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := q.Admit(t.Context(), "upload", j, nil); err != nil {
		t.Fatal("readonly finalize restart", err)
	}
	verifyAdmission(t, q, j)
}

func TestHerdrEnqueueDeadlineReportsResumableReservation(t *testing.T) {
	path := t.TempDir()
	j, source := admissionSource(t, 21<<20+17)
	// Expired real deadline, not a transport-class surrogate.
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	status, err := InvokeHerdr(ctx, func(string) string { return path }, "revision-1", queueLimits(), HerdrAction{Kind: "enqueue-upload", DeliveryID: j.DeliveryID, Job: j}, source, nil, nil, time.Now())
	if !errors.Is(err, context.DeadlineExceeded) || status.Admitted || status.Job.DeliveryID != j.DeliveryID {
		t.Fatal("reservation missing", status, err)
	}
	status, err = InvokeHerdr(t.Context(), func(string) string { return path }, "revision-1", queueLimits(), HerdrAction{Kind: "enqueue-upload", DeliveryID: j.DeliveryID, Job: j}, source, nil, nil, time.Now())
	if err != nil || !status.Admitted {
		t.Fatal("finite action did not resume", err)
	}
}

func TestAdmissionCorruptProgressAndLegacyReservationFailExplicitly(t *testing.T) {
	for _, mode := range []string{"both-slots-corrupt", "legacy", "identity"} {
		t.Run(mode, func(t *testing.T) {
			q := openQueue(t, t.TempDir(), queueLimits())
			j, source := admissionSource(t, 3<<20)
			canceled, cancel := context.WithCancel(t.Context())
			cancel()
			_ = q.Admit(canceled, "upload", j, source)
			want := ErrStore
			switch mode {
			case "both-slots-corrupt":
				f, err := q.file(j.DeliveryID+".admission", os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteAt(make([]byte, admissionFileBytes), 0)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "legacy":
				r, f, err := q.load(j.DeliveryID)
				if err != nil {
					t.Fatal(err)
				}
				r.AdmissionVersion = 0
				err = q.save(f, r)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
				want = ErrVersion
			case "identity":
				if err := q.root.Rename(j.DeliveryID+".snapshot", "replaced.snapshot"); err != nil {
					t.Fatal(err)
				}
				f, err := q.file(j.DeliveryID+".snapshot", os.O_CREATE|os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_ = f.Close()
				want = ErrSnapshot
			}
			if err := q.Admit(t.Context(), "upload", j, source); !errors.Is(err, want) {
				t.Fatalf("%s resumed: %v", mode, err)
			}
			if err := q.RequestCancel(j.DeliveryID); err != nil {
				t.Fatal(err)
			}
			if err := q.Retire(j.DeliveryID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func BenchmarkDurableAdmissionResume(b *testing.B) {
	const size int64 = 512 << 20
	source, err := os.CreateTemp(b.TempDir(), "source")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(source, h), io.LimitReader(zeroReader{}, size)); err != nil {
		b.Fatal(err)
	}
	j := newJob([]byte("x"))
	j.SnapshotID = j.DeliveryID
	j.Spec.SizeBytes = size
	j.Spec.SHA256 = hex.EncodeToString(h.Sum(nil))
	limits := queueLimits()
	limits.Bytes = 2 * size
	limits.JournalBytes = 65536
	b.SetBytes(size)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := b.TempDir()
		for attempt := 0; attempt < 16; attempt++ {
			q, err := OpenHerdrQueue(func(string) string { return path }, "revision-1", limits)
			if err != nil {
				b.Fatal(err)
			}
			err = q.Admit(b.Context(), "upload", j, source)
			_ = q.Close()
			if err == nil {
				break
			}
			if !errors.Is(err, ErrAdmissionPending) || attempt == 15 {
				b.Fatal(err)
			}
		}
	}
}
