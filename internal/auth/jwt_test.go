package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSessionUser = "11111111-1111-4111-8111-111111111111"
	testSessionID   = "22222222-2222-4222-8222-222222222222"
)

func parseTestClaims(t *testing.T, token, secret string) jwt.MapClaims {
	t.Helper()
	parsed, err := jwt.Parse(token, func(*jwt.Token) (interface{}, error) { return []byte(secret), nil })
	require.NoError(t, err)
	claims, ok := parsed.Claims.(jwt.MapClaims)
	require.True(t, ok)
	return claims
}

func signTestClaims(t *testing.T, claims jwt.MapClaims, secret string) string {
	t.Helper()
	claims["iat"] = time.Now().Unix()
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return signed
}

func TestGenerateInternalTokenCarriesInternalType(t *testing.T) {
	const secret = "test-secret"
	token, expiresAt, err := GenerateInternalToken("user-123", secret, 24*time.Hour)
	require.NoError(t, err)

	claims := parseTestClaims(t, token, secret)
	assert.Equal(t, "internal", claims[claimType])
	assert.Equal(t, "user-123", claims[claimUserID])
	assert.Equal(t, "user-123", claims[claimSubject])
	assert.NotContains(t, claims, claimSessionID)
	assert.Equal(t, int64(claims["exp"].(float64)), expiresAt.Unix())
	assert.Equal(t, int64(24*time.Hour/time.Second), int64(claims["exp"].(float64)-claims["iat"].(float64)))
}

func TestGenerateSessionTokenCarriesSessionWithoutType(t *testing.T) {
	const secret = "test-secret"
	token, _, err := GenerateSessionToken(testSessionUser, testSessionID, secret, time.Hour)
	require.NoError(t, err)

	claims := parseTestClaims(t, token, secret)
	assert.Equal(t, testSessionID, claims[claimSessionID])
	assert.Equal(t, testSessionUser, claims[claimUserID])
	assert.NotContains(t, claims, claimType)

	_, _, err = GenerateSessionToken(testSessionUser, " ", secret, time.Hour)
	require.Error(t, err)
}

func TestSessionAndInternalTokenFromContext(t *testing.T) {
	const secret = "test-secret"
	sessionToken, _, err := GenerateSessionToken(testSessionUser, testSessionID, secret, time.Hour)
	require.NoError(t, err)
	internalToken, _, err := GenerateInternalToken(testSessionUser, secret, time.Hour)
	require.NoError(t, err)

	e := echo.New()
	e.Use(JWTMiddleware(secret, func(echo.Context) bool { return false }))
	e.GET("/claims", func(c echo.Context) error {
		sid, present := SessionIDFromContext(c)
		return c.JSON(http.StatusOK, map[string]any{"sid": sid, "present": present, "internal": InternalTokenFromContext(c)})
	})

	for name, tc := range map[string]struct {
		token string
		want  string
	}{
		"session":  {token: sessionToken, want: `{"internal":false,"present":true,"sid":"` + testSessionID + `"}`},
		"internal": {token: internalToken, want: `{"internal":true,"present":false,"sid":""}`},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/claims", nil)
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+tc.token)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t, tc.want, rec.Body.String())
		})
	}

	empty := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	sid, present := SessionIDFromContext(empty)
	assert.Empty(t, sid)
	assert.False(t, present)
	assert.False(t, InternalTokenFromContext(empty))
}

type validatorCall struct {
	userID    string
	sessionID string
}

// The middleware admits an account token only as a device session token or an
// internal token; everything else is rejected before any lookup.
func TestJWTMiddlewareAccountTokenBranches(t *testing.T) {
	const secret = "test-secret"
	sessionToken, _, err := GenerateSessionToken(testSessionUser, testSessionID, secret, time.Hour)
	require.NoError(t, err)
	foreignSessionToken, _, err := GenerateSessionToken("33333333-3333-4333-8333-333333333333", testSessionID, secret, time.Hour)
	require.NoError(t, err)
	internalToken, _, err := GenerateInternalToken(testSessionUser, secret, time.Hour)
	require.NoError(t, err)
	chatToken, _, err := GenerateChatToken(ChatToken{BotID: "bot-1", ChatID: "chat-1", UserID: "channel-identity-1"}, secret, time.Hour)
	require.NoError(t, err)

	cases := []struct {
		name      string
		token     string
		wantCode  int
		wantCalls []validatorCall
	}{
		{name: "chat route token bypasses account validation", token: chatToken, wantCode: http.StatusNoContent},
		{name: "session token validates its session", token: sessionToken, wantCode: http.StatusNoContent, wantCalls: []validatorCall{{testSessionUser, testSessionID}}},
		{name: "session of another user is rejected", token: foreignSessionToken, wantCode: http.StatusUnauthorized, wantCalls: []validatorCall{{"33333333-3333-4333-8333-333333333333", testSessionID}}},
		{name: "internal token validates the account alone", token: internalToken, wantCode: http.StatusNoContent, wantCalls: []validatorCall{{testSessionUser, ""}}},
		{name: "token without session or type is rejected", token: signTestClaims(t, jwt.MapClaims{claimSubject: testSessionUser, claimUserID: testSessionUser}, secret), wantCode: http.StatusUnauthorized},
		{name: "empty sid is rejected without internal fallback", token: signTestClaims(t, jwt.MapClaims{claimUserID: testSessionUser, claimSessionID: "", claimType: "internal"}, secret), wantCode: http.StatusUnauthorized},
		{name: "blank sid is rejected", token: signTestClaims(t, jwt.MapClaims{claimUserID: testSessionUser, claimSessionID: "   "}, secret), wantCode: http.StatusUnauthorized},
		{name: "null sid is rejected without internal fallback", token: signTestClaims(t, jwt.MapClaims{claimUserID: testSessionUser, claimSessionID: nil, claimType: "internal"}, secret), wantCode: http.StatusUnauthorized},
		{name: "malformed sid is rejected without internal fallback", token: signTestClaims(t, jwt.MapClaims{claimUserID: testSessionUser, claimSessionID: "not-a-uuid", claimType: "internal"}, secret), wantCode: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []validatorCall
			e := echo.New()
			e.Use(JWTMiddleware(secret, func(echo.Context) bool { return false }, func(_ context.Context, userID, sessionID string) error {
				calls = append(calls, validatorCall{userID, sessionID})
				if userID != testSessionUser {
					return errTestInactiveSession
				}
				if sessionID != "" && sessionID != testSessionID {
					return errTestInactiveSession
				}
				return nil
			}))
			e.GET("/protected", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+tc.token)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantCode, rec.Code)
			assert.Equal(t, tc.wantCalls, calls)
		})
	}
}

