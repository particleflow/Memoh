package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/mcp"
)

type oauthDiagnosticQueries struct {
	dbstore.Queries
	token   sqlc.McpOauthToken
	readErr error
	updates int
}

func (q *oauthDiagnosticQueries) GetMCPOAuthToken(context.Context, pgtype.UUID) (sqlc.McpOauthToken, error) {
	return q.token, q.readErr
}

func (q *oauthDiagnosticQueries) UpdateMCPOAuthTokens(context.Context, sqlc.UpdateMCPOAuthTokensParams) error {
	q.updates++
	return errors.New("SECRET persistence failure")
}

func TestFederationOAuthRefreshLogHasSafeCategoryAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name, body, category, code    string
		status                        int
		readErr                       error
		wantCalls                     int
		wantUpdates                   int
		missingAccess, missingRefresh bool
	}{
		{name: "rejected", status: 400, body: `{"error":"invalid_grant","error_description":"SECRET-body"}`, category: "oauth_rejected", code: "invalid_grant", wantCalls: 1},
		{name: "upstream", status: 503, body: "SECRET-server-error", category: "http_error", wantCalls: 1},
		{name: "malformed", status: 200, body: `{"access_token":`, category: "response_invalid", wantCalls: 1},
		{name: "lookup", readErr: errors.New("SECRET query and private address"), category: "token_unavailable"},
		{name: "access_missing", missingAccess: true, category: "access_token_missing"},
		{name: "refresh_missing", missingRefresh: true, category: "refresh_token_missing"},
		{name: "persist_failure", status: 200, body: `{"access_token":"SECRET-new-access","refresh_token":"SECRET-rotated-refresh","expires_in":3600}`, category: "token_persist_failed", wantCalls: 1, wantUpdates: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.ParseForm() != nil || r.Form.Get("grant_type") != "refresh_token" {
					t.Error("unexpected credential flow")
				}
				w.Header().Set("X-SECRET", "SECRET-header")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer tokens.Close()
			queries := &oauthDiagnosticQueries{readErr: tc.readErr, token: sqlc.McpOauthToken{
				AccessToken: "SECRET-expired", RefreshToken: "SECRET-refresh", ClientID: "SECRET-client", TokenEndpoint: tokens.URL + "/SECRET",
				ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
			}}
			if tc.missingAccess {
				queries.token.AccessToken = ""
			}
			if tc.missingRefresh {
				queries.token.RefreshToken = ""
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			gateway := NewMCPFederationGateway(logger, nil)
			gateway.SetOAuthService(mcp.NewOAuthService(logger, queries, ""))
			gateway.connectionHTTPClient(t.Context(), mcp.Connection{ID: "11111111-1111-4111-8111-111111111111", AuthType: "oauth", Config: map[string]any{
				"url": "https://SECRET-resource.invalid", "headers": map[string]any{"Authorization": "SECRET-static-header"},
			}})
			if int(calls.Load()) != tc.wantCalls || queries.updates != tc.wantUpdates {
				t.Fatal("refresh retried or stored credentials after failure")
			}
			if strings.Contains(logs.String(), "SECRET") || strings.Contains(logs.String(), tokens.URL) {
				t.Fatal("secret/endpoint escaped into gateway log")
			}
			var entry map[string]any
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatalf("expected one diagnostic record: %v", err)
			}
			if entry["oauth_failure_category"] != tc.category {
				t.Fatalf("wrong category: %s", &logs)
			}
			if tc.wantCalls == 1 && tc.wantUpdates == 0 && entry["oauth_http_status"] != float64(tc.status) {
				t.Fatal("HTTP status was lost at the gateway")
			}
			if tc.code != "" && entry["oauth_error_code"] != tc.code {
				t.Fatal("OAuth category was lost at the gateway")
			}
			if _, exists := entry["error"]; exists {
				t.Fatal("gateway stringified raw error")
			}
		})
	}
}
