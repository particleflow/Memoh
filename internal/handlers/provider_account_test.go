package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/providers"
	"github.com/felinics/memoh/internal/server"
)

const (
	providerBoundarySecret = "synthetic-provider-boundary-signing-key"
	providerBoundaryKey    = "synthetic-provider-credential"
)

type providerBoundaryStore struct {
	dbstore.Queries // Unexpected queries/writes fail the test instead of succeeding.
	provider        sqlc.Provider
	reads, writes   int
	foreign         bool
}

func (q *providerBoundaryStore) GetProviderByID(context.Context, pgtype.UUID) (sqlc.Provider, error) {
	q.reads++
	if q.foreign {
		return sqlc.Provider{}, pgx.ErrNoRows
	}
	return q.provider, nil
}

func (q *providerBoundaryStore) GetProviderByName(context.Context, string) (sqlc.Provider, error) {
	q.reads++
	return q.provider, nil
}

func (q *providerBoundaryStore) ListProviders(context.Context) ([]sqlc.Provider, error) {
	q.reads++
	return []sqlc.Provider{q.provider}, nil
}

func (q *providerBoundaryStore) UpdateProvider(_ context.Context, p sqlc.UpdateProviderParams) (sqlc.Provider, error) {
	q.writes++
	q.provider.Config = p.Config
	q.provider.Name = p.Name
	return q.provider, nil
}

func (q *providerBoundaryStore) CreateProvider(_ context.Context, p sqlc.CreateProviderParams) (sqlc.Provider, error) {
	q.writes++
	return sqlc.Provider{ID: q.provider.ID, Name: p.Name, ClientType: p.ClientType, Config: p.Config, Enable: p.Enable}, nil
}

func (q *providerBoundaryStore) DeleteProvider(context.Context, pgtype.UUID) error {
	q.writes++
	return nil
}

func (q *providerBoundaryStore) ListModelsByProviderID(context.Context, pgtype.UUID) ([]sqlc.Model, error) {
	q.reads++
	return []sqlc.Model{}, nil
}

func newProviderBoundary(t *testing.T) (*echo.Echo, *providerBoundaryStore, *int, *int) {
	t.Helper()
	calls, authenticated, validated := 0, 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") == "Bearer "+providerBoundaryKey {
			authenticated++
		}
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	t.Cleanup(upstream.Close)
	cfg, _ := json.Marshal(map[string]string{"base_url": upstream.URL + "/v1", "api_key": providerBoundaryKey})
	q := &providerBoundaryStore{provider: sqlc.Provider{ID: pgtype.UUID{Bytes: [16]byte{3}, Valid: true}, Name: "synthetic", ClientType: "openai-completions", Enable: true, Config: cfg}}
	log := slog.New(slog.DiscardHandler)
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(log)
	e.Use(auth.JWTMiddleware(providerBoundarySecret, func(c echo.Context) bool {
		return c.Request().URL.Path == "/auth/callback" || c.Request().URL.Path == "/providers/oauth/callback"
	}, func(_ context.Context, user string) error {
		validated++
		if user != "active-account" {
			return errors.New("inactive synthetic account")
		}
		return nil
	}))
	service := providers.NewService(log, q, "")
	NewProvidersHandler(log, service, models.NewService(log, q)).Register(e)
	NewProviderOAuthHandler(service).Register(e)
	e.GET("/synthetic-chat", func(c echo.Context) error {
		_, err := auth.ChatTokenFromContext(c)
		if err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	})
	t.Cleanup(func() {
		require.Equal(t, calls, authenticated, "only the original synthetic credential should be used")
	})
	return e, q, &calls, &validated
}

func providerBoundaryToken(t *testing.T, kind string) string {
	t.Helper()
	var token string
	var err error
	switch kind {
	case "anonymous":
		return ""
	case "chat", "chat-with-account-id":
		user := "synthetic-channel-identity"
		if kind == "chat-with-account-id" {
			user = "active-account"
		}
		token, _, err = auth.GenerateChatToken(auth.ChatToken{BotID: "synthetic-bot", ChatID: "synthetic-chat", RouteID: "synthetic-route", UserID: user}, providerBoundarySecret, time.Minute)
	case "expired":
		token, _, err = auth.GenerateToken("active-account", providerBoundarySecret, time.Nanosecond)
	default:
		token, _, err = auth.GenerateToken(kind, providerBoundarySecret, time.Minute)
	}
	require.NoError(t, err)
	return token
}