func TestJWTMiddlewareRejectsInactiveAccountSession(t *testing.T) {
	const secret = "test-secret"
	token, _, err := GenerateInternalToken("user-123", secret, time.Hour)
	require.NoError(t, err)

	validatedUserID := ""
	e := echo.New()
	e.Use(JWTMiddleware(secret, func(echo.Context) bool { return false }, func(_ context.Context, userID, _ string) error {
		validatedUserID = userID
		return errTestInactiveSession
	}))
	e.GET("/protected", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "user-123", validatedUserID)
}

func TestJWTMiddlewareDoesNotValidateChatRouteTokenAsAccount(t *testing.T) {
	const secret = "test-secret"
	token, _, err := GenerateChatToken(ChatToken{
		BotID:  "bot-1",
		ChatID: "chat-1",
		UserID: "channel-identity-1",
	}, secret, time.Hour)
	require.NoError(t, err)

	validatorCalled := false
	e := echo.New()
	e.Use(JWTMiddleware(secret, func(echo.Context) bool { return false }, func(context.Context, string, string) error {
		validatorCalled = true
		return nil
	}))
	e.GET("/chat-route", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/chat-route", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.False(t, validatorCalled)
}

// The token a handler forwards has to be the one that was checked. A browser
// WebSocket sends it as a query parameter, and an Authorization header in
// another scheme does not replace it.
func TestRawTokenFromContextIsTheValidatedToken(t *testing.T) {
	const secret = "test-secret"
	token, _, err := GenerateInternalToken("user-123", secret, time.Hour)
	require.NoError(t, err)

	e := echo.New()
	e.Use(JWTMiddleware(secret, func(echo.Context) bool { return false }))
	e.GET("/protected", func(c echo.Context) error { return c.String(http.StatusOK, RawTokenFromContext(c)) })

	for name, setup := range map[string]func(*http.Request){
		"header": func(r *http.Request) { r.Header.Set(echo.HeaderAuthorization, "Bearer "+token) },
		"query":  func(r *http.Request) { r.URL.RawQuery = "token=" + token },
		"query beside another scheme": func(r *http.Request) {
			r.Header.Set(echo.HeaderAuthorization, "Basic dXNlcjpwYXNz")
			r.URL.RawQuery = "token=" + token
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			setup(req)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, token, rec.Body.String())
		})
	}
}

func TestRawTokenFromContextWithoutToken(t *testing.T) {
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	assert.Empty(t, RawTokenFromContext(c))
}

func TestRefreshTokenFormatParseAndHash(t *testing.T) {
	secret, hash, err := NewRefreshSecret()
	require.NoError(t, err)
	assert.Len(t, secret, 43, "256-bit secret in unpadded base64url")
	assert.Equal(t, HashRefreshSecret(secret), hash)
	assert.NotEqual(t, secret, hash)

	token := FormatRefreshToken(testSessionID, secret)
	assert.True(t, strings.HasPrefix(token, "rt_"))
	sessionID, parsedSecret, ok := ParseRefreshToken(" " + token + " ")
	require.True(t, ok)
	assert.Equal(t, testSessionID, sessionID)
	assert.Equal(t, secret, parsedSecret)
	assert.True(t, RefreshSecretMatches(parsedSecret, hash))
	assert.False(t, RefreshSecretMatches(parsedSecret+"x", hash))

	other, _, err := NewRefreshSecret()
	require.NoError(t, err)
	assert.NotEqual(t, secret, other)

	for _, malformed := range []string{"", "rt_", "rt_" + testSessionID, "rt_" + testSessionID + ".", "rt_not-a-uuid.secret", testSessionID + ".secret", "xx_" + testSessionID + ".secret"} {
		_, _, ok := ParseRefreshToken(malformed)
		assert.False(t, ok, malformed)
	}
}

var errTestInactiveSession = errors.New("inactive test session")
