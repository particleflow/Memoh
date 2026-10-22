package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const syntheticAuth = "Bearer synthetic-fixture-only"

func digest(p []byte) string { h := sha256.Sum256(p); return hex.EncodeToString(h[:]) }

type frozen []byte

func (f frozen) ReadAt(_ context.Context, _, _ string, spec Spec, dst []byte, offset int64) error {
	if digest(f) != spec.SHA256 || int64(len(f)) != spec.SizeBytes {
		return ErrSnapshot
	}
	if copy(dst, f[offset:]) != len(dst) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

type fixture struct {
	mu      sync.Mutex
	spec    Spec
	receipt Receipt
	data    []byte
	ready   []byte
	calls   map[string]int
	drop    string
	status  int
	corrupt bool
}

func (f *fixture) setDrop(key string)   { f.mu.Lock(); defer f.mu.Unlock(); f.drop = key }
func (f *fixture) setStatus(status int) { f.mu.Lock(); defer f.mu.Unlock(); f.status = status }
func (f *fixture) setCorrupt()          { f.mu.Lock(); defer f.mu.Unlock(); f.corrupt = true }
func (f *fixture) count(key string) int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls[key] }
func (f *fixture) current() (Receipt, []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.receipt, append([]byte(nil), f.ready...)
}