func providerBoundaryRequest(e *echo.Echo, method, path, body, token string, query bool) *httptest.ResponseRecorder {
	if query {
		join := "?"
		if strings.Contains(path, "?") {
			join = "&"
		}
		path += join + "token=" + url.QueryEscape(token)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" && !query {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// Before the fix this regression fails with PUT=200, test=200, one DB write
// and one credential-bearing request to a second, strictly local fixture.
func TestProviderAccountRejectsDestinationChangeAndProbe(t *testing.T) {
	e, q, calls, validated := newProviderBoundary(t)
	captured := 0
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer "+providerBoundaryKey {
			captured++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer sink.Close()
	before := append([]byte(nil), q.provider.Config...)
	body, _ := json.Marshal(map[string]any{"config": map[string]string{"base_url": sink.URL + "/v1"}})
	token := providerBoundaryToken(t, "chat")
	put := providerBoundaryRequest(e, http.MethodPut, "/providers/"+q.provider.ID.String(), string(body), token, false)
	probe := providerBoundaryRequest(e, http.MethodPost, "/providers/"+q.provider.ID.String()+"/test", "", token, false)
	t.Logf("synthetic chain: PUT=%d test=%d DBwrites=%d original_upstream=%d alternate_credential_requests=%d account_validations=%d", put.Code, probe.Code, q.writes, *calls, captured, *validated)
	require.Equal(t, http.StatusForbidden, put.Code)
	require.Equal(t, http.StatusForbidden, probe.Code)
	require.Zero(t, q.writes)
	require.Zero(t, *calls)
	require.Zero(t, captured)
	require.Zero(t, *validated)
	require.Equal(t, before, q.provider.Config)
}

func TestProviderAccountEveryManagementRoute(t *testing.T) {
	inventory, _, _, _ := newProviderBoundary(t)
	routes := []*echo.Route{}
	for _, route := range inventory.Routes() {
		if strings.HasPrefix(route.Path, "/providers") && !strings.HasSuffix(route.Path, "/callback") && route.Method != "echo_route_not_found" {
			routes = append(routes, route)
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Method+routes[i].Path < routes[j].Method+routes[j].Path })
	require.Len(t, routes, 16, "12 provider routes (including media) and 4 OAuth management routes; audit additions")
	for _, route := range routes {
		for _, kind := range []string{"chat", "chat-with-account-id", "anonymous", "revoked", "expired"} {
			for _, query := range []bool{false, true} {
				name := route.Method + route.Path + "/" + kind
				if query {
					name += "/query"
				}
				t.Run(name, func(t *testing.T) {
					e, q, calls, validated := newProviderBoundary(t)
					before := append([]byte(nil), q.provider.Config...)
					path := strings.ReplaceAll(strings.ReplaceAll(route.Path, ":id", q.provider.ID.String()), ":name", "synthetic")
					if strings.HasSuffix(path, "/media-catalog") {
						path += "?protocol=magpie"
					}
					rec := providerBoundaryRequest(e, route.Method, path, `{"name":"synthetic-change","client_type":"openai-completions","config":{"base_url":"https://must-not-contact.invalid/v1"}}`, providerBoundaryToken(t, kind), query)
					want := http.StatusUnauthorized
					if strings.HasPrefix(kind, "chat") {
						want = http.StatusForbidden
						require.Zero(t, *validated)
					}
					require.Equal(t, want, rec.Code)
					require.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
					require.NotContains(t, rec.Body.String(), providerBoundaryKey)
					require.Zero(t, q.reads)
					require.Zero(t, q.writes)
					require.Zero(t, *calls)
					require.Equal(t, before, q.provider.Config)
				})
			}
		}
	}
}

func TestProviderAccountNormalManagementAndChat(t *testing.T) {
	for _, operation := range []string{"list", "detail", "name", "models", "count", "create", "update", "delete", "import", "test"} {
		t.Run(operation, func(t *testing.T) {
			e, q, calls, validated := newProviderBoundary(t)
			method, path, body, want := http.MethodGet, "/providers", "", http.StatusOK
			switch operation {
			case "detail":
				path += "/" + q.provider.ID.String()
			case "name":
				path += "/name/synthetic"
			case "models":
				path += "/" + q.provider.ID.String() + "/models"
			case "count":
				path += "/count"
			case "create":
				method = http.MethodPost
				body = `{"name":"synthetic-new","client_type":"openai-completions"}`
				want = http.StatusCreated
			case "update":
				method = http.MethodPut
				path += "/" + q.provider.ID.String()
				body = `{"name":"synthetic-updated"}`
			case "delete":
				method = http.MethodDelete
				path += "/" + q.provider.ID.String()
				want = http.StatusNoContent
			case "import":
				method = http.MethodPost
				path += "/" + q.provider.ID.String() + "/import-models"
				body = `{}`
			case "test":
				method = http.MethodPost
				path += "/" + q.provider.ID.String() + "/test"
			}
			rec := providerBoundaryRequest(e, method, path, body, providerBoundaryToken(t, "active-account"), false)
			require.Equal(t, want, rec.Code, rec.Body.String())
			require.Equal(t, 1, *validated)
			require.NotContains(t, rec.Body.String(), providerBoundaryKey)
			if operation == "test" || operation == "import" {
				require.Positive(t, *calls)
			} else {
				require.Zero(t, *calls)
			}
			if operation == "create" || operation == "update" || operation == "delete" {
				require.Equal(t, 1, q.writes)
			} else {
				require.Zero(t, q.writes)
			}
		})
	}
	t.Run("foreign-team", func(t *testing.T) {
		e, q, calls, _ := newProviderBoundary(t)
		q.foreign = true
		rec := providerBoundaryRequest(e, http.MethodGet, "/providers/"+q.provider.ID.String(), "", providerBoundaryToken(t, "active-account"), false)
		require.Equal(t, http.StatusNotFound, rec.Code)
		require.Zero(t, q.writes)
		require.Zero(t, *calls)
	})
	t.Run("chat-still-accepted-on-chat-route", func(t *testing.T) {
		e, q, calls, validated := newProviderBoundary(t)
		before := q.provider
		rec := providerBoundaryRequest(e, http.MethodGet, "/synthetic-chat", "", providerBoundaryToken(t, "chat"), false)
		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Zero(t, *validated)
		require.Zero(t, *calls)
		require.True(t, reflect.DeepEqual(before, q.provider))
	})
}

func TestProviderAccountOAuthCallbacksKeepStateBoundary(t *testing.T) {
	for _, path := range []string{"/auth/callback", "/providers/oauth/callback"} {
		t.Run(path, func(t *testing.T) {
			e, q, calls, validated := newProviderBoundary(t)
			rec := providerBoundaryRequest(e, http.MethodGet, path+"?code=synthetic-code", "", "", false)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), `"field":"state"`)
			require.Zero(t, *validated)
			require.Zero(t, q.reads)
			require.Zero(t, q.writes)
			require.Zero(t, *calls)
		})
	}
}

// Existing error/probe fixtures now enter provider management as a signed,
// session-validated synthetic account; they still exercise the real guard.
func authorizeProviderTestRequest(t *testing.T, e *echo.Echo, req *http.Request) {
	t.Helper()
	if !strings.HasPrefix(req.URL.Path, "/providers") {
		return
	}
	e.Use(auth.JWTMiddleware(providerBoundarySecret, nil, func(_ context.Context, user string) error {
		if user != "active-account" {
			return errors.New("inactive synthetic account")
		}
		return nil
	}))
	req.Header.Set("Authorization", "Bearer "+providerBoundaryToken(t, "active-account"))
}
