package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
)

const (
	authTestUserID    = "11111111-1111-4111-8111-111111111111"
	authTestUserOther = "33333333-3333-4333-8333-333333333333"
	authTestSecret    = "auth-handler-test-secret" //nolint:gosec // test fixture, not a real credential
	authTestPassword  = "secret-123"
	authTestNewPass   = "new-secret-456"
)

// memAuthSessions is an in-memory AuthSessionStore that mirrors the postgres
// semantics the handler depends on: replace-then-insert per device, and
// injectable infrastructure failures.
type memAuthSessions struct {
	mu       sync.Mutex
	rows     map[string]dbstore.AuthSessionRecord
	next     int
	getErr   error
	touchErr error
}

func newMemAuthSessions() *memAuthSessions {
	return &memAuthSessions{rows: map[string]dbstore.AuthSessionRecord{}}
}

func (s *memAuthSessions) row(id string) (dbstore.AuthSessionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	return row, ok
}

func (s *memAuthSessions) liveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, row := range s.rows {
		if row.RevokedAt.IsZero() {
			count++
		}
	}
	return count
}

func (s *memAuthSessions) CreateAuthSession(_ context.Context, input dbstore.CreateAuthSessionInput) (dbstore.AuthSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	row := dbstore.AuthSessionRecord{
		ID:          fmt.Sprintf("%08d-0000-4000-8000-000000000000", s.next),
		UserID:      input.UserID,
		DeviceID:    input.DeviceID,
		DeviceName:  input.DeviceName,
		RefreshHash: input.RefreshHash,
		CreatedAt:   time.Now(),
		ExpiresAt:   input.ExpiresAt,
	}
	if input.DeviceID != "" {
		for _, existing := range s.rows {
			if existing.UserID == input.UserID && existing.DeviceID == input.DeviceID && existing.RevokedAt.IsZero() {
				existing.RevokedAt = time.Now()
				s.rows[existing.ID] = existing
			}
		}
	}
	s.rows[row.ID] = row
	return row, nil
}

func (s *memAuthSessions) GetAuthSessionByID(_ context.Context, sessionID string) (dbstore.AuthSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return dbstore.AuthSessionRecord{}, s.getErr
	}
	row, ok := s.rows[sessionID]
	if !ok {
		return dbstore.AuthSessionRecord{}, db.ErrNotFound
	}
	return row, nil
}

func (s *memAuthSessions) GetActiveAuthSession(_ context.Context, sessionID, userID string) error {
	row, ok := s.row(sessionID)
	if !ok || row.UserID != userID || !row.RevokedAt.IsZero() || !row.ExpiresAt.After(time.Now()) {
		return db.ErrNotFound
	}
	return nil
}

func (s *memAuthSessions) TouchAuthSession(_ context.Context, sessionID string, expiresAt time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.touchErr != nil {
		return 0, s.touchErr
	}
	row, ok := s.rows[sessionID]
	if !ok || !row.RevokedAt.IsZero() || !row.ExpiresAt.After(time.Now()) {
		return 0, nil
	}
	row.LastUsedAt = time.Now()
	row.ExpiresAt = expiresAt
	s.rows[sessionID] = row
	return 1, nil
}

func (s *memAuthSessions) RevokeAuthSession(_ context.Context, sessionID, userID, _ string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[sessionID]
	if !ok || row.UserID != userID || !row.RevokedAt.IsZero() {
		return 0, nil
	}
	row.RevokedAt = time.Now()
	s.rows[sessionID] = row
	return 1, nil
}

func (*memAuthSessions) DeleteStaleAuthSessions(context.Context) (int64, error) { return 0, nil }

// authAccountStore is the AccountStore of the fixture: one active user whose
// password change commits together with the session revocation.
type authAccountStore struct {
	mu          sync.Mutex
	password    string
	storedHash  string
	inactive    bool
	failRevoke  bool
	pwUpdates   int
	revokeCalls int
	sessions    *memAuthSessions
}

func (s *authAccountStore) record() dbstore.AccountRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, _ := bcrypt.GenerateFromPassword([]byte(s.password), bcrypt.MinCost)
	if s.storedHash != "" {
		hash = []byte(s.storedHash)
	}
	return dbstore.AccountRecord{
		ID:              authTestUserID,
		Username:        "alice",
		Role:            "admin",
		DisplayName:     "Alice",
		Timezone:        "UTC",
		PasswordHash:    string(hash),
		HasPasswordHash: true,
		IsActive:        !s.inactive,
	}
}

