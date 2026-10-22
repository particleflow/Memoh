package localfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func rangeFixture(t *testing.T) (*Originals, string, []byte, *atomic.Int64) {
	t.Helper()
	p := NewOriginals(t.TempDir())
	payload := bytes.Repeat([]byte("different synthetic block bytes!"), 110000)
	key := originalKey("bot-a", payload)
	if err := p.Put(t.Context(), key, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	counter := &atomic.Int64{}
	p.readAt = func(f *os.File, b []byte, offset int64) (int, error) {
		n, err := f.ReadAt(b, offset)
		counter.Add(int64(n))
		return n, err
	}
	return p, key, payload, counter
}

func readRangeFixture(t *testing.T, p *Originals, key string, offset int64, want []byte) {
	t.Helper()
	r, err := p.OpenVerifiedRange(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(r, got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("range mismatch: %v", err)
	}
}

func TestVerifiedRangeWarmReadsAreBoundedAndRestartRevalidates(t *testing.T) {
	p, key, payload, reads := rangeFixture(t)
	start := int64(rangeBlockBytes + 5)
	want := payload[start : start+100]
	readRangeFixture(t, p, key, start, want)
	if n := reads.Swap(0); n != int64(len(payload))+rangeBlockBytes {
		t.Fatalf("cold read bytes=%d", n)
	}
	for range 3 {
		readRangeFixture(t, p, key, start, want)
		if n := reads.Swap(0); n != rangeBlockBytes {
			t.Fatalf("warm range scanned %d bytes", n)
		}
	}
	// Cache identity is not a stored seal. A fresh provider must establish SHA.
	restarted := NewOriginals(p.root)
	restarted.readAt = p.readAt
	readRangeFixture(t, restarted, key, start, want)
	if n := reads.Swap(0); n != int64(len(payload))+rangeBlockBytes {
		t.Fatalf("restart trusted an unverified filename: %d", n)
	}
	// The same hash in a different Bot has a separate verification cache entry.
	other := strings.Replace(key, "bot-a/", "bot-b/", 1)
	if err := p.Put(t.Context(), other, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	readRangeFixture(t, p, other, start, want)
	if n := reads.Swap(0); n != int64(len(payload))+rangeBlockBytes {
		t.Fatalf("another Bot reused cached trust: %d", n)
	}
}

func TestVerifiedRangeDetectsChangesOutsideRequestedBlock(t *testing.T) {
	for _, mutation := range []string{"in-place-with-restored-mtime", "replace", "replace-root"} {
		t.Run(mutation, func(t *testing.T) {
			p, key, payload, _ := rangeFixture(t)
			readRangeFixture(t, p, key, 0, payload[:100])
			filename := filepath.Join(p.root, key)
			info, err := os.Stat(filename)
			if err != nil {
				t.Fatal(err)
			}
			bad := bytes.Clone(payload)
			bad[len(bad)-1] ^= 1
			switch mutation {
			case "replace-root":
				other := NewOriginals(t.TempDir())
				if err := other.Put(t.Context(), key, bytes.NewReader(payload)); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(p.root, p.root+"-retained"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Rename(p.root, other.root); _ = os.Rename(p.root+"-retained", p.root) })
				if err := os.Rename(other.root, p.root); err != nil {
					t.Fatal(err)
				}
			case "replace":
				if err := os.Rename(filename, filename+".retained"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filename, bad, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(filename, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if r, err := p.OpenVerifiedRange(t.Context(), key); err == nil {
				_ = r.Close()
				t.Fatal("out-of-range corruption was accepted")
			}
		})
	}
}

func TestVerifiedRangeSeekBoundariesAndFullRead(t *testing.T) {
	p, key, payload, _ := rangeFixture(t)
	readRangeFixture(t, p, key, rangeBlockBytes-3, payload[rangeBlockBytes-3:rangeBlockBytes+3])
	r, err := p.OpenVerifiedRange(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if pos, err := r.Seek(-5, io.SeekEnd); err != nil || pos != int64(len(payload)-5) {
		t.Fatal(pos, err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, payload[len(payload)-5:]) {
		t.Fatal("tail mismatch", err)
	}
	if _, err := r.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("negative seek accepted")
	}
	if _, err := r.Seek(1<<63-1, io.SeekEnd); err == nil {
		t.Fatal("overflow seek accepted")
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	if n, err := io.Copy(h, r); err != nil || n != int64(len(payload)) || !bytes.Equal(h.Sum(nil), sha256Sum(payload)) {
		t.Fatal("full read mismatch", n, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Seek(0, io.SeekStart); !errors.Is(err, os.ErrClosed) || strings.Contains(err.Error(), p.root) {
		t.Fatal("seek error lost cause or exposed the physical root", err)
	}
}

func sha256Sum(payload []byte) []byte {
	sum := sha256.Sum256(payload)
	return sum[:]
}

func TestVerifiedRangeNeverEmitsAnUnverifiedBlock(t *testing.T) {
	p, key, payload, _ := rangeFixture(t)
	readRangeFixture(t, p, key, 0, payload[:100])
	// Inject a read-time bit flip without a metadata change. The old full SHA
	// scan cannot protect later reads from this; block verification must do so.
	p.readAt = func(f *os.File, b []byte, offset int64) (int, error) {
		n, err := f.ReadAt(b, offset)
		if n > 0 {
			b[0] ^= 1
		}
		return n, err
	}
	r, err := p.OpenVerifiedRange(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	buf := bytes.Repeat([]byte{0xaa}, 100)
	if n, err := r.Read(buf); n != 0 || err == nil || !bytes.Equal(buf, bytes.Repeat([]byte{0xaa}, 100)) {
		t.Fatal("unverified bytes escaped", n, err)
	}
}

func TestVerifiedRangeFailedFillInvalidatesPreviousBuffer(t *testing.T) {
	p, key, payload, _ := rangeFixture(t)
	readRangeFixture(t, p, key, 0, payload[:100])
	p.readAt = func(f *os.File, b []byte, offset int64) (int, error) {
		n, err := f.ReadAt(b, offset)
		if offset == rangeBlockBytes && n > 0 {
			b[0] ^= 1
		}
		return n, err
	}
	r, err := p.OpenVerifiedRange(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	buf := make([]byte, 100)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Seek(rangeBlockBytes, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Read(buf); n != 0 || err == nil {
		t.Fatal("bad block accepted", n, err)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(r, buf); err != nil || !bytes.Equal(buf, payload[:100]) {
		t.Fatal("failed fill poisoned previously verified block", err)
	}
}

func TestVerifiedRangeRejectsMutationAfterOpenAndHardlinks(t *testing.T) {
	p, key, payload, _ := rangeFixture(t)
	r, err := p.OpenVerifiedRange(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	filename := filepath.Join(p.root, key)
	if err := os.WriteFile(filename, append(bytes.Clone(payload[:len(payload)-1]), '!'), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Read(make([]byte, 100)); n != 0 || err == nil {
		t.Fatal("open descriptor ignored change", n, err)
	}
	if err := os.Link(filename, filepath.Join(p.root, "other-link")); err != nil {
		t.Fatal(err)
	}
	if r, err := p.OpenVerifiedRange(t.Context(), key); err == nil {
		_ = r.Close()
		t.Fatal("original with a hard link accepted")
	}
}

func TestVerifiedRangeConcurrentColdReadsVerifyOnce(t *testing.T) {
	p, key, payload, reads := rangeFixture(t)
	var wg sync.WaitGroup
	const concurrent = 8
	for range concurrent {
		wg.Go(func() { readRangeFixture(t, p, key, 0, payload[:100]) })
	}
	wg.Wait()
	if n, want := reads.Load(), int64(len(payload))+concurrent*rangeBlockBytes; n != want {
		t.Fatalf("concurrent verification read %d bytes, want %d", n, want)
	}
}

func TestVerifiedRangeCancellationDoesNotPublishIndexOrHoldLock(t *testing.T) {
	p, key, payload, reads := rangeFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	baseRead := p.readAt
	p.readAt = func(f *os.File, b []byte, offset int64) (int, error) {
		n, err := baseRead(f, b, offset)
		cancel()
		return n, err
	}
	if _, err := p.OpenVerifiedRange(ctx, key); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if n := reads.Swap(0); n != rangeBlockBytes {
		t.Fatalf("cancellation did not bound scan: %d", n)
	}
	p.readAt = baseRead
	readRangeFixture(t, p, key, 0, payload[:100])
	if n := reads.Load(); n != int64(len(payload))+rangeBlockBytes {
		t.Fatalf("partial scan was cached: %d", n)
	}
	// Waiting for the same verification stripe is also bounded by context.
	unlock, err := p.ranges.lock(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	waitCtx, stop := context.WithTimeout(t.Context(), time.Millisecond)
	defer stop()
	if _, err := p.OpenVerifiedRange(waitCtx, key); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestVerifiedRangeCacheEvictionAndMemoryBound(t *testing.T) {
	p, key, payload, reads := rangeFixture(t)
	readRangeFixture(t, p, key, 0, payload[:1])
	for i := range rangeCacheItems {
		data := []byte(fmt.Sprintf("synthetic eviction %d", i))
		itemKey := originalKey("bot-a", data)
		if err := p.Put(t.Context(), itemKey, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		readRangeFixture(t, p, itemKey, 0, data)
	}
	reads.Store(0)
	readRangeFixture(t, p, key, 0, payload[:1])
	if n := reads.Load(); n != int64(len(payload))+rangeBlockBytes {
		t.Fatalf("evicted content reused stale trust: %d", n)
	}
	// Metadata-only fixtures exercise the byte budget without creating huge
	// originals. Real I/O volume is measured separately with 200/512 MiB files.
	for i := range 5 {
		p.ranges.put(fmt.Sprintf("synthetic-index-%d", i), &rangeIndex{blocks: make([][sha256.Size]byte, (4<<20)/sha256.Size)})
	}
	if p.ranges.bytes > rangeCacheBytes || len(p.ranges.items) > rangeCacheItems {
		t.Fatal("verification cache exceeded budget")
	}
}

func TestVerifiedRangeConfinesKeysAndLinks(t *testing.T) {
	p, key, payload, _ := rangeFixture(t)
	for _, invalid := range []string{"../" + key, "/" + key, "bot-b/../" + key, "bot-b\\..\\" + key} {
		if r, err := p.OpenVerifiedRange(t.Context(), invalid); err == nil {
			_ = r.Close()
			t.Fatal("unsafe key opened")
		}
	}
	if err := os.Symlink("bot-a", filepath.Join(p.root, "bot-link")); err != nil {
		t.Fatal(err)
	}
	if r, err := p.OpenVerifiedRange(t.Context(), strings.Replace(key, "bot-a/", "bot-link/", 1)); err == nil {
		_ = r.Close()
		t.Fatal("Bot symlink opened")
	}
	filename := filepath.Join(p.root, key)
	if err := os.Rename(filename, filename+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(filename)+".retained", filename); err != nil {
		t.Fatal(err)
	}
	if r, err := p.OpenVerifiedRange(t.Context(), key); err == nil {
		_ = r.Close()
		t.Fatal("final symlink opened")
	}
	disk, err := os.OpenRoot(p.root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = disk.Close() }()
	got, err := disk.ReadFile(key + ".retained")
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("original changed", err)
	}
}
