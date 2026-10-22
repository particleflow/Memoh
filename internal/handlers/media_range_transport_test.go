package handlers

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/media"
)

func TestMediaRangeWarmCacheStillRequiresHTTPAuthorization(t *testing.T) {
	f := newUploadHTTPFixture(t)
	data := bytes.Repeat([]byte("synthetic-range-bytes"), 180000)
	a, err := f.handler.mediaService.Ingest(t.Context(), media.IngestInput{BotID: invocationTestBotID, Mime: "application/octet-stream", Reader: bytes.NewReader(data)})
	if err != nil {
		t.Fatal(err)
	}
	path := "/bots/" + invocationTestBotID + "/media/" + a.ContentHash
	request := func(path, token string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Range", "bytes=1048574-1048578")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := f.server.Client().Do(req) //nolint:gosec // G704: the test-owned loopback server and fixed routes only.
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("HTTP %d, expected %d, read error %v", res.StatusCode, want, err)
		}
		if want == http.StatusPartialContent && !bytes.Equal(body, data[1048574:1048579]) {
			t.Fatal("cross-block range bytes differ")
		}
	}
	request(path, f.token, http.StatusPartialContent) // Establish full verification.
	request(path, f.token, http.StatusPartialContent) // Warm cache.
	request(path, "", http.StatusUnauthorized)
	request(strings.Replace(path, invocationTestBotID, "44444444-4444-4444-4444-444444444444", 1), f.token, http.StatusNotFound)
	f.queries.revoked.Store(true)
	request(path, f.token, http.StatusForbidden)
	f.queries.revoked.Store(false)
	request(path, f.token, http.StatusPartialContent)
}
