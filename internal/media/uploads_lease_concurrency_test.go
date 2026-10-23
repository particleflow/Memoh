package media

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type leasePausedReader struct {
	reader  io.Reader
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (r *leasePausedReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		close(r.entered)
		<-r.resume
	})
	return r.reader.Read(p)
}

func TestTransferUploadAppendCrossesExpiryWithConcurrentSweep(t *testing.T) {
	for _, progress := range []bool{true, false} {
		name := "accepted-progress"
		if !progress {
			name = "rejected-no-progress"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			writer := uploadService(t, root, 1024).Uploads()
			sweeper := uploadService(t, root, 1024).Uploads()
			var clock atomic.Int64
			clock.Store(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano())
			now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
			writer.now, sweeper.now = now, now
			body := []byte("progress")
			u := createTransferUpload(ctx, t, writer, body)
			clock.Store(u.ExpiresAt.Add(-time.Second).UnixNano())
			reader := &leasePausedReader{reader: bytes.NewReader(body[:1]), entered: make(chan struct{}), resume: make(chan struct{})}
			var release sync.Once
			defer release.Do(func() { close(reader.resume) })
			type response struct {
				receipt Upload
				err     error
			}
			done := make(chan response, 1)
			hash := digest(body[:1])
			if !progress {
				hash = digest([]byte("rejected"))
			}
			go func() {
				r, err := writer.Append(ctx, "owner", "bot", u.ID, 0, 1, hash, reader)
				done <- response{r, err}
			}()
			select {
			case <-reader.entered:
			case result := <-done:
				t.Fatal("append did not acquire lock and reach body", result.err)
			case <-time.After(5 * time.Second):
				t.Fatal("append did not reach body")
			}
			clock.Store(u.ExpiresAt.Add(time.Second).UnixNano())
			// The second store is at a time beyond the old deadline while
			// Append holds the OS receipt lock. Sweep must skip that receipt.
			if err := sweeper.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
			for _, suffix := range []string{".json", ".data"} {
				if _, err := os.Stat(filepath.Join(root, ".uploads", u.ID+suffix)); err != nil {
					t.Fatal("sweep deleted locked upload", suffix, err)
				}
			}
			release.Do(func() { close(reader.resume) })
			var result response
			select {
			case result = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("append remained blocked")
			}
			if progress && result.err != nil || !progress && !errors.Is(result.err, ErrUploadInvalid) {
				t.Fatal(result.err)
			}
			if err := sweeper.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := sweeper.Get(ctx, "owner", "bot", u.ID)
			if progress {
				if err != nil || got.ID != u.ID || got.Offset != 1 || !got.ExpiresAt.Equal(now().Add(UploadTTL)) {
					t.Fatal("durable progress lost after releasing lock", got, err)
				}
			} else {
				if !errors.Is(err, ErrUploadNotFound) {
					t.Fatal("failed append extended idle expiry", got, err)
				}
				for _, suffix := range []string{".json", ".data"} {
					if _, err := os.Stat(filepath.Join(root, ".uploads", u.ID+suffix)); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("expired idle upload not collected", suffix, err)
					}
				}
			}
		})
	}
}
