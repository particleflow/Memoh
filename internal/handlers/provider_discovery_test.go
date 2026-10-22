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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/providers"
	"github.com/felinics/memoh/internal/server"
)

type discoveryHTTPQueries struct {
	dbstore.Queries // All writes and OAuth operations panic if called.
	provider        sqlc.Provider
	reads           atomic.Int32
}

func (q *discoveryHTTPQueries) GetProviderByID(_ context.Context, id pgtype.UUID) (sqlc.Provider, error) {
	q.reads.Add(1)
	if id != q.provider.ID {
		return sqlc.Provider{}, pgx.ErrNoRows
	}
	return q.provider, nil
}

type captureDiscoveryHandler struct {
	handler *ProvidersHandler
	echo    *echo.Echo
}

func (h *captureDiscoveryHandler) Register(e *echo.Echo) {
	h.echo = e
	h.handler.Register(e)
}

func TestProviderDiscoveryRealHTTPAuthAndSanitization(t *testing.T) {
	const (
		owner  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		id     = "11111111-1111-4111-8111-111111111111"
		secret = "synthetic-discovery-jwt-secret" //nolint:gosec // Test-only signing key.
		path   = "/providers/" + id + "/model-discovery"
	)
	var calls atomic.Int32
	var reject atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/models" {
			t.Errorf("not a metadata request: %s %s", r.Method, r.URL.Path)
		}
		if reject.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"SECRET upstream key/private endpoint"}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"synthetic-image-edit","secret":"SECRET"}]}`)
	}))
	t.Cleanup(upstream.Close)
	cfg, _ := json.Marshal(map[string]string{"base_url": upstream.URL, "api_key": "SECRET-fixture-key"})
	q := &discoveryHTTPQueries{provider: sqlc.Provider{ID: testUUID(id), ClientType: "openai-completions", Enable: true, Config: cfg}}
	var logs lockedBuffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	capture := &captureDiscoveryHandler{handler: NewProvidersHandler(log, providers.NewService(log, q, ""), nil)}
	_ = server.NewServerWithSessionValidator(log, ":0", secret, func(_ context.Context, user string) error {
		if user != owner {
			return errors.New("SECRET inactive account")
		}
		return nil
	}, capture)
	transport := httptest.NewServer(capture.echo)
	t.Cleanup(transport.Close)
	token, _, err := auth.GenerateToken(owner, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, token string, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, transport.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := transport.Client().Do(req) //nolint:gosec // Fixed routes on a test-owned HTTP server.
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("status/cache = %d/%s; body=%s", resp.StatusCode, resp.Header.Get("Cache-Control"), body)
		}
		for _, forbidden := range []string{"SECRET", upstream.URL, "base_url", "api_key"} {
			if strings.Contains(string(body), forbidden) {
				t.Fatalf("response leaked %s", forbidden)
			}
		}
		return body
	}
	request(path, "", 401)
	request(path, "invalid", 401)
	expired, _, _ := auth.GenerateToken(owner, secret, time.Nanosecond)
	request(path, expired, 401)
	revoked, _, _ := auth.GenerateToken("revoked", secret, time.Minute)
	request(path, revoked, 401)
	chat, _, _ := auth.GenerateChatToken(auth.ChatToken{BotID: id, ChatID: id, UserID: owner}, secret, time.Minute)
	request(path, chat, 403)
	if q.reads.Load() != 0 || calls.Load() != 0 {
		t.Fatal("unauthorized request read provider or contacted upstream")
	}
	request("/providers/22222222-2222-4222-8222-222222222222/model-discovery", token, 404)
	if calls.Load() != 0 {
		t.Fatal("unavailable provider contacted upstream")
	}
	body := request(path, token, 200)
	var result providers.DiscoverModelsResponse
	if json.Unmarshal(body, &result) != nil || len(result.Models) != 1 || result.Models[0].Capabilities[3].Status != "unknown" {
		t.Fatal("wrong unknown capability response")
	}
	reject.Store(true)
	request(path, token, 502)
	if calls.Load() != 2 || strings.Contains(logs.String(), "SECRET") || strings.Contains(logs.String(), upstream.URL) {
		t.Fatalf("extra calls or private error in logs; calls=%d", calls.Load())
	}
}
