package handlers

import (
	"bytes"
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

	"github.com/jackc/pgx/v5/pgtype"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/mcp"
)

type oauthBoundaryQueries struct {
	dbstore.Queries
	token        sqlc.McpOauthToken
	updates      int
	persistError bool
}

func (q *oauthBoundaryQueries) GetMCPOAuthToken(context.Context, pgtype.UUID) (sqlc.McpOauthToken, error) {
	return q.token, nil
}

func (q *oauthBoundaryQueries) UpdateMCPOAuthTokens(_ context.Context, p sqlc.UpdateMCPOAuthTokensParams) error {
	q.updates++
	if q.persistError {
		return errors.New("SECRET persistence failure")
	}
	q.token.AccessToken = p.AccessToken
	q.token.RefreshToken = p.RefreshToken
	q.token.ExpiresAt = p.ExpiresAt
	return nil
}

func TestFederationOAuthFailureNeverConnectsMCP(t *testing.T) {
	for _, transport := range []string{"http", "sse"} {
		for _, fault := range []string{"invalid_grant", "upstream", "malformed", "missing_token", "missing_refresh", "missing_service", "persist"} {
			t.Run(transport+"/"+fault, func(t *testing.T) {
				var refreshCalls, mcpCalls atomic.Int32
				tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					refreshCalls.Add(1)
					switch fault {
					case "invalid_grant":
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"SECRET"}`))
					case "upstream":
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = w.Write([]byte("SECRET"))
					case "malformed":
						_, _ = w.Write([]byte(`{"access_token":`))
					default:
						_, _ = w.Write([]byte(`{"access_token":"SECRET-valid","refresh_token":"SECRET-rotated","expires_in":3600}`))
					}
				}))
				defer tokens.Close()
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { mcpCalls.Add(1); w.WriteHeader(http.StatusBadRequest) }))
				defer remote.Close()
				q := &oauthBoundaryQueries{token: sqlc.McpOauthToken{AccessToken: "SECRET-expired", RefreshToken: "SECRET-refresh", TokenEndpoint: tokens.URL, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}}, persistError: fault == "persist"}
				if fault == "missing_token" {
					q.token.AccessToken = ""
				}
				if fault == "missing_refresh" {
					q.token.RefreshToken = ""
				}
				before, _ := json.Marshal(q.token)
				var logs bytes.Buffer
				g := NewMCPFederationGateway(slog.New(slog.NewJSONHandler(&logs, nil)), nil)
				if fault != "missing_service" {
					g.SetOAuthService(mcp.NewOAuthService(g.logger, q, ""))
				}
				conn := mcp.Connection{ID: "11111111-1111-4111-8111-111111111111", AuthType: "oauth", Config: map[string]any{"url": remote.URL, "sse_url": remote.URL + "/sse", "headers": map[string]any{"authorization": "SECRET-static"}}}
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				var err error
				if transport == "http" {
					_, err = g.CallHTTPConnectionTool(ctx, conn, "echo", map[string]any{"query": "synthetic"})
				} else {
					_, err = g.CallSSEConnectionTool(ctx, conn, "echo", nil)
				}
				if err == nil || mcpCalls.Load() != 0 {
					t.Fatalf("OAuth failure reached MCP: calls=%d err=%v", mcpCalls.Load(), err)
				}
				wantRefresh := int32(1)
				if strings.HasPrefix(fault, "missing_") {
					wantRefresh = 0
				}
				wantUpdates := 0
				if fault == "persist" {
					wantUpdates = 1
				}
				after, _ := json.Marshal(q.token)
				if refreshCalls.Load() != wantRefresh || q.updates != wantUpdates || !bytes.Equal(before, after) {
					t.Fatal("token changed or refresh retried")
				}
				wantAuth := fault == "invalid_grant" || strings.HasPrefix(fault, "missing_")
				if IsMCPAuthenticationUnavailable(err) != wantAuth {
					t.Fatal("transient failure was confused with unavailable authorization")
				}
				if strings.Contains(logs.String(), "SECRET") || strings.Contains(err.Error(), "SECRET") {
					t.Fatal("secret escaped diagnostic boundary")
				}
			})
		}
	}
}

func TestFederationOAuthRecoveryReadsWithoutReplayingTool(t *testing.T) {
	for _, transport := range []string{"http", "sse"} {
		t.Run(transport, func(t *testing.T) {
			var allowed atomic.Bool
			var tokenCalls, requests, tools atomic.Int32
			tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tokenCalls.Add(1)
				if !allowed.Load() {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
					return
				}
				_, _ = w.Write([]byte(`{"access_token":"synthetic-valid","expires_in":3600}`))
			}))
			defer tokens.Close()
			server := newTestMCPServer()
			var handler http.Handler = sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
			if transport == "sse" {
				handler = sdkmcp.NewSSEHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
			}
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "Bearer synthetic-valid" {
					t.Error("stale or missing authorization")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Method == http.MethodPost {
					var body bytes.Buffer
					_, _ = body.ReadFrom(r.Body)
					_ = r.Body.Close()
					raw := body.Bytes()
					if bytes.Contains(raw, []byte(`"tools/call"`)) {
						tools.Add(1)
					}
					r.Body = io.NopCloser(bytes.NewReader(raw))
				}
				handler.ServeHTTP(w, r)
			}))
			defer remote.Close()
			q := &oauthBoundaryQueries{token: sqlc.McpOauthToken{AccessToken: "synthetic-expired", RefreshToken: "synthetic-refresh", TokenEndpoint: tokens.URL, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}}}
			g := NewMCPFederationGateway(slog.New(slog.DiscardHandler), nil)
			g.SetOAuthService(mcp.NewOAuthService(g.logger, q, ""))
			headers := map[string]string{"authorization": "synthetic-stale-config"}
			conn := mcp.Connection{ID: "11111111-1111-4111-8111-111111111111", AuthType: "oauth", Config: map[string]any{"url": remote.URL, "headers": headers}}
			list := g.ListHTTPConnectionTools
			if transport == "sse" {
				list = g.ListSSEConnectionTools
			}
			if _, err := list(t.Context(), conn); !IsMCPAuthenticationUnavailable(err) || requests.Load() != 0 || q.updates != 0 {
				t.Fatal("rejected initial read reached MCP")
			}
			allowed.Store(true)
			result, err := list(t.Context(), conn)
			if err != nil || len(result) != 1 || result[0].Name != "echo" || q.updates != 1 || tokenCalls.Load() != 2 || requests.Load() == 0 || tools.Load() != 0 {
				t.Fatalf("explicit recovery failed or replayed tool: err=%v tools=%d", err, tools.Load())
			}
			if headers["authorization"] != "synthetic-stale-config" || len(headers) != 1 {
				t.Fatal("request auth mutated stored connection config")
			}
		})
	}
}