func newFixture(t *testing.T) (*fixture, *Client) {
	t.Helper()
	f := &fixture{calls: map[string]int{}}
	s := httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(s.Close)
	c, err := New(s.URL, s.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != syntheticAuth {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	key := r.Method + " " + r.URL.Path
	f.calls[key]++
	if f.status != 0 {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte("PRIVATE_FIXTURE_DIAGNOSTIC"))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/limits") {
		_ = json.NewEncoder(w).Encode(Limits{MaxUploadBytes: 20 << 20, ResumableEnabled: true, ChunkMaxBytes: 4})
		return
	}
	status := 200
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/uploads"):
		if json.NewDecoder(r.Body).Decode(&f.spec) != nil || !f.spec.valid() {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.data = nil
		f.receipt = Receipt{ID: "receipt-1", State: "uploading", SizeBytes: f.spec.SizeBytes, ExpiresAt: time.Now().Add(24 * time.Hour), Mime: f.spec.Mime}
		status = 201
	case r.Method == http.MethodPatch:
		body, _ := io.ReadAll(io.LimitReader(r.Body, MaxChunkBytes+1))
		offset, _ := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
		if r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("Upload-Length") != strconv.Itoa(len(body)) || digest(body) != r.Header.Get("Upload-SHA256") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if offset != f.receipt.Offset {
			w.WriteHeader(http.StatusConflict)
			return
		}
		f.data = append(f.data, body...)
		f.receipt.Offset = int64(len(f.data))
	case strings.HasSuffix(r.URL.Path, "/complete"):
		if int64(len(f.data)) != f.spec.SizeBytes || digest(f.data) != f.spec.SHA256 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.ready = append([]byte(nil), f.data...)
		f.receipt.State, f.receipt.ContentHash = "ready", f.spec.SHA256
	case r.Method == http.MethodDelete:
		if f.receipt.State == "ready" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		f.receipt.State = "canceled"
	case r.Method != http.MethodGet:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if f.drop == key {
		f.drop = ""
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
		return
	}
	out := f.receipt
	if f.corrupt {
		out.SizeBytes++
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

func newJob(data []byte) Job {
	return Job{Version: 1, DeliveryID: "delivery-1", BotID: "bot-1", OperationID: "operation-1", SnapshotID: "snapshot-1", LocatorRevision: "revision-1", Phase: "queued", Spec: Spec{SizeBytes: int64(len(data)), SHA256: digest(data), Mime: "application/octet-stream"}}
}

// This test store exercises reopen/checkpoint ordering. It is deliberately not
// shipped as a Windows store, lock manager or durability implementation.
type testStore struct {
	path string
	t    *testing.T
}

func storeJob(t *testing.T, j Job) testStore {
	t.Helper()
	s := testStore{path: filepath.Join(t.TempDir(), "checkpoint.json"), t: t}
	if err := s.save(j); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s testStore) load() Job {
	s.t.Helper()
	b, err := os.ReadFile(s.path)
	if err != nil {
		s.t.Fatal(err)
	}
	var j Job
	if json.Unmarshal(b, &j) != nil {
		s.t.Fatal("checkpoint unreadable")
	}
	return j
}

func (s testStore) save(j Job) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}

func advance(t *testing.T, c *Client, s testStore, src frozen) Job {
	t.Helper()
	j := s.load()
	now := time.Now()
	if now.Before(j.NextAttempt) {
		now = j.NextAttempt
	}
	if err := c.Advance(t.Context(), j, s.save, src, now); err != nil {
		t.Fatal(err)
	}
	return s.load()
}

func TestLostPatchAndCompleteRecoverFromDurableReceipt(t *testing.T) {
	src := frozen{0xff, 0, 1, 2, 0x80, 4, 5, 6, 7}
	f, c := newFixture(t)
	s := storeJob(t, newJob(src))
	advance(t, c, s, src)
	f.setDrop("PATCH /bots/bot-1/media/uploads/receipt-1")
	j := advance(t, c, s, src)
	if j.Offset != 0 || j.NextAttempt.IsZero() || fixtureReceipt(f).Offset != 4 {
		t.Fatalf("lost PATCH was acknowledged locally: %+v", j)
	}
	for s.load().Offset < int64(len(src)) {
		advance(t, c, s, src)
	}
	f.setDrop("POST /bots/bot-1/media/uploads/receipt-1/complete")
	j = advance(t, c, s, src)
	if j.Ready != nil || fixtureReceipt(f).State != "ready" {
		t.Fatal("lost completion exposed readiness")
	}
	j = advance(t, c, s, src)
	if j.Ready == nil || j.Ready.ContentHash != digest(src) || !bytes.Equal(fixtureReady(f), src) || f.count("PATCH /bots/bot-1/media/uploads/receipt-1") != 3 {
		t.Fatal("did not reconcile exact binary bytes without replay")
	}
	r, err := c.Complete(t.Context(), j.BotID, j.ReceiptID)
	if err != nil || r.ContentHash != j.Ready.ContentHash {
		t.Fatal("completion not idempotent")
	}
}

func TestUnknownCreateNeverBlindlyRepeats(t *testing.T) {
	for _, scenario := range []string{"lost-response", "crash-before-receipt-save", "save-before-create-fails"} {
		t.Run(scenario, func(t *testing.T) {
			src := frozen("example")
			f, c := newFixture(t)
			s := storeJob(t, newJob(src))
			save := s.save
			switch scenario {
			case "lost-response":
				f.setDrop("POST /bots/bot-1/media/uploads")
			case "crash-before-receipt-save":
				save = func(j Job) error {
					if j.ReceiptID != "" {
						return errors.New("synthetic crash")
					}
					return s.save(j)
				}
			case "save-before-create-fails":
				save = func(Job) error { return errors.New("synthetic disk full") }
			}
			err := c.Advance(t.Context(), s.load(), save, src, time.Now())
			if scenario != "lost-response" && !errors.Is(err, ErrCheckpoint) {
				t.Fatal(err)
			}
			if scenario == "save-before-create-fails" {
				if f.count("POST /bots/bot-1/media/uploads") != 0 {
					t.Fatal("network effect before durable intent")
				}
				return
			}
			for range 3 {
				advance(t, c, s, src)
			}
			if s.load().Phase != "create_unknown" || f.count("POST /bots/bot-1/media/uploads") != 1 {
				t.Fatal("unknown create repeated")
			}
		})
	}
}

func TestRetryAuthorizationExpiryAndSourceChange(t *testing.T) {
	for _, scenario := range []string{"quota", "revoked", "expired", "source-change", "receipt-mismatch", "checksum", "size-limit"} {
		t.Run(scenario, func(t *testing.T) {
			src := frozen("abcdefgh")
			f, c := newFixture(t)
			s := storeJob(t, newJob(src))
			advance(t, c, s, src)
			want := "blocked"
			switch scenario {
			case "quota":
				f.setStatus(429)
				want = "uploading"
			case "revoked":
				f.setStatus(403)
			case "expired":
				f.setStatus(404)
			case "source-change":
				src[0] = '!'
			case "receipt-mismatch":
				f.setCorrupt()
			case "checksum":
				f.setStatus(400)
			case "size-limit":
				f.setStatus(413)
			}
			j := advance(t, c, s, src)
			if j.Phase != want || j.Ready != nil {
				t.Fatalf("unexpected state: %+v", j)
			}
			if scenario == "quota" {
				before := f.count("GET /bots/bot-1/media/uploads/receipt-1")
				if err := c.Advance(t.Context(), j, s.save, src, j.NextAttempt.Add(-time.Nanosecond)); err != nil {
					t.Fatal(err)
				}
				if f.count("GET /bots/bot-1/media/uploads/receipt-1") != before {
					t.Fatal("backoff bypassed")
				}
				for range 12 {
					advance(t, c, s, src)
				}
				if s.load().ErrorCode != "retry_budget" {
					t.Fatal("unbounded retries")
				}
			}
			if f.count("PATCH /bots/bot-1/media/uploads/receipt-1") != 0 || f.count("POST /bots/bot-1/media/uploads") != 1 {
				t.Fatal("failed state caused extra mutation")
			}
		})
	}
}

func TestCancellationLossAndPublicationRace(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(strconv.FormatBool(published), func(t *testing.T) {
			src := frozen("abcd")
			f, c := newFixture(t)
			s := storeJob(t, newJob(src))
			advance(t, c, s, src)
			advance(t, c, s, src)
			if published {
				if _, err := c.Complete(t.Context(), "bot-1", "receipt-1"); err != nil {
					t.Fatal(err)
				}
			}
			j := s.load()
			j.CancelRequested = true
			if err := s.save(j); err != nil {
				t.Fatal(err)
			}
			f.setDrop("DELETE /bots/bot-1/media/uploads/receipt-1")
			advance(t, c, s, src)
			j = advance(t, c, s, src)
			if published && (j.Phase != "ready" || j.Ready == nil || f.count("DELETE /bots/bot-1/media/uploads/receipt-1") != 0) {
				t.Fatal("cancel hid or removed a published original")
			}
			if !published && (j.Phase != "canceled" || j.Ready != nil) {
				t.Fatal("cancel response loss not reconciled")
			}
		})
	}
}

func TestStrictTransportAndBoundedDiagnostics(t *testing.T) {
	auth := func(context.Context) (string, error) { return syntheticAuth, nil }
	for _, u := range []string{"http://[::1]", "https://user:secret@example.invalid", "https://example.invalid?token=secret", "https://example.invalid#secret"} {
		if _, err := New(u, nil, auth); err == nil {
			t.Fatalf("accepted unsafe URL: %s", u)
		}
	}
	insecure := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // Negative test: the constructor must reject this before any request.
	if _, err := New("https://example.invalid", &http.Client{Transport: &http.Transport{TLSClientConfig: insecure}}, auth); err == nil {
		t.Fatal("accepted insecure TLS")
	}
	for _, id := range []string{"../outside", `a\b`, "a%2fb", "a?token=x", "a:b", ""} {
		if validID(id) {
			t.Fatal("path accepted")
		}
	}
	hits := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer target.Close()
	for _, mode := range []string{"redirect", "large-json", "private-error", "untrusted-cert"} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
				case "large-json":
					_, _ = w.Write(bytes.Repeat([]byte(" "), maxJSONBytes+1))
				case "private-error":
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("PRIVATE_TEXT_AND_TOKEN"))
				}
			}))
			defer s.Close()
			h := s.Client()
			if mode == "untrusted-cert" {
				h = nil
			}
			c, err := New(s.URL, h, auth)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Limits(t.Context(), "bot-1")
			if err == nil || strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), s.URL) {
				t.Fatal("unbounded or private diagnostic")
			}
		})
	}
	if hits != 0 {
		t.Fatal("credentials followed redirect")
	}
}

