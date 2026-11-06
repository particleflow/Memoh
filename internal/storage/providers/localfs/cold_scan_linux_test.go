package localfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// Each real deadline expires after exactly one completed read, independently
// of disk speed. This models a cold scan slower than the caller's budget without
// spending two minutes per attempt or changing any production timeout.
func TestColdScanResumesAcrossRequestDeadlines(t *testing.T) {
	p, key, payload, reads := rangeFixture(t)
	baseRead := p.readAt
	var offsets []int64
	for attempt := 1; attempt <= 12; attempt++ {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		p.readAt = func(f *os.File, b []byte, offset int64) (int, error) {
			offsets = append(offsets, offset)
			n, err := baseRead(f, b, offset)
			<-ctx.Done()
			return n, err
		}
		r, err := p.OpenVerifiedRange(ctx, key)
		cancel()
		if err == nil {
			_ = r.Close()
			p.readAt = baseRead
			readRangeFixture(t, p, key, 0, payload[:100])
			if got := reads.Load(); got != int64(len(payload))+rangeBlockBytes {
				t.Fatalf("read bytes=%d, expected single cold scan plus returned block", got)
			}
			t.Logf("complete after %d bounded attempts; scan offsets=%v; read bytes=%d", attempt, offsets, reads.Load())
			return
		}
		if r != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("partial verification exposed a reader or wrong error: %v %v", r, err)
		}
		assertPausedScan(t, p, key)
	}
	t.Fatalf("12 deadlines exhausted without completing: scan offsets=%v read bytes=%d", offsets, reads.Load())
}

func assertPausedScan(t *testing.T, p *Originals, key string) {
	t.Helper()
	p.ranges.mu.Lock()
	defer p.ranges.mu.Unlock()
	e := p.ranges.items[key]
	if e == nil {
		t.Fatal("completed scan work was not retained")
	}
	item := e.Value.(cachedRange)
	if item.index != nil || item.scan == nil || item.scan.next == 0 {
		t.Fatal("incomplete work was published as a readable index")
	}
}

func pauseScan(t *testing.T, p *Originals, key string) {
	t.Helper()
	base := p.readAt
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var descriptor *os.File
	p.readAt = func(f *os.File, b []byte, offset int64) (int, error) {
		descriptor = f
		n, err := base(f, b, offset)
		cancel()
		return n, err
	}
	r, err := p.OpenVerifiedRange(ctx, key)
	p.readAt = base
	if r != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("paused scan returned reader=%v err=%v", r, err)
	}
	if descriptor == nil {
		t.Fatal("fixture did not finish a scan block")
	}
	if _, err := descriptor.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("paused work retained an open descriptor", err)
	}
	assertPausedScan(t, p, key)
}

func TestColdScanChangedIdentityDiscardsProgress(t *testing.T) {
	for _, mutation := range []string{"restored-mtime", "replacement", "root-replacement", "mode", "hardlink"} {
		t.Run(mutation, func(t *testing.T) {
			p, key, payload, reads := rangeFixture(t)
			pauseScan(t, p, key)
			filename := filepath.Join(p.root, key)
			info, err := os.Stat(filename)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "root-replacement":
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
			case "replacement":
				if err := os.Rename(filename, filename+".retained"); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(filename, 0o400); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(filename, filepath.Join(p.root, "other-link")); err != nil {
					t.Fatal(err)
				}
			}
			if mutation != "mode" && mutation != "hardlink" {
				bad := bytes.Clone(payload)
				bad[0] ^= 1 // The changed byte was already hashed in the paused prefix.
				if err := os.WriteFile(filename, bad, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(filename, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			}
			reads.Store(0)
			if mutation == "mode" {
				readRangeFixture(t, p, key, 0, payload[:100])
				if reads.Load() != int64(len(payload))+rangeBlockBytes {
					t.Fatal("mode change reused a partial verification")
				}
				return
			}
			if r, err := p.OpenVerifiedRange(t.Context(), key); err == nil || r != nil {
				t.Fatal("changed original accepted using a stale prefix")
			}
		})
	}
}

func TestColdScanReadFailureAndMutationDuringPauseDiscardWork(t *testing.T) {
	for _, mode := range []string{"read-error", "mutation-at-cancel", "full-sha-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			p, key, payload, reads := rangeFixture(t)
			pauseScan(t, p, key)
			base := p.readAt
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			p.readAt = func(f *os.File, b []byte, offset int64) (int, error) {
				if mode == "read-error" {
					return 0, io.ErrUnexpectedEOF
				}
				n, err := base(f, b, offset)
				if mode == "mutation-at-cancel" {
					bad := bytes.Clone(payload)
					bad[0] ^= 1
					if writeErr := os.WriteFile(filepath.Join(p.root, key), bad, 0o600); writeErr != nil {
						t.Fatal(writeErr)
					}
					cancel()
				} else if n > 0 {
					b[0] ^= 1 // A read-time bit flip with unchanged file metadata.
				}
				return n, err
			}
			if r, err := p.OpenVerifiedRange(ctx, key); r != nil || err == nil {
				t.Fatal("uncertain bytes completed verification")
			}
			if len(p.ranges.items) != 0 {
				t.Fatal("failed scan retained reusable state")
			}
			p.readAt = base
			if mode != "mutation-at-cancel" {
				reads.Store(0)
				readRangeFixture(t, p, key, 0, payload[:100])
				if reads.Load() != int64(len(payload))+rangeBlockBytes {
					t.Fatal("failed scan prefix was reused")
				}
			}
		})
	}
}

