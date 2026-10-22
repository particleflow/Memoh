package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/server"
)

type oauthHTTPQueries struct {
	oauthConnectionScopeQueries
	reads atomic.Int32
}

func (*oauthHTTPQueries) GetBotByID(_ context.Context, id pgtype.UUID) (sqlc.GetBotByIDRow, error) {
	return testBotRow(id.String(), nil), nil
}

func (q *oauthHTTPQueries) GetMCPOAuthToken(_ context.Context, _ pgtype.UUID) (sqlc.McpOauthToken, error) {
	q.reads.Add(1)
	return sqlc.McpOauthToken{ //nolint:gosec // G101: synthetic markers assert credentials never enter the response.
		AuthorizationEndpoint: "https://synthetic.invalid/authorize",
		AccessToken:           "SECRET old expired token",
		RefreshToken:          "SECRET refresh token",
		PkceCodeVerifier:      "SECRET verifier",
		StateParam:            "SECRET state",
		ExpiresAt:             pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	}, nil
}

type captureOAuthHandler struct {
	handler *MCPOAuthHandler
	echo    *echo.Echo
}

func (h *captureOAuthHandler) Register(e *echo.Echo) {
	h.echo = e
	h.handler.Register(e)
}

func TestMCPOAuthNoStoreThroughServerMiddleware(t *testing.T) {
	const (
		bot        = "11111111-1111-4111-8111-111111111111"
		connection = "33333333-3333-4333-8333-333333333333"
		owner      = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		revoked    = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
		secret     = "synthetic-oauth-http-secret" //nolint:gosec // G101: test-only signing key.
		base       = "/bots/" + bot + "/mcp/" + connection + "/oauth"
	)
	q := &oauthHTTPQueries{oauthConnectionScopeQueries: oauthConnectionScopeQueries{connection: sqlc.McpConnection{
		ID: testUUID(connection), BotID: testUUID(bot), Type: "http", Config: []byte(`{"url":"https://synthetic.invalid/mcp"}`),
	}}}
	h := NewMCPOAuthHandler(mcp.NewOAuthService(nil, q, "https://synthetic.invalid/oauth/mcp/callback"),
		mcp.NewConnectionService(nil, q), bots.NewService(nil, q), newTestAdminAccountService("admin"))
	capture := &captureOAuthHandler{handler: h}
	// Construct the real server: its global JWT/session validator, CORS, error
	// handler and request-ID middleware all run before the registered handlers.
	_ = server.NewServerWithSessionValidator(slog.New(slog.DiscardHandler), ":0", secret, func(_ context.Context, user string) error {
		if user == revoked {
			return errors.New("SECRET revoked account diagnostic")
		}
		return nil
	}, capture)
	transport := httptest.NewServer(capture.echo)
	t.Cleanup(transport.Close)
	sign := func(user, key string, ttl time.Duration) string {
		t.Helper()
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user, "user_id": user, "exp": time.Now().Add(ttl).Unix()}).SignedString([]byte(key))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	request := func(t *testing.T, method, path, token string, status int, noStore bool) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, transport.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
		}
		response, err := transport.Client().Do(req) //nolint:gosec // G704: test-owned HTTP server and fixed synthetic routes.
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatalf("%s %s: status=%d body=%s", method, path, response.StatusCode, body)
		}
		if (response.Header.Get(echo.HeaderCacheControl) == "no-store") != noStore {
			t.Fatalf("%s %s: wrong cache policy %q", method, path, response.Header.Get(echo.HeaderCacheControl))
		}
		if response.Header.Get(echo.HeaderXRequestID) == "" || (status == 401 && response.Header.Get(echo.HeaderContentType) != "application/problem+json") {
			t.Fatal("server request ID or Problem contract missing")
		}
		if strings.Contains(string(body), "SECRET") {
			t.Fatal("private OAuth or account diagnostic leaked")
		}
		return body
	}
	for name, token := range map[string]string{
		"anonymous": "", "malformed": "invalid",
		"bad_signature": sign(owner, "wrong-secret", time.Minute),
		"expired":       sign(owner, secret, -time.Minute), "revoked": sign(revoked, secret, time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			for _, route := range []struct{ method, path string }{
				{http.MethodGet, base + "/status"},
				{http.MethodPost, base + "/authorize"},
				{http.MethodPost, base + "/exchange"},
				{http.MethodPost, base + "/discover"},
				{http.MethodDelete, base + "/token"},
				{http.MethodGet, base},
				{http.MethodGet, base + "/unknown"},
			} {
				request(t, route.method, route.path, token, 401, true)
			}
		})
	}
	if q.reads.Load() != 0 {
		t.Fatal("rejected request reached OAuth storage")
	}
	for _, path := range []string{base + "-extra/status", strings.TrimSuffix(base, "/oauth") + "/probe", "/other" + base + "/status"} {
		request(t, http.MethodGet, path, "", 401, false)
	}
	valid := sign(owner, secret, time.Minute)
	var status mcp.OAuthStatus
	if err := json.Unmarshal(request(t, http.MethodGet, base+"/status", valid, 200, true), &status); err != nil {
		t.Fatal(err)
	}
	if !status.HasToken || !status.Expired || status.Authorization != nil {
		t.Fatal("old-token status changed")
	}
	if err := json.Unmarshal(request(t, http.MethodGet, base+"/status?authorization_id="+strings.Repeat("a", 64), valid, 200, true), &status); err != nil {
		t.Fatal(err)
	}
	if status.Authorization == nil || status.Authorization.Status != "unknown" {
		t.Fatalf("missing unknown authorization receipt: %+v", status.Authorization)
	}
	request(t, http.MethodGet, base+"/status?authorization_id="+strings.Repeat("a", 65), valid, 400, true)
	request(t, http.MethodGet, strings.Replace(base, connection, bot, 1)+"/status", valid, 404, true)
	request(t, http.MethodGet, strings.Replace(base, bot, owner, 1)+"/status", valid, 404, true)
	request(t, http.MethodPost, base+"/status", valid, 405, true)
	request(t, http.MethodGet, base+"/unknown", valid, 404, true)
	for _, path := range []string{"/oauth/mcp/callback", "/api/oauth/mcp/callback"} {
		request(t, http.MethodGet, path, "", 400, true)
		request(t, http.MethodPost, path, "", 405, true)
		request(t, http.MethodGet, path+"/unknown", "", 404, true)
		request(t, http.MethodGet, path+"-extra", "", 404, false)
	}
	if q.reads.Load() != 2 {
		t.Fatal("invalid requests unexpectedly accessed OAuth tokens")
	}
}