func TestDownloadResumeAndValidation(t *testing.T) {
	data := []byte("0123456789binary\xff\x00")
	for _, mode := range []string{"ok", "wrong-prefix", "full-on-resume", "wrong-range", "wrong-etag", "truncated", "revoked", "disk-full", "checkpoint-failure"} {
		t.Run(mode, func(t *testing.T) {
			hash := digest(data)
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "bytes=4-" || r.Header.Get("If-Range") != `"`+hash+`"` || r.Header.Get("Authorization") != syntheticAuth {
					t.Error("wrong resume headers")
				}
				w.Header().Set("ETag", `"`+hash+`"`)
				w.Header().Set("Content-Length", strconv.Itoa(len(data)-4))
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 4-%d/%d", len(data)-1, len(data)))
				status := 206
				switch mode {
				case "full-on-resume":
					status = 200
				case "wrong-range":
					w.Header().Set("Content-Range", "bytes 5-6/7")
				case "wrong-etag":
					w.Header().Set("ETag", `"different"`)
				case "revoked":
					status = 403
				}
				w.WriteHeader(status)
				if mode == "truncated" {
					_, _ = w.Write(data[4:6])
					return
				}
				_, _ = w.Write(data[4:])
			}))
			defer s.Close()
			c, err := New(s.URL, s.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.CreateTemp(t.TempDir(), "part")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			prefix := append([]byte(nil), data[:4]...)
			if mode == "wrong-prefix" {
				prefix[0] = '!'
			}
			_, _ = file.Write(append(prefix, []byte("unacknowledged-tail")...))
			var staging StagingFile = file
			if mode == "disk-full" {
				staging = failingFile{file}
			}
			var saved int64
			err = c.Download(t.Context(), "bot-1", hash, int64(len(data)), 4, staging, func(n int64) error {
				if mode == "checkpoint-failure" {
					return errors.New("synthetic disk full")
				}
				saved = n
				return nil
			})
			if mode == "ok" {
				if err != nil || saved != int64(len(data)) {
					t.Fatal(err)
				}
				_, _ = file.Seek(0, 0)
				got, _ := io.ReadAll(file)
				if !bytes.Equal(got, data) {
					t.Fatal("unacknowledged tail not replaced")
				}
			} else if err == nil {
				t.Fatal("unsafe download accepted")
			}
		})
	}
}

type failingFile struct{ *os.File }

func (failingFile) Write([]byte) (int, error) { return 0, errors.New("synthetic ENOSPC") }

func TestVerifyBoundsAndCancellation(t *testing.T) {
	p := []byte("hash me")
	spec := newJob(p).Spec
	for _, content := range [][]byte{p[:3], append(append([]byte(nil), p...), '!'), []byte("Hash me")} {
		if err := Verify(t.Context(), bytes.NewReader(content), spec); !errors.Is(err, ErrIntegrity) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Verify(ctx, bytes.NewReader(p), spec); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func fixtureReceipt(f *fixture) Receipt { r, _ := f.current(); return r }
func fixtureReady(f *fixture) []byte    { _, b := f.current(); return b }
