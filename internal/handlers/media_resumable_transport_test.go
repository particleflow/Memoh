package handlers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type revocableUploadQueries struct {
	*sessionInvocationQueries
	revoked atomic.Bool
}

func (*revocableUploadQueries) GetBotByID(_ context.Context, id pgtype.UUID) (sqlc.GetBotByIDRow, error) {
	return testBotRow(id.String(), map[string]any{}), nil
}

func (q *revocableUploadQueries) ListBotUserGrantsForUser(_ context.Context, _ sqlc.ListBotUserGrantsForUserParams) ([]sqlc.ListBotUserGrantsForUserRow, error) {
	permissions := []byte(`["chat"]`)
	if q.revoked.Load() {
		permissions = []byte(`[]`)
	}
	return []sqlc.ListBotUserGrantsForUserRow{{Permissions: permissions}}, nil
}

type captureUploadHandler struct {
	handler *MessageHandler
	echo    *echo.Echo
}

func (h *captureUploadHandler) Register(e *echo.Echo) { h.echo = e; h.handler.Register(e) }

type uploadHTTPFixture struct {
	server  *httptest.Server
	handler *MessageHandler
	queries *revocableUploadQueries
	token   string
	base    string
	hash    string
	payload []byte
}

func newUploadHTTPFixture(t *testing.T) *uploadHTTPFixture {
	t.Helper()
	root := t.TempDir()
	service := media.NewService(nil, localfs.NewOriginals(root))
	service.SetRequirePersistence(true)
	if err := service.EnableUploads(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Uploads().Close() })
	q := &revocableUploadQueries{sessionInvocationQueries: newInvocationTestQueries()}
	h := &MessageHandler{mediaService: service, botService: bots.NewService(nil, q), accountService: newTestAdminAccountService("user"), uploadReadTimeout: 250 * time.Millisecond}
	capture := &captureUploadHandler{handler: h}
	_ = server.NewServer(slog.New(slog.DiscardHandler), ":0", "synthetic-http-secret", capture)
	transport := httptest.NewServer(capture.echo)
	t.Cleanup(transport.Close)
	token, _, err := auth.GenerateToken(invocationTestOtherUser, "synthetic-http-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("12345678")
	hash := sha256.Sum256(payload)
	return &uploadHTTPFixture{server: transport, handler: h, queries: q, token: token, base: "/bots/" + invocationTestBotID + "/media/uploads", hash: hex.EncodeToString(hash[:]), payload: payload}
}

func (f *uploadHTTPFixture) request(t *testing.T, method, path string, data []byte, want int) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.server.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPatch {
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Upload-Offset", "0")
		req.Header.Set("Upload-Length", strconv.Itoa(len(f.payload)))
		req.Header.Set("Upload-SHA256", f.hash)
	}
	response, err := f.server.Client().Do(req) //nolint:gosec // G704: URL is the test-owned httptest server plus fixed test routes.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, body)
	}
	return body
}

func (f *uploadHTTPFixture) create(t *testing.T) media.Upload {
	t.Helper()
	input, err := json.Marshal(media.UploadInput{SizeBytes: int64(len(f.payload)), SHA256: f.hash, Mime: "application/octet-stream"})
	if err != nil {
		t.Fatal(err)
	}
	body := f.request(t, http.MethodPost, f.base, input, http.StatusCreated)
	var upload media.Upload
	if err := json.Unmarshal(body, &upload); err != nil {
		t.Fatal(err)
	}
	return upload
}

func TestResumableHTTPRevocationAndCrossBot(t *testing.T) {
	f := newUploadHTTPFixture(t)
	u := f.create(t)
	path := f.base + "/" + u.ID
	otherBot := "44444444-4444-4444-4444-444444444444"
	crossPath := strings.Replace(path, invocationTestBotID, otherBot, 1)
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodPost, http.MethodDelete} {
		target := crossPath
		if method == http.MethodPost {
			target += "/complete"
		}
		f.request(t, method, target, f.payload, http.StatusNotFound)
	}
	f.queries.revoked.Store(true)
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodPost, http.MethodDelete} {
		target := path
		if method == http.MethodPost {
			target += "/complete"
		}
		f.request(t, method, target, f.payload, http.StatusForbidden)
	}
	f.queries.revoked.Store(false)
	f.request(t, http.MethodPatch, path, f.payload, http.StatusOK)
	f.request(t, http.MethodPost, path+"/complete", nil, http.StatusOK)
}

func TestResumableHTTPStalledBodyDeadlineReleasesLock(t *testing.T) {
	f := newUploadHTTPFixture(t)
	u := f.create(t)
	path := f.base + "/" + u.ID
	endpoint, err := url.Parse(f.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", endpoint.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(conn, "PATCH %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/octet-stream\r\nContent-Length: 8\r\nUpload-Offset: 0\r\nUpload-Length: 8\r\nUpload-SHA256: %s\r\n\r\n12", path, endpoint.Host, f.token, f.hash)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal("stalled request did not terminate", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode == http.StatusOK || time.Since(started) > 5*time.Second {
		t.Fatal("read deadline ineffective", response.StatusCode, time.Since(started))
	}
	var status media.Upload
	if err := json.Unmarshal(f.request(t, http.MethodGet, path, nil, http.StatusOK), &status); err != nil {
		t.Fatal(err)
	}
	if status.Offset != 0 || status.ContentHash != "" {
		t.Fatal(status)
	}
	f.request(t, http.MethodPatch, path, f.payload, http.StatusOK)
	f.request(t, http.MethodPost, path+"/complete", nil, http.StatusOK)
}

func TestResumableHTTPUnsupportedDeadlineFailsBeforeReading(t *testing.T) {
	root := t.TempDir()
	e, s := mediaTestHTTP(t, localfs.NewOriginals(root), 1024, "admin")
	if err := s.EnableUploads(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Uploads().Close() }()
	data := []byte("a")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	u, err := s.Uploads().Create(t.Context(), invocationTestOtherUser, invocationTestBotID, media.UploadInput{SizeBytes: 1, SHA256: hash, Mime: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	body := &neverReadUploadBody{t: t}
	req := httptest.NewRequest(http.MethodPatch, "/bots/"+invocationTestBotID+"/media/uploads/"+u.ID, body)
	req.Header.Set("Upload-Offset", "0")
	req.Header.Set("Upload-Length", "1")
	req.Header.Set("Upload-SHA256", hash)
	response := httptest.NewRecorder()
	e.ServeHTTP(response, req)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal(response.Code, response.Body)
	}
	got, err := s.Uploads().Get(t.Context(), invocationTestOtherUser, invocationTestBotID, u.ID)
	if err != nil || got.Offset != 0 {
		t.Fatal(got, err)
	}
}

type neverReadUploadBody struct{ t *testing.T }

func (r *neverReadUploadBody) Read([]byte) (int, error) {
	r.t.Fatal("read body without an enforceable deadline")
	return 0, io.EOF
}