func (*authAccountStore) CountAccounts(context.Context) (int64, error) { return 1, nil }
func (*authAccountStore) List(context.Context) ([]dbstore.AccountRecord, error) {
	return nil, nil
}

func (*authAccountStore) Search(context.Context, string, int32) ([]dbstore.AccountRecord, error) {
	return nil, nil
}

func (*authAccountStore) CreateUser(context.Context, dbstore.CreateUserInput) (dbstore.AccountRecord, error) {
	return dbstore.AccountRecord{}, errors.New("not implemented")
}

func (*authAccountStore) CreateAccount(context.Context, dbstore.CreateAccountInput) (dbstore.AccountRecord, error) {
	return dbstore.AccountRecord{}, errors.New("not implemented")
}
func (*authAccountStore) UpdateLastLogin(context.Context, string) error { return nil }
func (*authAccountStore) UpdateAdmin(context.Context, dbstore.UpdateAccountAdminInput) (dbstore.AccountRecord, error) {
	return dbstore.AccountRecord{}, errors.New("not implemented")
}

func (*authAccountStore) UpdateProfile(context.Context, dbstore.UpdateAccountProfileInput) (dbstore.AccountRecord, error) {
	return dbstore.AccountRecord{}, errors.New("not implemented")
}

func (*authAccountStore) IsValidTitleModel(context.Context, string) (bool, error) {
	return false, nil
}
func (*authAccountStore) RemoveMember(context.Context, string) error { return nil }

func (s *authAccountStore) GetByUserID(context.Context, string) (dbstore.AccountRecord, error) {
	return s.record(), nil
}

func (s *authAccountStore) GetByIdentity(_ context.Context, identity string) (dbstore.AccountRecord, error) {
	if identity != "alice" {
		return dbstore.AccountRecord{}, db.ErrNotFound
	}
	return s.record(), nil
}

func (s *authAccountStore) UpdatePasswordAndRevokeSessions(ctx context.Context, input dbstore.UpdateAccountPasswordInput) error {
	s.mu.Lock()
	s.pwUpdates++
	fail := s.failRevoke
	s.mu.Unlock()
	if fail {
		return errors.New("revoke sessions failed")
	}
	// The revocation belongs to the same commit as the password change: it
	// either happens with the change or not at all.
	if err := s.sessions.revokeAllForUser(ctx, input.UserID); err != nil {
		return err
	}
	s.mu.Lock()
	s.storedHash = input.PasswordHash
	s.revokeCalls++
	s.mu.Unlock()
	return nil
}

func (s *memAuthSessions) revokeAllForUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, row := range s.rows {
		if row.UserID == userID && row.RevokedAt.IsZero() {
			row.RevokedAt = time.Now()
			s.rows[id] = row
		}
	}
	return nil
}

// The fixture serves the auth routes behind the real error handler and JWT
// middleware, plus a probe route that reports the authenticated identity.
type authFixture struct {
	e        *echo.Echo
	sessions *memAuthSessions
	store    *authAccountStore
}

func newAuthFixture(t *testing.T, idleTTL time.Duration) *authFixture {
	t.Helper()
	sessions := newMemAuthSessions()
	store := &authAccountStore{password: authTestPassword, sessions: sessions}
	accountService := accounts.NewService(slog.New(slog.DiscardHandler), store)
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	e.Use(auth.JWTMiddleware(authTestSecret, func(c echo.Context) bool {
		path := c.Request().URL.Path
		return path == "/auth/login" || path == "/auth/refresh" || path == "/auth/logout"
	}, AccountSessionValidator(accountService, sessions)))
	NewAuthHandler(slog.New(slog.DiscardHandler), accountService, sessions, authTestSecret, 24*time.Hour, idleTTL).Register(e)
	NewUsersHandler(slog.New(slog.DiscardHandler), accountService, nil, nil, nil, nil, nil, nil).Register(e)
	e.GET("/protected", func(c echo.Context) error {
		userID, err := auth.UserIDFromContext(c)
		if err != nil {
			return err
		}
		sid, _ := auth.SessionIDFromContext(c)
		return c.JSON(http.StatusOK, map[string]string{"user_id": userID, "sid": sid})
	})
	return &authFixture{e: e, sessions: sessions, store: store}
}

