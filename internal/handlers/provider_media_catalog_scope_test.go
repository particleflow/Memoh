package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/providers"
	"github.com/felinics/memoh/internal/server"
)

// Use the actual Server middleware stack, not a handler-only auth simulation.
// The only substituted dependencies are the provider query and HTTP catalog.
func TestProviderMediaCatalogAccountScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		chat    bool
		revoked bool
		query   bool
		user    string
		status  int
	}{
		{name: "active account", user: "synthetic-account", status: http.StatusOK},
		{name: "revoked account", user: "synthetic-account", revoked: true, status: http.StatusUnauthorized},
		{name: "chat route with active account", user: "synthetic-account", chat: true, status: http.StatusForbidden},
		{name: "chat route with revoked account", user: "synthetic-account", chat: true, revoked: true, status: http.StatusForbidden},
		{name: "chat route channel identity", user: "synthetic-channel-identity", chat: true, status: http.StatusForbidden},
		{name: "chat route query credential", user: "synthetic-account", chat: true, query: true, status: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("X-Magpie-Videomakers") != "1" {
					t.Error("unexpected catalog request")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			defer upstream.Close()
			cfg, err := json.Marshal(map[string]string{"base_url": upstream.URL + "/v1"})
			if err != nil {
				t.Fatal(err)
			}
			queries := &mediaCatalogHandlerQueries{provider: sqlc.Provider{
				ID: pgtype.UUID{Bytes: [16]byte{3}, Valid: true}, Enable: true,
				ClientType: "openai-completions", Config: cfg,
			}}
			const secret = "synthetic-catalog-scope-signing-secret"
			log := slog.New(slog.DiscardHandler)
			validated := 0
			srv := server.NewServerWithSessionValidator(log, "", secret, func(_ context.Context, user string) error {
				validated++
				if user != tc.user {
					t.Error("wrong account reached validator")
				}
				if tc.revoked {
					return pgx.ErrNoRows
				}
				return nil
			}, NewProvidersHandler(log, providers.NewService(log, queries, ""), nil))
			token, _, err := auth.GenerateToken(tc.user, secret, time.Minute)
			if tc.chat {
				token, _, err = auth.GenerateChatToken(auth.ChatToken{
					BotID: "synthetic-bot", ChatID: "synthetic-chat", RouteID: "synthetic-route", UserID: tc.user,
				}, secret, time.Minute)
			}
			if err != nil {
				t.Fatal(err)
			}
			path := "/providers/" + queries.provider.ID.String() + "/media-catalog?protocol=magpie"
			if tc.query {
				path += "&token=" + url.QueryEscape(token)
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if !tc.query {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			t.Logf("status=%d account_validations=%d provider_reads=%d upstream_GETs=%d", rec.Code, validated, queries.reads, upstreamCalls.Load())
			if rec.Code != tc.status {
				t.Errorf("status=%d, want=%d", rec.Code, tc.status)
			}
			wantValidations, wantCalls := 1, 0
			if tc.chat {
				wantValidations = 0 // Existing JWT middleware deliberately treats these as route credentials.
			}
			if tc.status == http.StatusOK {
				wantCalls = 1
			}
			if validated != wantValidations || queries.reads != wantCalls || upstreamCalls.Load() != int32(wantCalls) {
				t.Errorf("wanted validations=%d and provider/upstream calls=%d", wantValidations, wantCalls)
			}
			if tc.chat && rec.Header().Get("Cache-Control") != "no-store" {
				t.Error("route-scope denial must not be cached")
			}
			if tc.chat {
				var problem server.Problem
				if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil || problem.Code != "http.forbidden" {
					t.Error("expected existing machine-readable forbidden response")
				}
			}
		})
	}
}
