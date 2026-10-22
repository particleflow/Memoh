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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/userruntime"
	"github.com/felinics/memoh/internal/workspace"
)

const privateDeviceBot = "11111111-1111-4111-8111-111111111111"

type privateDeviceQueries struct{ dbstore.Queries }

func (*privateDeviceQueries) GetBotByID(_ context.Context, id pgtype.UUID) (sqlc.GetBotByIDRow, error) {
	if id != testUUID(privateDeviceBot) {
		return sqlc.GetBotByIDRow{}, pgx.ErrNoRows
	}
	return testBotRow(privateDeviceBot, nil), nil
}

func (*privateDeviceQueries) ListBotUserGrantsForUser(context.Context, sqlc.ListBotUserGrantsForUserParams) ([]sqlc.ListBotUserGrantsForUserRow, error) {
	return nil, nil
}

type privateDeviceTargets struct{ reads atomic.Int32 }

func (f *privateDeviceTargets) ListWorkspaceTargets(context.Context, string) ([]workspace.WorkspaceTarget, error) {
	f.reads.Add(1)
	return []workspace.WorkspaceTarget{{
		TargetID: "private-device-id", Name: "Synthetic private computer",
		Provider: agenvo.Provider, Status: "authentication_unavailable", Online: false,
	}}, nil
}

type privateDeviceStore struct {
	dbstore.UserRuntimeStore
	reads atomic.Int32
}

func (f *privateDeviceStore) ExpirePendingUserRuntimes(context.Context, string) error {
	f.reads.Add(1)
	return nil
}

func (f *privateDeviceStore) ListUserRuntimes(_ context.Context, user string) ([]dbstore.UserRuntimeRecord, error) {
	f.reads.Add(1)
	return []dbstore.UserRuntimeRecord{{
		ID: "private-device-id", UserID: user, Name: "Synthetic private computer",
		Provider: agenvo.Provider, ActivatedAt: time.Now(),
		APIToken: "SECRET synthetic storage marker", //nolint:gosec // G101: fixture verifies Agenvo credentials are not projected.
	}}, nil
}

type privateDeviceHTTPHandlers struct {
	handlers []server.Handler
	echo     *echo.Echo
}

func (h *privateDeviceHTTPHandlers) Register(e *echo.Echo) {
	h.echo = e
	for _, handler := range h.handlers {
		handler.Register(e)
	}
	e.GET("/bots/"+privateDeviceBot+"/sessions", func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderCacheControl, "private, max-age=60")
		return c.NoContent(http.StatusOK)
	})
}

