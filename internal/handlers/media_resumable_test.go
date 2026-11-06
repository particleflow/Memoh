package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func TestResumableHTTPContract(t *testing.T) {
	root := t.TempDir()
	e, s := mediaTestHTTP(t, localfs.NewOriginals(root), 1024, "admin")
	if err := s.EnableUploads(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Uploads().Close() }()
	base := "/bots/" + invocationTestBotID + "/media/uploads"
	data := []byte("synthetic attachment")
	hash := sha256.Sum256(data)
	sha := hex.EncodeToString(hash[:])
	request := func(method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(uploadDeadlineRecorder{rec}, req)
		if strings.Contains(rec.Body.String(), root) {
			t.Fatal("physical path exposed")
		}
		return rec
	}
	input, _ := json.Marshal(media.UploadInput{SizeBytes: int64(len(data)), SHA256: sha, Mime: "text/plain"})
	rec := request(http.MethodPost, base, input, nil)
	if rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body)
	}
	var upload media.Upload
	if err := json.Unmarshal(rec.Body.Bytes(), &upload); err != nil {
		t.Fatal(err)
	}
	path := base + "/" + upload.ID
	if rec := request(http.MethodPost, path+"/complete", nil, nil); rec.Code != 409 {
		t.Fatal(rec.Code, rec.Body)
	}
	headers := map[string]string{"Content-Type": "application/octet-stream", "Upload-Offset": "0", "Upload-Length": strconv.Itoa(len(data)), "Upload-SHA256": sha}
	rec = request(http.MethodPatch, path, data, headers)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "content_hash") {
		t.Fatal("ready reference before complete")
	}
	if rec := request(http.MethodPatch, path, data, headers); rec.Code != 409 {
		t.Fatal(rec.Code, rec.Body)
	}
	rec = request(http.MethodGet, path, nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"offset":20`) {
		t.Fatal(rec.Code, rec.Body)
	}
	ready := request(http.MethodPost, path+"/complete", nil, nil)
	if ready.Code != 200 || !strings.Contains(ready.Body.String(), sha) {
		t.Fatal(ready.Code, ready.Body)
	}
	again := request(http.MethodPost, path+"/complete", nil, nil)
	if again.Code != 200 || again.Body.String() != ready.Body.String() {
		t.Fatal(again.Code, again.Body)
	}
	if rec := request(http.MethodDelete, path, nil, nil); rec.Code != 409 {
		t.Fatal(rec.Code, rec.Body)
	}
	// Route permission gates apply to every upload operation, not just creation.
	for _, role := range []string{"", "user"} {
		unauthorized, _ := mediaTestHTTP(t, localfs.NewOriginals(root), 1024, role)
		for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodPost, http.MethodDelete} {
			target := path
			if method == http.MethodPost {
				target += "/complete"
			}
			rec := httptest.NewRecorder()
			unauthorized.ServeHTTP(rec, httptest.NewRequest(method, target, bytes.NewReader(data)))
			want := http.StatusForbidden
			if role == "" {
				want = http.StatusUnauthorized
			}
			if rec.Code != want {
				t.Fatalf("%s %s: %d %s", role, method, rec.Code, rec.Body)
			}
		}
	}
	// A different identity with Bot access still cannot operate this receipt.
	other, err := s.Uploads().Create(t.Context(), "different-user", invocationTestBotID, media.UploadInput{SizeBytes: 1, SHA256: sha, Mime: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodPost, http.MethodDelete} {
		target := base + "/" + other.ID
		if method == http.MethodPost {
			target += "/complete"
		}
		rec := request(method, target, data, headers)
		if rec.Code != 404 {
			t.Fatalf("owner isolation %s: %d %s", method, rec.Code, rec.Body)
		}
	}
	rec = request(http.MethodGet, "/bots/"+invocationTestBotID+"/media/"+sha, nil, nil)
	if rec.Code != 200 || rec.Body.String() != string(data) {
		t.Fatal(rec.Code, rec.Body)
	}
}

func TestResumableHTTPDisabledAndLimits(t *testing.T) {
	e, _ := mediaTestHTTP(t, localfs.NewOriginals(t.TempDir()), 1024, "admin")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/bots/"+invocationTestBotID+"/media/uploads", strings.NewReader(`{}`)))
	if rec.Code != 503 {
		t.Fatal(rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bots/"+invocationTestBotID+"/media/limits", nil))
	var limits MediaUploadLimitsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &limits); err != nil || limits.ResumableEnabled || limits.MaxUploadBytes != 1024 || limits.ChunkMaxBytes != media.UploadChunkBytes {
		t.Fatal(limits, err)
	}
}

// These recorder tests cover the handler contract. Real transport deadlines are
// tested separately through NewServer and a stalled TCP request body.
type uploadDeadlineRecorder struct{ *httptest.ResponseRecorder }

func (uploadDeadlineRecorder) SetReadDeadline(time.Time) error { return nil }
