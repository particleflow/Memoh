package transferclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestKnownReceiptCheckpointFailureRecoversAndServerOffsetWins(t *testing.T) {
	src := frozen("abcdefgh")
	f, c := newFixture(t)
	s := storeJob(t, newJob(src))
	advance(t, c, s, src)
	err := c.Advance(t.Context(), s.load(), func(j Job) error {
		if j.Offset > 0 {
			return errors.New("synthetic crash after PATCH")
		}
		return s.save(j)
	}, src, time.Now())
	if !errors.Is(err, ErrCheckpoint) || s.load().Offset != 0 || fixtureReceipt(f).Offset != 4 {
		t.Fatal("checkpoint fault was not preserved")
	}
	// A stale local offset never drives a PATCH; even a lower server offset wins.
	j := s.load()
	j.Offset = 7
	if err := s.save(j); err != nil {
		t.Fatal(err)
	}
	advance(t, c, s, src)
	j = advance(t, c, s, src)
	if j.Phase != "ready" || f.count("PATCH /bots/bot-1/media/uploads/receipt-1") != 2 {
		t.Fatal("stale offset replayed")
	}
}

func TestCancellationBeforeAdmission(t *testing.T) {
	src := frozen("abcd")
	f, c := newFixture(t)
	j := newJob(src)
	j.CancelRequested = true
	s := storeJob(t, j)
	if err := c.Advance(t.Context(), j, s.save, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := s.load(); got.Phase != "canceled" || f.count("GET /bots/bot-1/media/limits") != 0 {
		t.Fatal("local cancel created remote work")
	}
}

func TestDownloadFullCheckpointRechecksAuthorization(t *testing.T) {
	data := []byte("complete local checkpoint")
	file, err := os.CreateTemp(t.TempDir(), "part")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	_, _ = file.Write(data)
	var requests atomic.Int64
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodHead {
			t.Error("expected authorization HEAD")
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer s.Close()
	c, err := New(s.URL, s.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	err = c.Download(t.Context(), "bot-1", digest(data), int64(len(data)), int64(len(data)), file, func(int64) error { t.Fatal("unexpected checkpoint"); return nil })
	var h *HTTPError
	if !errors.As(err, &h) || h.Status != 403 || requests.Load() != 1 {
		t.Fatal("local completion bypassed revoked grant")
	}
}

func TestCreateQuotaRejectionCanRetryButUnknownCannot(t *testing.T) {
	var creates atomic.Int64
	src := frozen("abcd")
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creates.Add(1)
			w.Header().Set("Retry-After", "3000")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"max_upload_bytes":1024,"resumable_enabled":true,"chunk_max_bytes":4}`)
	}))
	defer s.Close()
	c, err := New(s.URL, s.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	store := storeJob(t, newJob(src))
	before := time.Now()
	j := advance(t, c, store, src)
	if j.Phase != "queued" || j.NextAttempt.Before(before.Add(2999*time.Second)) || j.NextAttempt.After(time.Now().Add(3001*time.Second)) {
		t.Fatalf("create quota mishandled: %+v", j)
	}
	advance(t, c, store, src)
	if creates.Load() != 2 {
		t.Fatal("definitely rejected create cannot retry")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// This benchmark measures bounded client hashing only, not NAS/NIC throughput.
func BenchmarkVerify(b *testing.B) {
	for _, size := range []int64{1 << 20, 512 << 20} {
		b.Run(strconv.FormatInt(size>>20, 10)+"MiB", func(b *testing.B) {
			h := sha256.New()
			_, _ = io.CopyBuffer(h, io.LimitReader(zeroReader{}, size), make([]byte, copyBytes))
			spec := Spec{SizeBytes: size, SHA256: hex.EncodeToString(h.Sum(nil)), Mime: "application/octet-stream"}
			b.SetBytes(size)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := Verify(b.Context(), io.LimitReader(zeroReader{}, size), spec); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
