package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func TestMediaHTTPRangesAndFullIntegrity(t *testing.T) {
	root := t.TempDir()
	e, service := mediaTestHTTP(t, localfs.NewOriginals(root), 0, "admin")
	const payload = "0123456789abcdefghijklmnopqrstuvwxyz"
	asset, err := service.Ingest(context.Background(), media.IngestInput{
		BotID: invocationTestBotID, Mime: "audio/mpeg", Reader: strings.NewReader(payload),
	})
	if err != nil {
		t.Fatal(err)
	}
	url := "/bots/" + invocationTestBotID + "/media/" + asset.ContentHash
	for _, tc := range []struct {
		name, method, byteRange, ifRange, expected string
		status                                     int
	}{
		{"full", "GET", "", "", payload, 200},
		{"prefix", "GET", "bytes=0-3", "", "0123", 206},
		{"seek", "GET", "bytes=10-", "", payload[10:], 206},
		{"suffix", "GET", "bytes=-4", "", "wxyz", 206},
		{"if range matches", "GET", "bytes=0-3", `"` + asset.ContentHash + `"`, "0123", 206},
		{"if range differs", "GET", "bytes=0-3", `"different"`, payload, 200},
		{"unsatisfiable", "GET", "bytes=36-", "", "", 416},
		{"head", "HEAD", "", "", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, url, nil)
			req.Header.Set("Range", tc.byteRange)
			req.Header.Set("If-Range", tc.ifRange)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d: %s", rec.Code, rec.Body)
			}
			if tc.status != 416 && rec.Body.String() != tc.expected {
				t.Fatalf("range bytes=%q, want %q", rec.Body.String(), tc.expected)
			}
			if tc.status == 206 && !strings.HasPrefix(rec.Header().Get("Content-Range"), "bytes ") {
				t.Fatal("missing Content-Range")
			}
			if tc.status == 416 && rec.Header().Get("Content-Range") != "bytes */36" {
				t.Fatal("missing unsatisfiable range size")
			}
			if tc.status != 416 && (rec.Header().Get("Accept-Ranges") != "bytes" || rec.Header().Get("Content-Type") != "audio/mpeg") {
				t.Fatal("missing seek support or MIME")
			}
		})
	}
	// Corrupt bytes OUTSIDE the requested range. Partial reads must still fail
	// full-original verification before sending any partial success response.
	file := filepath.Join(root, invocationTestBotID, filepath.FromSlash(asset.StorageKey))
	if err := os.WriteFile(file, []byte(payload[:35]+"!"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 500 || strings.Contains(rec.Body.String(), root) {
		t.Fatalf("corrupt original: %d %s", rec.Code, rec.Body)
	}
}

type rangeStreamProvider struct {
	*localfs.Originals
	failRead bool
}

func (p rangeStreamProvider) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := p.Originals.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	if p.failRead {
		_ = reader.Close()
		return io.NopCloser(&failOnRead{}), nil
	}
	// Hide Seek to exercise legacy streaming backends.
	return struct {
		io.Reader
		io.Closer
	}{reader, reader}, nil
}

func TestMediaHTTPRangeStreamingBackendAndReadFailure(t *testing.T) {
	for _, failRead := range []bool{false, true} {
		t.Run(strconv.FormatBool(failRead), func(t *testing.T) {
			spool := t.TempDir()
			t.Setenv("TMPDIR", spool)
			root := localfs.NewOriginals(t.TempDir())
			service := media.NewService(nil, root)
			asset, err := service.Ingest(context.Background(), media.IngestInput{
				BotID: invocationTestBotID, Mime: "video/mp4", Reader: strings.NewReader("synthetic video bytes"),
			})
			if err != nil {
				t.Fatal(err)
			}
			e, _ := mediaTestHTTP(t, rangeStreamProvider{root, failRead}, 0, "admin")
			req := httptest.NewRequest(http.MethodGet, "/bots/"+invocationTestBotID+"/media/"+asset.ContentHash, nil)
			req.Header.Set("Range", "bytes=0-8")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			want := 206
			if failRead {
				want = 500
			}
			if rec.Code != want || strings.Contains(rec.Body.String(), "PRIVATE_BODY_DIAGNOSTIC") {
				t.Fatalf("streaming range=%d: %s", rec.Code, rec.Body)
			}
			if !failRead && rec.Body.String() != "synthetic" {
				t.Fatal("incorrect streamed range")
			}
			assertNoMediaSpools(t, spool)
		})
	}
}

type rangeSeekFailure struct {
	*strings.Reader
}

func (rangeSeekFailure) Close() error { return nil }
func (rangeSeekFailure) Seek(int64, int) (int64, error) {
	return 0, errors.New("PRIVATE_SEEK_DIAGNOSTIC")
}

type rangeSeekProvider struct{ storage.Provider }

func (rangeSeekProvider) ListPrefix(_ context.Context, prefix string) ([]string, error) {
	return []string{prefix + ".mp4"}, nil
}

func (rangeSeekProvider) Open(context.Context, string) (io.ReadCloser, error) {
	return rangeSeekFailure{strings.NewReader("synthetic")}, nil
}

func TestMediaHTTPRangeSeekFailureAndAuthorization(t *testing.T) {
	for _, tc := range []struct {
		role   string
		status int
	}{{"admin", 500}, {"user", 403}, {"", 401}} {
		e, _ := mediaTestHTTP(t, rangeSeekProvider{}, 0, tc.role)
		req := httptest.NewRequest(http.MethodGet, "/bots/"+invocationTestBotID+"/media/"+strings.Repeat("a", 64), nil)
		req.Header.Set("Range", "bytes=0-1")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != tc.status || strings.Contains(rec.Body.String(), "PRIVATE_SEEK_DIAGNOSTIC") {
			t.Fatalf("range authorization/seek failure: %d %s", rec.Code, rec.Body)
		}
	}
}
