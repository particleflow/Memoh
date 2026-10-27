package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/server"
)

type contactRecorder struct {
	calls     int
	bot, user string
	err       error
	after     func()
}

func (r *contactRecorder) EnsurePrivateChat(_ context.Context, bot, user string) (thread.Thread, error) {
	r.calls++
	r.bot, r.user = bot, user
	if r.after != nil {
		r.after()
	}
	return thread.Thread{ID: invocationTestSessionA, BotID: bot, CreatedByUserID: user}, r.err
}

func TestPrivateChatHTTPReauthorizesChangedDescriptor(t *testing.T) {
	for _, tc := range []struct {
		name, mode, runtime, grants, creator string
		revokeAfterResolve                   bool
		want                                 int
	}{
		{name: "model", mode: "chat", runtime: "model", grants: `["chat"]`, want: 200},
		{name: "discuss", mode: "discuss", runtime: "model", grants: `["chat","manage"]`, want: 200},
		{name: "external", mode: "chat", runtime: "codex", grants: `["chat","workspace_exec"]`, want: 200},
		{name: "revoked_external_permission", mode: "chat", runtime: "codex", grants: `["chat"]`, want: 404},
		{name: "foreign_contact", mode: "chat", runtime: "model", creator: invocationTestOtherUser, grants: `["chat"]`, want: 404},
		{name: "revoked_during_resolution", mode: "chat", runtime: "model", grants: `["chat"]`, revokeAfterResolve: true, want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creator := tc.creator
			if creator == "" {
				creator = invocationTestOwnerID
			}
			s := invocationTestSession(invocationTestSessionA, creator)
			s.Type, s.SessionMode, s.RuntimeType = tc.mode, tc.mode, tc.runtime
			q := newInvocationTestQueries(s)
			q.permissions = []byte(tc.grants)
			h := newInvocationTestHandler(q, nil)
			resolver := &contactRecorder{}
			if tc.revokeAfterResolve {
				resolver.after = func() { q.permissions = []byte(`[]`) }
			}
			h.SetPrivateChatResolver(resolver)
			e := echo.New()
			e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
			rec := httptest.NewRecorder()
			c := testAuthContext(e, httptest.NewRequest(http.MethodPost, "/bots/"+invocationTestBotID+"/sessions/contact", nil), rec, invocationTestOwnerID)
			c.SetParamNames("bot_id")
			c.SetParamValues(invocationTestBotID)
			if err := h.ResolvePrivateChat(c); err != nil {
				e.HTTPErrorHandler(err, c)
			}
			if rec.Code != tc.want || resolver.calls != 1 {
				t.Fatalf("status=%d want=%d calls=%d body=%s", rec.Code, tc.want, resolver.calls, rec.Body.String())
			}
		})
	}
}

func TestPrivateChatHTTPUsesOnlyAuthenticatedAccount(t *testing.T) {
	for _, scenario := range []string{"account", "anonymous", "revoked_account", "chat_route", "revoked_bot", "injected_identity", "invalid_bot", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			const secret = "synthetic-contact-http-secret"
			q := newInvocationTestQueries(invocationTestSession(invocationTestSessionA, invocationTestOwnerID))
			h := newInvocationTestHandler(q, nil)
			resolver := &contactRecorder{}
			h.SetPrivateChatResolver(resolver)
			e := echo.New()
			e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
			e.Use(auth.JWTMiddleware(secret, nil, func(context.Context, string) error {
				if scenario == "revoked_account" {
					return errors.New("synthetic revoked account")
				}
				return nil
			}))
			h.Register(e)
			token, _, err := auth.GenerateToken(invocationTestOwnerID, secret, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			bot, body, want := invocationTestBotID, "", http.StatusOK
			switch scenario {
			case "anonymous":
				token, want = "", http.StatusUnauthorized
			case "revoked_account":
				want = http.StatusUnauthorized
			case "chat_route":
				token, _, err = auth.GenerateChatToken(auth.ChatToken{BotID: bot, ChatID: bot, UserID: invocationTestOwnerID}, secret, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				want = http.StatusForbidden
			case "revoked_bot":
				q.permissions, want = []byte(`[]`), http.StatusForbidden
			case "injected_identity":
				body, want = `{"user_id":"another-account","route_id":"another-route"}`, http.StatusBadRequest
			case "invalid_bot":
				bot, want = "invalid", http.StatusBadRequest
			case "unavailable":
				resolver.err, want = route.ErrContactUnavailable, http.StatusServiceUnavailable
			}
			req := httptest.NewRequest(http.MethodPost, "/bots/"+bot+"/sessions/contact", strings.NewReader(body))
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
			}
			if rec.Header().Get("Cache-Control") != "no-store" && scenario != "anonymous" && scenario != "revoked_account" {
				t.Fatal("contact response must not be cached")
			}
			if want == http.StatusOK || scenario == "unavailable" {
				if resolver.calls != 1 || resolver.bot != invocationTestBotID || resolver.user != invocationTestOwnerID {
					t.Fatal("wrong authoritative contact scope")
				}
			} else if resolver.calls != 0 {
				t.Fatal("rejected request reached contact creation")
			}
		})
	}
}