func (f *authFixture) do(t *testing.T, method, path string, body any, bearer string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	if bearer != "" {
		req.Header.Set(echo.HeaderAuthorization, bearer)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	var payload map[string]any
	if rec.Body.Len() > 0 && strings.Contains(rec.Header().Get(echo.HeaderContentType), "json") {
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	}
	return rec, payload
}

func (f *authFixture) login(t *testing.T, deviceID string) map[string]any {
	t.Helper()
	body := map[string]any{"username": "alice", "password": authTestPassword}
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	rec, payload := f.do(t, http.MethodPost, "/auth/login", body, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return payload
}

func accessClaims(t *testing.T, token string) jwt.MapClaims {
	t.Helper()
	parsed, err := jwt.Parse(token, func(*jwt.Token) (interface{}, error) { return []byte(authTestSecret), nil })
	require.NoError(t, err)
	claims, ok := parsed.Claims.(jwt.MapClaims)
	require.True(t, ok)
	return claims
}

func TestLoginCreatesDeviceSessionWithRefreshToken(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)

	response := f.login(t, "")
	for _, field := range []string{"access_token", "token_type", "expires_at", "user_id", "role", "display_name", "username", "refresh_token", "refresh_expires_at"} {
		assert.Contains(t, response, field)
	}
	assert.Equal(t, authTestUserID, response["user_id"])

	refreshToken, _ := response["refresh_token"].(string)
	sessionID, secret, ok := auth.ParseRefreshToken(refreshToken)
	require.True(t, ok)
	row, found := f.sessions.row(sessionID)
	require.True(t, found)
	assert.True(t, auth.RefreshSecretMatches(secret, row.RefreshHash))
	assert.Empty(t, row.DeviceID)
	assert.True(t, row.ExpiresAt.After(time.Now().Add(23*time.Hour)), "session expiry should be the idle ttl")

	claims := accessClaims(t, response["access_token"].(string))
	assert.Equal(t, sessionID, claims["sid"])
	assert.Equal(t, authTestUserID, claims["user_id"])
	assert.NotContains(t, claims, "typ")

	device := f.login(t, "device-1")
	deviceSession, _, ok := auth.ParseRefreshToken(device["refresh_token"].(string))
	require.True(t, ok)
	stored, found := f.sessions.row(deviceSession)
	require.True(t, found)
	assert.Equal(t, "device-1", stored.DeviceID)
}

func TestLoginReplacesSameDeviceSession(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)

	first := f.login(t, "device-1")
	second := f.login(t, "device-1")

	firstSession, _, ok := auth.ParseRefreshToken(first["refresh_token"].(string))
	require.True(t, ok)
	secondSession, _, ok := auth.ParseRefreshToken(second["refresh_token"].(string))
	require.True(t, ok)
	assert.NotEqual(t, firstSession, secondSession)

	row, found := f.sessions.row(firstSession)
	require.True(t, found)
	assert.False(t, row.RevokedAt.IsZero(), "the replaced session must be revoked")
	row, found = f.sessions.row(secondSession)
	require.True(t, found)
	assert.True(t, row.RevokedAt.IsZero())
	assert.Equal(t, 1, f.sessions.liveCount())
}

func TestLoginConflictMapsToUnavailable(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)
	conflicting := &conflictSessions{AuthSessionStore: f.sessions}
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	accountService := accounts.NewService(slog.New(slog.DiscardHandler), f.store)
	NewAuthHandler(slog.New(slog.DiscardHandler), accountService, conflicting, authTestSecret, time.Hour, time.Hour).Register(e)

	rec, _ := f.loginOn(t, e, "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func (f *authFixture) loginOn(t *testing.T, e *echo.Echo, deviceID string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	body := map[string]any{"username": "alice", "password": authTestPassword}
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(string(raw)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var payload map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	}
	return rec, payload
}

// conflictSessions reports the sentinel a retry could not clear.
type conflictSessions struct {
	dbstore.AuthSessionStore
}

func (*conflictSessions) CreateAuthSession(context.Context, dbstore.CreateAuthSessionInput) (dbstore.AuthSessionRecord, error) {
	return dbstore.AuthSessionRecord{}, fmt.Errorf("%w: auth_sessions_active_device", db.ErrAuthSessionConflict)
}

