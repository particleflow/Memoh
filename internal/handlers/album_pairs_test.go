package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/albumpairs"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
)

const (
	albumPairUser = "11111111-1111-1111-1111-111111111111"
	albumPairBot  = "22222222-2222-2222-2222-222222222222"
)

type albumPairHTTPStore struct {
	rows []dbstore.AlbumPairRecord
}

func (s *albumPairHTTPStore) ListAlbumPairs(_ context.Context, userID string) ([]dbstore.AlbumPairRecord, error) {
	var out []dbstore.AlbumPairRecord
	for _, row := range s.rows {
		if row.UserID == userID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *albumPairHTTPStore) UpsertAlbumPair(_ context.Context, record dbstore.AlbumPairRecord) (dbstore.AlbumPairRecord, error) {
	for i := range s.rows {
		row := &s.rows[i]
		if row.UserID == record.UserID && row.BotID == record.BotID && row.State == albumpairs.StateActive && row.PairKey != record.PairKey {
			row.State = albumpairs.StateRevoked
		}
	}
	for i := range s.rows {
		row := &s.rows[i]
		if row.UserID == record.UserID && row.PairKey == record.PairKey {
			row.BotID, row.State = record.BotID, albumpairs.StateActive
			return *row, nil
		}
	}
	record.State = albumpairs.StateActive
	record.CreatedAt = time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	record.UpdatedAt = record.CreatedAt
	s.rows = append(s.rows, record)
	return record, nil
}

func (s *albumPairHTTPStore) RevokeAlbumPair(_ context.Context, userID, pairKey string) (dbstore.AlbumPairRecord, error) {
	for i := range s.rows {
		row := &s.rows[i]
		if row.UserID == userID && row.PairKey == pairKey {
			row.State = albumpairs.StateRevoked
			return *row, nil
		}
	}
	return dbstore.AlbumPairRecord{}, db.ErrNotFound
}

func (s *albumPairHTTPStore) ResolveActiveAlbumPair(_ context.Context, userID, botID string) (dbstore.AlbumPairRecord, error) {
	for _, row := range s.rows {
		if row.UserID == userID && row.BotID == botID && row.State == albumpairs.StateActive {
			return row, nil
		}
	}
	return dbstore.AlbumPairRecord{}, db.ErrNotFound
}

func newAlbumPairTestServer(t *testing.T) (*echo.Echo, *albumpairs.Service) {
	t.Helper()
	const secret = "synthetic-album-pair-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	service := albumpairs.NewService(nil, &albumPairHTTPStore{})
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	e.Use(auth.JWTMiddleware(secret, func(echo.Context) bool { return false }))
	NewAlbumPairHandler(nil, service).Register(e)
	return e, service
}

func albumPairToken(t *testing.T) string {
	t.Helper()
	const secret = "synthetic-album-pair-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	token, _, err := auth.GenerateInternalToken(albumPairUser, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func albumPairCall(e *echo.Echo, method, path, token, body string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestAlbumPairHTTPRequiresTheOwnersToken(t *testing.T) {
	e, _ := newAlbumPairTestServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/users/me/album-pairs", ""},
		{http.MethodPut, "/users/me/album-pairs/red-jade", `{"bot_id":"` + albumPairBot + `"}`},
		{http.MethodDelete, "/users/me/album-pairs/red-jade", ""},
	} {
		if rec := albumPairCall(e, tc.method, tc.path, "", tc.body); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s anonymous = %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestAlbumPairHTTPDesignateListAndUnbind(t *testing.T) {
	e, _ := newAlbumPairTestServer(t)
	token := albumPairToken(t)

	put := albumPairCall(e, http.MethodPut, "/users/me/album-pairs/red-jade", token, `{"bot_id":"`+albumPairBot+`"}`)
	if put.Code != http.StatusOK {
		t.Fatalf("designate = %d %s", put.Code, put.Body.String())
	}
	var pair albumpairs.Pair
	if err := json.Unmarshal(put.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode designate: %v", err)
	}
	if pair.PairKey != "red-jade" || pair.BotID != albumPairBot || pair.State != albumpairs.StateActive {
		t.Fatalf("designated pair = %+v", pair)
	}

	list := albumPairCall(e, http.MethodGet, "/users/me/album-pairs", token, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}
	var body AlbumPairResponse
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(body.Pairs) != 1 || body.Pairs[0].PairKey != "red-jade" {
		t.Fatalf("listed pairs = %+v", body.Pairs)
	}

	unbind := albumPairCall(e, http.MethodDelete, "/users/me/album-pairs/red-jade", token, "")
	if unbind.Code != http.StatusOK {
		t.Fatalf("unbind = %d %s", unbind.Code, unbind.Body.String())
	}
	if err := json.Unmarshal(unbind.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode unbind: %v", err)
	}
	if pair.State != albumpairs.StateRevoked {
		t.Fatalf("unbound pair state = %q", pair.State)
	}
	// The row and its label stay readable after the unbind.
	if after := albumPairCall(e, http.MethodGet, "/users/me/album-pairs", token, ""); after.Code != http.StatusOK {
		t.Fatalf("list after unbind = %d %s", after.Code, after.Body.String())
	} else if err := json.Unmarshal(after.Body.Bytes(), &body); err != nil || len(body.Pairs) != 1 {
		t.Fatalf("pairs after unbind = %+v, %v", body.Pairs, err)
	}
	// A second unbind is idempotent, and an unknown label is not found.
	if rec := albumPairCall(e, http.MethodDelete, "/users/me/album-pairs/red-jade", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("repeat unbind = %d %s", rec.Code, rec.Body.String())
	}
	if rec := albumPairCall(e, http.MethodDelete, "/users/me/album-pairs/absent", token, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unbind unknown = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAlbumPairHTTPRejectsMalformedRequests(t *testing.T) {
	e, _ := newAlbumPairTestServer(t)
	token := albumPairToken(t)

	for _, tc := range []struct{ name, path, body string }{
		{"missing bot", "/users/me/album-pairs/red-jade", `{}`},
		{"malformed bot", "/users/me/album-pairs/red-jade", `{"bot_id":"not-a-uuid"}`},
		{"unknown field", "/users/me/album-pairs/red-jade", `{"bot_id":"` + albumPairBot + `","album_uri":"file://media/x"}`},
		{"invalid label", "/users/me/album-pairs/bad%20label", `{"bot_id":"` + albumPairBot + `"}`},
	} {
		if rec := albumPairCall(e, http.MethodPut, tc.path, token, tc.body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

func TestAlbumPairHTTPReportsAnUnconfiguredService(t *testing.T) {
	const secret = "synthetic-album-pair-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	e.Use(auth.JWTMiddleware(secret, func(echo.Context) bool { return false }))
	NewAlbumPairHandler(nil, nil).Register(e)
	if rec := albumPairCall(e, http.MethodGet, "/users/me/album-pairs", albumPairToken(t), ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured list = %d %s", rec.Code, rec.Body.String())
	}
}
