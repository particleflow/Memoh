package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func mediaTestHTTP(t *testing.T, provider storage.Provider, limit int64, role string) (*echo.Echo, *media.Service) {
	t.Helper()
	service := media.NewService(nil, provider)
	service.SetRequirePersistence(true)
	service.SetMaxUploadBytes(limit)
	queries := newInvocationTestQueries()
	queries.permissions = []byte(`[]`)
	h := &MessageHandler{
		mediaService: service, botService: bots.NewService(nil, queries),
		accountService: newTestAdminAccountService(role),
	}
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	if role != "" {
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				c.Set("user", &jwt.Token{Valid: true, Claims: jwt.MapClaims{
					"sub": invocationTestOtherUser, "user_id": invocationTestOtherUser,
				}})
				return next(c)
			}
		})
	}
	h.Register(e)
	return e, service
}

// Generates arbitrary-sized payloads without allocating the file in memory.
type uploadZeros struct{}

func (uploadZeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func streamingUploadRequest(t *testing.T, size int64) *http.Request {
	t.Helper()
	reader, writer := io.Pipe()
	mw := multipart.NewWriter(writer)
	req := httptest.NewRequest(http.MethodPost, "/bots/"+invocationTestBotID+"/media", reader)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	done := make(chan struct{})
	go func() {
		defer close(done)
		part, err := mw.CreateFormFile("file", "synthetic.bin")
		if err == nil {
			_, err = io.CopyN(part, uploadZeros{}, size)
		}
		if err == nil {
			err = mw.Close()
		}
		_ = writer.CloseWithError(err)
	}()
	t.Cleanup(func() { _ = reader.Close(); <-done })
	return req
}

func TestMediaHTTPUploadConfiguredAbove200MiB(t *testing.T) {
	const size = 201 * 1024 * 1024
	const limit = 256 * 1024 * 1024
	spool := t.TempDir()
	t.Setenv("TMPDIR", spool)
	root := t.TempDir()
	e, service := mediaTestHTTP(t, localfs.NewOriginals(root), limit, "admin")
	limits := httptest.NewRecorder()
	e.ServeHTTP(limits, httptest.NewRequest(http.MethodGet, "/bots/"+invocationTestBotID+"/media/limits", nil))
	var policy MediaUploadLimitsResponse
	if err := json.Unmarshal(limits.Body.Bytes(), &policy); err != nil || policy.MaxUploadBytes != limit {
		t.Fatalf("configured policy: %s, %v", limits.Body, err)
	}
	rec := httptest.NewRecorder()
	req := streamingUploadRequest(t, size)
	e.ServeHTTP(rec, req)
	_ = req.Body.Close()
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var uploaded MediaUploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &uploaded); err != nil || uploaded.SizeBytes != size {
		t.Fatalf("upload reference: %+v, %v", uploaded, err)
	}
	reader, _, err := service.Open(context.Background(), invocationTestBotID, uploaded.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	n, err := io.Copy(hash, reader)
	_ = reader.Close()
	if err != nil || n != size || hex.EncodeToString(hash.Sum(nil)) != uploaded.ContentHash {
		t.Fatalf("stored original: bytes=%d, err=%v", n, err)
	}
	assertNoMediaSpools(t, spool)
	// The upload handler has no session/model dependency. A subsequent hash
	// admission resolves the original without an inline or workspace fallback.
	h := &LocalChannelHandler{mediaService: service}
	result, err := h.ingestWSInboundAttachments(context.Background(), invocationTestBotID, []turn.Attachment{{
		Type: "file", ContentHash: uploaded.ContentHash,
	}})
	if err != nil || len(result) != 1 || result[0].Base64 != "" || result[0].ContentHash != uploaded.ContentHash {
		t.Fatalf("hash admission: %+v %v", result, err)
	}
}

func assertNoMediaSpools(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "memoh-media-*"))
	if err != nil || len(matches) > 0 {
		t.Fatalf("temporary media remains: %v %v", matches, err)
	}
}

func TestMediaHTTPStreamingLimitAndAdmission(t *testing.T) {
	for _, tc := range []struct {
		name        string
		limit, size int64
		status      int
	}{
		{"exact", 1024, 1024, http.StatusCreated},
		{"over configured", 1024, 1025, http.StatusRequestEntityTooLarge},
		{"over default 200MiB", 0, media.MaxAssetBytes + 1, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spool := t.TempDir()
			t.Setenv("TMPDIR", spool)
			root := t.TempDir()
			e, service := mediaTestHTTP(t, localfs.NewOriginals(root), tc.limit, "admin")
			rec := httptest.NewRecorder()
			req := streamingUploadRequest(t, tc.size)
			e.ServeHTTP(rec, req)
			_ = req.Body.Close()
			if rec.Code != tc.status {
				t.Fatalf("status=%d: %s", rec.Code, rec.Body)
			}
			assertNoMediaSpools(t, spool)
			if tc.status == http.StatusCreated {
				return
			}
			var problem server.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil || problem.Code != "http.payload_too_large" {
				t.Fatalf("overlimit problem: %s %v", rec.Body, err)
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("rejected upload persisted an original")
			}
			admission := &wsMessageAdmission{
				handler: &LocalChannelHandler{mediaService: service}, botID: invocationTestBotID,
				attachments: []turn.Attachment{{Type: "file", ContentHash: strings.Repeat("a", 64)}},
			}
			view, err := admission.build(context.Background(), sessionruntime.RunHandle{})
			if err == nil || view.RequestUserTurn != nil || admission.attachmentsPrepared {
				t.Fatal("incomplete upload admitted a model request")
			}
		})
	}
}