func TestColdScanRestartBotAndWaitingCancellation(t *testing.T) {
	p, key, payload, reads := rangeFixture(t)
	pauseScan(t, p, key)
	restarted := NewOriginals(p.root)
	restarted.readAt = p.readAt
	reads.Store(0)
	readRangeFixture(t, restarted, key, 0, payload[:100])
	if reads.Load() != int64(len(payload))+rangeBlockBytes {
		t.Fatal("new process/provider imported unfinished trust")
	}
	other := strings.Replace(key, "bot-a/", "bot-b/", 1)
	if err := p.Put(t.Context(), other, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	reads.Store(0)
	readRangeFixture(t, p, other, 0, payload[:100])
	if reads.Load() != int64(len(payload))+rangeBlockBytes {
		t.Fatal("different Bot took over another Bot's scan")
	}
	unlock, err := p.ranges.lock(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if r, err := p.OpenVerifiedRange(ctx, key); r != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiting cancellation ignored", err)
	}
	unlock()
	assertPausedScan(t, p, key)
	reads.Store(0)
	readRangeFixture(t, p, key, 0, payload[:100])
	if reads.Load() != int64(len(payload)) {
		t.Fatal("waiting request discarded another request's progress")
	}
}

func TestColdScanConcurrentTakeoverHasOneOwner(t *testing.T) {
	p, key, payload, reads := rangeFixture(t)
	base := p.readAt
	first := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var firstRead atomic.Bool
	p.readAt = func(f *os.File, b []byte, offset int64) (int, error) {
		n, err := base(f, b, offset)
		if firstRead.CompareAndSwap(false, true) {
			close(first)
			<-release
			cancel()
		}
		return n, err
	}
	finished := make(chan error, 1)
	go func() {
		r, err := p.OpenVerifiedRange(ctx, key)
		if r != nil {
			_ = r.Close()
			finished <- errors.New("canceled scan exposed reader")
			return
		}
		finished <- err
	}()
	<-first
	var wg sync.WaitGroup
	const waiters = 8
	for range waiters {
		wg.Go(func() { readRangeFixture(t, p, key, 0, payload[:100]) })
	}
	close(release)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	wg.Wait()
	if reads.Load() != int64(len(payload))+waiters*rangeBlockBytes {
		t.Fatalf("concurrent takeover repeated or skipped scan bytes: %d", reads.Load())
	}
}

func TestColdScanSharesCacheLimitsAndEvictsSafely(t *testing.T) {
	p, key, payload, reads := rangeFixture(t)
	pauseScan(t, p, key)
	for i := range rangeCacheItems {
		data := []byte(fmt.Sprintf("unfinished synthetic %d", i))
		itemKey := originalKey("bot-a", data)
		if err := p.Put(t.Context(), itemKey, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		pauseScan(t, p, itemKey)
	}
	reads.Store(0)
	readRangeFixture(t, p, key, 0, payload[:100])
	if reads.Load() != int64(len(payload))+rangeBlockBytes {
		t.Fatal("evicted scan was reused")
	}
	for i := range 5 {
		blocks := make([][sha256.Size]byte, (4<<20)/sha256.Size)
		if i%2 == 0 {
			p.ranges.putScan(fmt.Sprint("partial-", i), &rangeScan{blocks: blocks, next: 1, full: sha256.New()})
		} else {
			p.ranges.put(fmt.Sprint("complete-", i), &rangeIndex{blocks: blocks})
		}
	}
	if p.ranges.bytes > rangeCacheBytes || len(p.ranges.items) > rangeCacheItems {
		t.Fatal("pending and complete work exceeded their shared budget")
	}
}

type coldScanZeros struct{}

func (coldScanZeros) Read(b []byte) (int, error) { clear(b); return len(b), nil }

func TestColdScanLargeResumeReadVolume(t *testing.T) {
	if os.Getenv("MEMOH_COLD_SCAN_VOLUME") != "1" {
		t.Skip("opt-in 200/512 MiB interrupted cold verification")
	}
	for _, size := range []int64{200 << 20, 512 << 20} {
		t.Run(fmt.Sprintf("%dMiB", size>>20), func(t *testing.T) {
			p := NewOriginals(t.TempDir())
			h := sha256.New()
			if _, err := io.CopyN(h, coldScanZeros{}, size); err != nil {
				t.Fatal(err)
			}
			hash := hex.EncodeToString(h.Sum(nil))
			key := "bot-a/" + hash[:2] + "/" + hash + ".bin"
			if err := p.Put(t.Context(), key, io.LimitReader(coldScanZeros{}, size)); err != nil {
				t.Fatal(err)
			}
			var total int64
			for attempt := 1; attempt <= 12; attempt++ {
				ctx, cancel := context.WithCancel(t.Context())
				var current int64
				p.readAt = func(f *os.File, b []byte, offset int64) (int, error) {
					n, err := f.ReadAt(b, offset)
					current += int64(n)
					total += int64(n)
					if current >= 64<<20 {
						cancel()
					}
					return n, err
				}
				r, err := p.OpenVerifiedRange(ctx, key)
				if err != nil {
					cancel()
					if r != nil || !errors.Is(err, context.Canceled) {
						t.Fatal("interrupted verification exposed reader", err)
					}
					assertPausedScan(t, p, key)
					continue
				}
				if _, err := io.CopyN(io.Discard, r, 64<<10); err != nil {
					t.Fatal(err)
				}
				_ = r.Close()
				cancel()
				if total != size+rangeBlockBytes {
					t.Fatalf("read bytes=%d, expected %d", total, size+rangeBlockBytes)
				}
				t.Logf("size=%d attempts=%d scan_and_response_read_bytes=%d; cancellation every 64 MiB; response=64 KiB", size, attempt, total)
				return
			}
			t.Fatal("bounded transfer retry budget exhausted")
		})
	}
}
