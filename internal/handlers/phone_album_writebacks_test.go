package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/albumwriteback"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
)

const (
	albumTestUser = "11111111-1111-1111-1111-111111111111"
	albumTestBot  = "22222222-2222-2222-2222-222222222222"
)

type albumHTTPStore struct {
	rows []dbstore.PhoneAlbumWritebackRecord
}

func (s *albumHTTPStore) EnqueuePhoneAlbumWriteback(_ context.Context, record dbstore.PhoneAlbumWritebackRecord) (dbstore.PhoneAlbumWritebackRecord, error) {
	for i, row := range s.rows {
		if row.UserID == record.UserID && row.PairKey == record.PairKey && row.ContentHash == record.ContentHash {
			record.State = albumwriteback.StatePending
			s.rows[i] = record
			return record, nil
		}
	}
	record.State = albumwriteback.StatePending
	s.rows = append(s.rows, record)
	return record, nil
}

func (s *albumHTTPStore) ListPendingPhoneAlbumWritebacks(_ context.Context, userID string) ([]dbstore.PhoneAlbumWritebackRecord, error) {
	var out []dbstore.PhoneAlbumWritebackRecord
	for _, row := range s.rows {
		if row.UserID == userID && row.State == albumwriteback.StatePending {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *albumHTTPStore) MarkPhoneAlbumWritebackWritten(_ context.Context, userID, pairKey, contentHash string) (dbstore.PhoneAlbumWritebackRecord, error) {
	for i, row := range s.rows {
		if row.UserID == userID && row.PairKey == pairKey && row.ContentHash == contentHash {
			s.rows[i].State = albumwriteback.StateWritten
			return s.rows[i], nil
		}
	}
	return dbstore.PhoneAlbumWritebackRecord{}, db.ErrNotFound
}

type albumHTTPAssets struct{ known map[string]media.Asset }

func (a albumHTTPAssets) Stat(_ context.Context, botID, contentHash string) (media.Asset, error) {
	asset, ok := a.known[contentHash]
	if !ok || asset.BotID != botID {
		return media.Asset{}, media.ErrAssetNotFound
	}
	return asset, nil
}

func albumTestHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func newAlbumWritebackTestServer(t *testing.T) (*echo.Echo, *albumwriteback.Service) {
	t.Helper()
	const secret = "synthetic-album-http-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	hash := albumTestHash("album")
	service := albumwriteback.NewService(nil, &albumHTTPStore{}, albumHTTPAssets{known: map[string]media.Asset{
		hash: {ContentHash: hash, BotID: albumTestBot, Mime: "image/jpeg", SizeBytes: 42, StorageKey: hash[:2] + "/" + hash + ".jpg"},
	}})
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	e.Use(auth.JWTMiddleware(secret, func(echo.Context) bool { return false }))
	NewPhoneAlbumWritebackHandler(nil, service).Register(e)
	return e, service
}

func TestPhoneAlbumWritebackHTTPRequiresOwnerToken(t *testing.T) {
	e, _ := newAlbumWritebackTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/users/me/album-writebacks", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous pull = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPhoneAlbumWritebackHTTPPullAndMark(t *testing.T) {
	const secret = "synthetic-album-http-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	e, service := newAlbumWritebackTestServer(t)
	hash := albumTestHash("album")
	if _, err := service.Enqueue(context.Background(), albumTestUser, albumTestBot, albumwriteback.EnqueueInput{
		PairKey:       "processed-default",
		ContentHashes: []string{hash},
		Name:          "结果.jpg",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	token, _, err := auth.GenerateInternalToken(albumTestUser, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	pull := func() albumwriteback.PendingResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/users/me/album-writebacks?state=pending", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("pull = %d %s", rec.Code, rec.Body.String())
		}
		var body albumwriteback.PendingResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode pull: %v", err)
		}
		return body
	}

	body := pull()
	if len(body.Items) != 1 || body.Items[0].ContentHash != hash || body.Items[0].PairKey != "processed-default" {
		t.Fatalf("pending body = %+v", body.Items)
	}
	if body.Items[0].Name != "结果.jpg" || body.Items[0].Mime != "image/jpeg" || body.Items[0].BotID != albumTestBot {
		t.Fatalf("pending item fields = %+v", body.Items[0])
	}

	post := func(payload string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/users/me/album-writebacks/written", bytes.NewBufferString(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	if rec := post(`{"pair_key":"processed-default","content_hash":"` + hash + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("mark written = %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(`{"pair_key":"processed-default","content_hash":"` + hash + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("repeat mark written = %d %s", rec.Code, rec.Body.String())
	}
	if items := pull().Items; len(items) != 0 {
		t.Fatalf("written item still pending: %+v", items)
	}
	if rec := post(`{"pair_key":"processed-default","content_hash":"` + albumTestHash("other") + `"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown item mark = %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(`{"pair_key":"processed-default","content_hash":"` + hash + `","extra":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field mark = %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(`{"pair_key":"","content_hash":"` + hash + `"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty pairing mark = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPhoneAlbumWritebackHTTPRejectsUnknownStateFilter(t *testing.T) {
	const secret = "synthetic-album-http-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	e, _ := newAlbumWritebackTestServer(t)
	token, _, err := auth.GenerateInternalToken(albumTestUser, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/users/me/album-writebacks?state=written", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported state = %d %s", rec.Code, rec.Body.String())
	}
}