func TestRefreshReturnsTokensAndSlidesExpiry(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)
	login := f.login(t, "device-1")
	refreshToken := login["refresh_token"].(string)
	sessionID, _, ok := auth.ParseRefreshToken(refreshToken)
	require.True(t, ok)
	row, _ := f.sessions.row(sessionID)
	before := row.ExpiresAt

	rec, response := f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshToken}, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, refreshToken, response["refresh_token"], "the static refresh token must be returned unchanged")
	assert.NotEmpty(t, response["access_token"])
	assert.Equal(t, "Bearer", response["token_type"])

	claims := accessClaims(t, response["access_token"].(string))
	assert.Equal(t, sessionID, claims["sid"])

	row, _ = f.sessions.row(sessionID)
	assert.True(t, row.ExpiresAt.After(before), "expiry should slide forward")
	assert.False(t, row.LastUsedAt.IsZero())

	// The same refresh token can be used again.
	rec, response = f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshToken}, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, refreshToken, response["refresh_token"])
}

func TestRefreshRejectsInvalidGrants(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)
	login := f.login(t, "device-1")
	valid := login["refresh_token"].(string)
	sessionID, secret, ok := auth.ParseRefreshToken(valid)
	require.True(t, ok)

	cases := []struct {
		name  string
		token string
	}{
		{name: "missing token", token: ""},
		{name: "malformed token", token: "not-a-refresh-token"},
		{name: "wrong prefix", token: "xx_" + sessionID + "." + secret},
		{name: "unknown session", token: auth.FormatRefreshToken("99999999-9999-4999-8999-999999999999", secret)},
		{name: "wrong secret", token: auth.FormatRefreshToken(sessionID, secret+"x")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": tc.token}, "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestRefreshRejectsRevokedAndExpiredSessions(t *testing.T) {
	t.Run("revoked", func(t *testing.T) {
		f := newAuthFixture(t, 24*time.Hour)
		login := f.login(t, "")
		refreshToken := login["refresh_token"].(string)
		rec, _ := f.do(t, http.MethodPost, "/auth/logout", map[string]string{"refresh_token": refreshToken}, "")
		require.Equal(t, http.StatusNoContent, rec.Code)

		rec, _ = f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshToken}, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("expired", func(t *testing.T) {
		f := newAuthFixture(t, 40*time.Millisecond)
		login := f.login(t, "")
		time.Sleep(80 * time.Millisecond)
		rec, _ := f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": login["refresh_token"].(string)}, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestRefreshRejectsInactiveAccount(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)
	login := f.login(t, "device-1")

	f.store.mu.Lock()
	f.store.inactive = true
	f.store.mu.Unlock()

	rec, _ := f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": login["refresh_token"].(string)}, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// A database failure must answer 503, never 401, and must not revoke the
// session: an outage does not log devices out.
func TestRefreshInfrastructureFailureAnswersUnavailable(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)
	login := f.login(t, "device-1")
	refreshToken := login["refresh_token"].(string)
	sessionID, _, ok := auth.ParseRefreshToken(refreshToken)
	require.True(t, ok)

	f.sessions.mu.Lock()
	f.sessions.getErr = errors.New("connection refused")
	f.sessions.mu.Unlock()
	rec, _ := f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshToken}, "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	f.sessions.mu.Lock()
	f.sessions.getErr = nil
	f.sessions.touchErr = errors.New("connection reset")
	f.sessions.mu.Unlock()
	rec, _ = f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshToken}, "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	f.sessions.mu.Lock()
	f.sessions.touchErr = nil
	f.sessions.mu.Unlock()
	row, found := f.sessions.row(sessionID)
	require.True(t, found)
	assert.True(t, row.RevokedAt.IsZero(), "no failure path may revoke the session")
	assert.True(t, row.ExpiresAt.After(time.Now()))

	rec, _ = f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": refreshToken}, "")
	assert.Equal(t, http.StatusOK, rec.Code, "the session must still refresh after the outage")
}

func TestLogoutIsIdempotent(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)
	login := f.login(t, "device-1")
	refreshToken := login["refresh_token"].(string)

	rec, _ := f.do(t, http.MethodPost, "/auth/logout", map[string]string{"refresh_token": refreshToken}, "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	for name, token := range map[string]string{
		"repeat":     refreshToken,
		"unknown":    auth.FormatRefreshToken("99999999-9999-4999-8999-999999999999", "secret"),
		"malformed":  "rt_nonsense",
		"empty body": "",
	} {
		t.Run(name, func(t *testing.T) {
			rec, _ := f.do(t, http.MethodPost, "/auth/logout", map[string]string{"refresh_token": token}, "")
			assert.Equal(t, http.StatusNoContent, rec.Code)
		})
	}
}

// End to end: login, refresh, use the new access token, log out, and watch
// both tokens die.
func TestLoginRefreshAccessLogoutEndToEnd(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)
	login := f.login(t, "device-1")

	rec, body := f.do(t, http.MethodGet, "/protected", nil, "Bearer "+login["access_token"].(string))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, authTestUserID, body["user_id"])

	rec, refreshed := f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": login["refresh_token"].(string)}, "")
	require.Equal(t, http.StatusOK, rec.Code)
	access := refreshed["access_token"].(string)

	rec, _ = f.do(t, http.MethodGet, "/protected", nil, "Bearer "+access)
	require.Equal(t, http.StatusOK, rec.Code)

	// A session token bound to another user is rejected even with a live sid.
	sessionID, _, ok := auth.ParseRefreshToken(login["refresh_token"].(string))
	require.True(t, ok)
	foreign, _, err := auth.GenerateSessionToken(authTestUserOther, sessionID, authTestSecret, time.Hour)
	require.NoError(t, err)
	rec, _ = f.do(t, http.MethodGet, "/protected", nil, "Bearer "+foreign)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec, _ = f.do(t, http.MethodPost, "/auth/logout", map[string]string{"refresh_token": login["refresh_token"].(string)}, "")
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec, _ = f.do(t, http.MethodGet, "/protected", nil, "Bearer "+access)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "the access token must die with its session")
	rec, _ = f.do(t, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": login["refresh_token"].(string)}, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// An internal token keeps working; it is not a device session.
	internal, _, err := auth.GenerateInternalToken(authTestUserID, authTestSecret, time.Hour)
	require.NoError(t, err)
	rec, body = f.do(t, http.MethodGet, "/protected", nil, "Bearer "+internal)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, body["sid"])
}