type failOnRead struct{ read bool }

func (r *failOnRead) Read([]byte) (int, error) {
	r.read = true
	return 0, errors.New("PRIVATE_BODY_DIAGNOSTIC")
}

type cancelUploadBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	read   int
}

func (r *cancelUploadBody) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.read += n
	if r.read >= 16*1024 {
		r.cancel()
	}
	return n, err
}

func TestMediaHTTPCancelPartiallyReceivedUpload(t *testing.T) {
	spool := t.TempDir()
	t.Setenv("TMPDIR", spool)
	root := t.TempDir()
	e, _ := mediaTestHTTP(t, localfs.NewOriginals(root), 1024*1024, "admin")
	req := streamingUploadRequest(t, 64*1024)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	req.Body = &cancelUploadBody{ReadCloser: req.Body, cancel: cancel}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	_ = req.Body.Close()
	if rec.Code != 499 {
		t.Fatalf("partially received cancellation: %d %s", rec.Code, rec.Body)
	}
	assertNoMediaSpools(t, spool)
	entries, _ := os.ReadDir(root)
	if len(entries) > 0 {
		t.Fatal("canceled upload published an original")
	}
}

func TestMediaHTTPHashAdmissionRemainsBotScopedWithoutOriginalsMode(t *testing.T) {
	service := media.NewService(nil, localfs.New(t.TempDir()))
	asset, err := service.Ingest(context.Background(), media.IngestInput{
		BotID: invocationTestBotID, Mime: "text/plain", Reader: strings.NewReader("synthetic legacy"),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &LocalChannelHandler{mediaService: service}
	attachments := []turn.Attachment{{Type: "file", ContentHash: asset.ContentHash}}
	if _, err := h.ingestWSInboundAttachments(context.Background(), invocationTestBotID, attachments); err != nil {
		t.Fatalf("legacy hash no longer accepted: %v", err)
	}
	if _, err := h.ingestWSInboundAttachments(context.Background(), "another-bot", attachments); err == nil {
		t.Fatal("other Bot reference bypassed admission")
	}
}

func TestMediaHTTPAuthorizationBeforeBody(t *testing.T) {
	for _, tc := range []struct {
		role   string
		status int
	}{{"", 401}, {"user", 403}} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			e, _ := mediaTestHTTP(t, localfs.NewOriginals(t.TempDir()), 1024, tc.role)
			body := &failOnRead{}
			req := httptest.NewRequest(http.MethodPost, "/bots/"+invocationTestBotID+"/media", body)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tc.status || body.read {
				t.Fatalf("status=%d bodyRead=%v", rec.Code, body.read)
			}
			rec = httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bots/"+invocationTestBotID+"/media/limits", nil))
			if rec.Code != tc.status {
				t.Fatalf("limits bypassed authorization: %d", rec.Code)
			}
		})
	}
}

func TestMediaHTTPFailedUploadCleansSpool(t *testing.T) {
	for _, kind := range []string{"truncated", "extra part", "empty", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			spool := t.TempDir()
			t.Setenv("TMPDIR", spool)
			root := t.TempDir()
			e, _ := mediaTestHTTP(t, localfs.NewOriginals(root), 1024, "admin")
			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			part, _ := mw.CreateFormFile("file", "synthetic.txt")
			if kind != "empty" {
				_, _ = io.WriteString(part, "synthetic data")
			}
			if kind == "extra part" {
				_, _ = mw.CreateFormFile("file", "second.txt")
			}
			if kind != "truncated" {
				_ = mw.Close()
			}
			req := httptest.NewRequest(http.MethodPost, "/bots/"+invocationTestBotID+"/media", &body)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			want := http.StatusBadRequest
			if kind == "canceled" {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
				want = 499
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Fatalf("status=%d: %s", rec.Code, rec.Body)
			}
			assertNoMediaSpools(t, spool)
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("failed upload persisted bytes")
			}
		})
	}
}

type uploadStorageFailure struct{ storage.Provider }

func (uploadStorageFailure) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}

func (uploadStorageFailure) Put(context.Context, string, io.Reader) error {
	return errors.New("PRIVATE_STORAGE_DIAGNOSTIC")
}

func TestMediaHTTPUploadStorageFailure(t *testing.T) {
	spool := t.TempDir()
	t.Setenv("TMPDIR", spool)
	e, _ := mediaTestHTTP(t, uploadStorageFailure{}, 1024, "admin")
	req := streamingUploadRequest(t, 10)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	_ = req.Body.Close()
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "PRIVATE_STORAGE_DIAGNOSTIC") {
		t.Fatalf("storage error leaked or misclassified: %d %s", rec.Code, rec.Body)
	}
	assertNoMediaSpools(t, spool)
}
