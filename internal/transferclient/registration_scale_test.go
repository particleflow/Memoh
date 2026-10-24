//go:build linux

package transferclient

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This is 10,000 metadata registrations for distinct sparse 1 GiB sources.
// It does NOT transfer/read 10 TiB, allocate that payload, or measure NAS speed.
// Smaller native tests exercise the same queue/source code on Windows.
func TestRegistrationTenThousandMetadataNoPayloadCopies(t *testing.T) {
	q, reopen, cfg, template := registeredFixture(t)
	const count = 10000
	const size = int64(1 << 30)
	h := sha256.New()
	if _, err := io.CopyN(h, zeroReader{}, size); err != nil {
		t.Fatal(err)
	}
	template.Spec.SizeBytes, template.Spec.SHA256 = size, hex.EncodeToString(h.Sum(nil))
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	start := time.Now()
	var peak uint64
	for i := range count {
		name := fmt.Sprintf("large-%05d", i)
		f, err := openPrivateFile(cfg.sources, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		// Linux ftruncate creates holes here, not a physical GiB. No source is
		// transmitted and the test never asserts sparsity is a production feature.
		err = f.Truncate(size)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		job := template
		job.DeliveryID, job.SnapshotID = name, name
		r, err := q.RegisterUpload(t.Context(), job, name, cfg.OpenSource)
		if err != nil || !r.Registered || r.Activated || r.Admitted || r.Job.Spec.SizeBytes != size {
			t.Fatalf("register %d: %v", i, err)
		}
		if i%100 == 0 {
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			peak = max(peak, mem.HeapAlloc)
		}
	}
	registeredIn := time.Since(start)
	_ = q.Close()
	q = reopen()
	seen := 0
	if err := q.visitAdmissions(func(r QueueStatus) error {
		if !r.Registered || r.Admitted || r.Activated || r.Job.ReceiptID != "" {
			return ErrStore
		}
		seen++
		return nil
	}); err != nil || seen != count+1 {
		t.Fatal("reopen lost registrations", seen, err)
	}
	for _, i := range []int{0, 4999, 9999} {
		name := fmt.Sprintf("large-%05d", i)
		job := template
		job.DeliveryID, job.SnapshotID = name, name
		r, err := q.RegisterUpload(t.Context(), job, name, cfg.OpenSource)
		if err != nil || r.Job != job {
			t.Fatal("original metadata replay", err)
		}
	}
	var journalBytes, allocated int64
	if err := filepath.WalkDir(q.root.Name(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".snapshot", ".part", ".tree", ".admission":
			return ErrStore
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		journalBytes += info.Size()
		var st unix.Stat_t
		if err := unix.Stat(path, &st); err != nil {
			return err
		}
		allocated += st.Blocks * 512
		return nil
	}); err != nil {
		t.Fatal("unexpected payload allocation", err)
	}
	if journalBytes > 32<<20 || allocated > 80<<20 || peak > 128<<20 {
		t.Fatalf("metadata resource regression bytes=%d allocated=%d heap=%d", journalBytes, allocated, peak)
	}
	selectStart := time.Now()
	id, err := q.nextDue(time.Now())
	if err != nil || id == "" {
		t.Fatal("pending metadata not schedulable", err)
	}
	t.Logf("distinct_sparse_sources=%d logical_bytes=%d registration_time=%s queue_file_bytes=%d queue_allocated_bytes=%d sampled_heap_baseline=%d sampled_heap_peak=%d sampled_heap_growth=%d select_time=%s", count, int64(count)*size, registeredIn, journalBytes, allocated, baseline.HeapAlloc, peak, peak-baseline.HeapAlloc, time.Since(selectStart))
}