func TestPrivateDeviceNoStoreThroughServerMiddleware(t *testing.T) {
	const (
		owner   = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		other   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
		revoked = "cccccccc-cccc-cccc-cccc-cccccccccccc"
		secret  = "synthetic-private-device-http-key" //nolint:gosec // G101: test-only signing key.
		base    = "/bots/" + privateDeviceBot + "/workspace-targets"
		account = "/users/me/runtimes"
	)
	targets, store := &privateDeviceTargets{}, &privateDeviceStore{}
	botHandler := NewBotRemoteRuntimeHandler(nil, nil, nil, nil, bots.NewService(nil, &privateDeviceQueries{}), newTestAdminAccountService("user"))
	botHandler.workspaces = targets
	capture := &privateDeviceHTTPHandlers{handlers: []server.Handler{botHandler, NewUserRuntimeHandler(nil, userruntime.NewService(store, nil))}}
	// Use the real global JWT/session validator, CORS and Problem/error chain,
	// with actual TCP HTTP. The device stores are synthetic and never execute.
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
	request := func(t *testing.T, method, path, token string, status int, policy string) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, transport.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
		}
		response, err := transport.Client().Do(req) //nolint:gosec // G704: test-owned HTTP server and fixed synthetic paths.
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status || response.Header.Get(echo.HeaderCacheControl) != policy {
			t.Fatalf("%s %s: status=%d cache=%q want=%d/%q", method, path, response.StatusCode, response.Header.Get(echo.HeaderCacheControl), status, policy)
		}
		if response.Header.Get(echo.HeaderXRequestID) == "" || (status >= 400 && response.Header.Get(echo.HeaderContentType) != "application/problem+json") {
			t.Fatal("server request ID or Problem contract missing")
		}
		if strings.Contains(string(body), "SECRET") {
			t.Fatal("private storage or account diagnostic leaked")
		}
		if status >= 400 {
			for _, private := range []string{"private-device-id", "Synthetic private computer", "authentication_unavailable"} {
				if strings.Contains(string(body), private) {
					t.Fatal("device information leaked in a rejected response")
				}
			}
		}
		return body
	}
	for name, token := range map[string]string{
		"anonymous": "", "malformed": "invalid",
		"bad_signature": sign(owner, "wrong-key", time.Minute),
		"expired":       sign(owner, secret, -time.Minute), "revoked": sign(revoked, secret, time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			for _, path := range []string{base, base + "/", base + "/unknown", base + "/primary", base + "/remotes/private-device-id", account, account + "/", account + "/unknown", account + "/agenvo/discover", account + "/private-device-id/agenvo-agent"} {
				request(t, http.MethodGet, path, token, 401, "no-store")
			}
		})
	}
	if targets.reads.Load() != 0 || store.reads.Load() != 0 {
		t.Fatal("rejected authentication reached device services")
	}
	valid := sign(owner, secret, time.Minute)
	request(t, http.MethodGet, base, sign(other, secret, time.Minute), 403, "no-store")
	request(t, http.MethodGet, strings.Replace(base, privateDeviceBot, other, 1), valid, 404, "no-store")
	if targets.reads.Load() != 0 {
		t.Fatal("denied Bot access reached device observation")
	}
	var listed workspace.WorkspaceTargetsResponse
	if err := json.Unmarshal(request(t, http.MethodGet, base, valid, 200, "no-store"), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Targets) != 1 || listed.Targets[0].Status != "authentication_unavailable" || listed.Targets[0].Online {
		t.Fatal("cache policy changed authentication-unavailable device status")
	}
	var runtimes []userruntime.Runtime
	if err := json.Unmarshal(request(t, http.MethodGet, account, valid, 200, "no-store"), &runtimes); err != nil || len(runtimes) != 1 {
		t.Fatalf("account runtime projection failed: %v", err)
	}
	for _, path := range []string{base, account} {
		request(t, http.MethodPatch, path, valid, 405, "no-store")
		request(t, http.MethodGet, path+"/unknown/extra", valid, 404, "no-store")
		request(t, http.MethodGet, path+"/", valid, 404, "no-store")
	}
	// Echo routes on its escaped path: dynamic segments stay in scope, while
	// escaped static names and lookalike prefixes are distinct rejected paths.
	for _, bot := range []string{"%31" + privateDeviceBot[1:], "%2F", "%252F"} {
		path := "/bots/" + bot + "/workspace-targets/unknown/extra"
		request(t, http.MethodGet, path, "", 401, "no-store")
		request(t, http.MethodGet, path, valid, 404, "no-store")
	}
	// Echo accepts an empty dynamic segment and the handler rejects it as 400;
	// it remains inside this private namespace, including its pre-JWT response.
	request(t, http.MethodGet, "/bots//workspace-targets", "", 401, "no-store")
	request(t, http.MethodGet, "/bots//workspace-targets", valid, 400, "no-store")
	for _, path := range []string{base + "-extra", account + "-extra", "/other" + base, "/users/me/computer-access", "/runtimes", "/bots/" + privateDeviceBot + "/%77orkspace-targets", "/users/me/%72untimes", "/users/%6de/runtimes"} {
		request(t, http.MethodGet, path, "", 401, "")
		request(t, http.MethodGet, path, valid, 404, "")
	}
	request(t, http.MethodGet, "/bots/"+privateDeviceBot+"/sessions", valid, 200, "private, max-age=60")
	if targets.reads.Load() != 1 || store.reads.Load() != 2 {
		t.Fatal("routing/auth failures unexpectedly reached device services")
	}
}
