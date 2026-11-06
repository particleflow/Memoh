//go:build linux

package media_test

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type syntheticBytes struct{}

func (syntheticBytes) Read(b []byte) (int, error) {
	clear(b)
	return len(b), nil
}

// rchar counts actual read syscall bytes, including cached filesystem reads;
// it is not a measurement of physical disk traffic. Run this test alone.
func readCharacters(t *testing.T) int64 {
	t.Helper()
	f, err := os.Open("/proc/self/io")
	if err != nil {
		t.Skip("Linux process I/O accounting unavailable", err)
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if value, ok := strings.CutPrefix(s.Text(), "rchar: "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatal("rchar missing", s.Err())
	return 0
}

func TestSeekableReadVolume(t *testing.T) {
	if os.Getenv("MEMOH_RANGE_VOLUME") != "1" {
		t.Skip("opt-in isolated process read-volume measurement")
	}
	for _, mib := range []int64{200, 512} {
		t.Run(fmt.Sprintf("%dMiB", mib), func(t *testing.T) {
			root := t.TempDir()
			newService := func() *media.Service {
				s := media.NewService(nil, localfs.NewOriginals(root))
				s.SetRequirePersistence(true)
				return s
			}
			s := newService()
			size := mib << 20
			asset, err := s.Ingest(t.Context(), media.IngestInput{BotID: "synthetic-bot", Mime: "application/octet-stream", MaxBytes: size, Reader: io.LimitReader(syntheticBytes{}, size)})
			if err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"first", "resume", "new-provider", "resume-after-new-provider"} {
				if phase == "new-provider" {
					s = newService()
				}
				before := readCharacters(t)
				start := time.Now()
				r, _, err := s.OpenSeekable(t.Context(), asset.BotID, asset.ContentHash)
				if err != nil {
					t.Fatal(err)
				}
				opened := time.Since(start)
				if _, err := r.Seek(size-(1<<20), io.SeekStart); err != nil {
					t.Fatal(err)
				}
				n, err := io.CopyN(io.Discard, r, 64<<10)
				_ = r.Close()
				if err != nil || n != 64<<10 {
					t.Fatal(n, err)
				}
				read := readCharacters(t) - before
				t.Logf("size=%d phase=%s response_bytes=%d read_syscall_bytes=%d open=%s total=%s", size, phase, n, read, opened, time.Since(start))
				want := int64(1 << 20)
				if phase == "first" || phase == "new-provider" {
					want += size
				}
				if read < want || read > want+(64<<10) {
					t.Fatalf("unexpected whole-file scan or missing verification: read=%d, expected %d plus small accounting overhead", read, want)
				}
			}
		})
	}
}
