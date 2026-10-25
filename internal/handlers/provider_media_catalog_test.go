package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

type mediaCatalogHandlerQueries struct {
	dbstore.Queries
	provider sqlc.Provider
	err      error
	reads    int
}

func (q *mediaCatalogHandlerQueries) GetProviderByID(context.Context, pgtype.UUID) (sqlc.Provider, error) {
	q.reads++
	return q.provider, q.err
}

func TestProviderMediaCatalogBoundary(t *testing.T) {
	for _, mode := range []string{"success", "empty", "forbidden", "unauthorized_upstream", "redirect", "invalid", "anonymous", "revoked", "foreign_team", "wrong_protocol", "bad_id", "not_enabled"} {
		t.Run(mode, func(t *testing.T) {
			upstreamCalls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls++
				if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("X-Magpie-Videomakers") != "1" {
					t.Error("wrong upstream request")
				}
				switch mode {
				case "forbidden":
					w.WriteHeader(http.StatusForbidden)
				case "unauthorized_upstream":
					w.WriteHeader(http.StatusUnauthorized)
				case "redirect":
					w.Header().Set("Location", "https://must-not-contact.invalid")
					w.WriteHeader(http.StatusFound)
				case "invalid":
					w.WriteHeader(http.StatusOK)
				default:
					w.Header().Set("Content-Type", "application/json")
					if mode == "empty" {
						_, _ = w.Write([]byte(`{"data":[]}`))
						return
					}
					_, _ = w.Write([]byte(`{"data":[{"id":"synthetic/video","kind":"video","modalities":{"input":["text"],"output":["video"]}}]}`))
					return
				}
				_, _ = w.Write([]byte("PRIVATE synthetic-key " + upstreamURLPlaceholder))
			}))
			defer upstream.Close()
			cfg, _ := json.Marshal(map[string]string{"base_url": upstream.URL + "/v1", "api_key": "synthetic-key"})
			q := &mediaCatalogHandlerQueries{provider: sqlc.Provider{ID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, Enable: true, ClientType: "openai-completions", Config: cfg}}
			if mode == "foreign_team" {
				q.err = pgx.ErrNoRows
			}
			if mode == "not_enabled" {
				q.provider.Enable = false
			}
			e := echo.New()
			e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
			e.Use(auth.JWTMiddleware("synthetic-test-signing-secret", nil, func(context.Context, string) error {
				if mode == "revoked" {
					return pgx.ErrNoRows
				}
				return nil
			}))
			NewProvidersHandler(slog.New(slog.DiscardHandler), providers.NewService(nil, q, ""), nil).Register(e)
			path := "/providers/" + q.provider.ID.String() + "/media-catalog?protocol=magpie"
			if mode == "wrong_protocol" {
				path = strings.ReplaceAll(path, "protocol=magpie", "protocol=guessed")
			}
			if mode == "bad_id" {
				path = "/providers/not-a-uuid/media-catalog?protocol=magpie"
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if mode != "anonymous" {
				token, _, err := auth.GenerateToken("synthetic-user", "synthetic-test-signing-secret", time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			want := 200
			switch mode {
			case "forbidden", "unauthorized_upstream", "redirect", "invalid":
				want = 502
			case "anonymous", "revoked":
				want = 401
			case "foreign_team":
				want = 404
			case "wrong_protocol", "bad_id", "not_enabled":
				want = 400
			}
			if rec.Code != want {
				t.Fatalf("status=%d body=%s want=%d", rec.Code, rec.Body.String(), want)
			}
			for _, private := range []string{"PRIVATE", "synthetic-key", upstream.URL, upstreamURLPlaceholder} {
				if strings.Contains(rec.Body.String(), private) {
					t.Fatal("private response leaked")
				}
			}
			if mode == "forbidden" && !strings.Contains(rec.Body.String(), "agent.provider_permission_denied") {
				t.Fatal("permission failure lost")
			}
			noUpstream := mode == "anonymous" || mode == "revoked" || mode == "foreign_team" || mode == "wrong_protocol" || mode == "bad_id" || mode == "not_enabled"
			if (noUpstream && upstreamCalls != 0) || (!noUpstream && upstreamCalls != 1) {
				t.Fatalf("upstream calls=%d", upstreamCalls)
			}
			if (mode == "anonymous" || mode == "revoked" || mode == "wrong_protocol" || mode == "bad_id") && q.reads != 0 {
				t.Fatal("unexpected provider read")
			}
			if mode == "success" && rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("catalog must not be cached")
			}
		})
	}
}

const upstreamURLPlaceholder = "https://private-catalog.example.test"