func TestUpdatePasswordRevokesAllSessionsInSameTransaction(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)
	first := f.login(t, "device-1")
	second := f.login(t, "device-2")
	third := f.login(t, "")

	rec, _ := f.do(t, http.MethodPut, "/users/me/password", map[string]string{"current_password": authTestPassword, "new_password": authTestNewPass}, "Bearer "+first["access_token"].(string))
	require.Equal(t, http.StatusNoContent, rec.Code)

	for _, login := range []map[string]any{first, second, third} {
		sessionID, _, ok := auth.ParseRefreshToken(login["refresh_token"].(string))
		require.True(t, ok)
		row, found := f.sessions.row(sessionID)
		require.True(t, found)
		assert.False(t, row.RevokedAt.IsZero(), "every session must be revoked")
		rec, _ := f.do(t, http.MethodGet, "/protected", nil, "Bearer "+login["access_token"].(string))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	}

	f.store.mu.Lock()
	updates, revokes := f.store.pwUpdates, f.store.revokeCalls
	f.store.mu.Unlock()
	assert.Equal(t, 1, updates)
	assert.Equal(t, 1, revokes, "revocation happens inside the password change")

	// The old password no longer works; the new one does.
	rec, _ = f.do(t, http.MethodPost, "/auth/login", map[string]string{"username": "alice", "password": authTestPassword}, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec, _ = f.do(t, http.MethodPost, "/auth/login", map[string]string{"username": "alice", "password": authTestNewPass}, "")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestUpdatePasswordFailsWhenSessionRevocationFails(t *testing.T) {
	f := newAuthFixture(t, 24*time.Hour)
	login := f.login(t, "device-1")

	f.store.mu.Lock()
	f.store.failRevoke = true
	f.store.mu.Unlock()
	rec, _ := f.do(t, http.MethodPut, "/users/me/password", map[string]string{"current_password": authTestPassword, "new_password": authTestNewPass}, "Bearer "+login["access_token"].(string))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	f.store.mu.Lock()
	f.store.failRevoke = false
	f.store.mu.Unlock()

	// The password change was rolled back with the failed revocation.
	rec, _ = f.do(t, http.MethodPost, "/auth/login", map[string]string{"username": "alice", "password": authTestPassword}, "")
	assert.Equal(t, http.StatusOK, rec.Code)
	rec, _ = f.do(t, http.MethodPost, "/auth/login", map[string]string{"username": "alice", "password": authTestNewPass}, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
