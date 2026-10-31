package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/server"
)

func TestBotDelegationHistoryJWTDoesNotTransferExecutionBinding(t *testing.T) {
	r := botdelegation.Record{
		ID: uuid.NewString(), State: "completed", SourceSelection: "original-selection", CreatedAt: time.Now().Add(-time.Hour),
		Identity: botdelegation.Identity{TeamID: "team", UserID: taskUser, SourceBotID: taskBot, SourceSessionID: taskRoot, SourceRootID: taskRoot, TargetBotID: taskTarget, TargetSessionID: taskOp, SourceSelection: "original-selection"},
	}
	f := &delegationHTTPFixture{allowed: true, row: r}
	a := &botdelegation.Authority{Permissions: f, Sessions: delegationHTTPSessions{f}, Assets: f}
	s := botdelegation.NewService(a, f, f, slog.New(slog.DiscardHandler), time.Minute)
	s.SetCursorSecret("synthetic-history")
	s.Host = f
	t.Cleanup(s.Close)
	h := NewBotDelegationHandler(s, bots.NewService(nil, &taskHTTPFixture{chat: true}), newTestAdminAccountService("user"))
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	e.Use(auth.JWTMiddleware("synthetic-history", func(echo.Context) bool { return false }))
	h.Register(e)
	token, _, err := auth.GenerateToken(taskUser, "synthetic-history", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	base := "/bots/" + taskBot + "/sessions/" + taskRoot + "/delegations"
	call := func(method, path, credential string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != want || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s returned %d: %s", method, path, rec.Code, rec.Body.String())
		}
		return rec
	}
	call(http.MethodGet, base, "", http.StatusUnauthorized)
	response := call(http.MethodGet, base+"?limit=100", token, http.StatusOK)
	var page botdelegation.Page
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.Items[0].TargetBotID != taskTarget || page.Items[0].TargetSessionID != taskOp || page.Items[0].State != "completed" {
		t.Fatalf("original linked history=%+v err=%v", page, err)
	}
	call(http.MethodGet, base+"/"+r.ID, token, http.StatusNotFound)
	call(http.MethodPost, base+"/"+r.ID+"/delivery", token, http.StatusNotFound)
	f.allowed = false
	call(http.MethodGet, base, token, http.StatusNotFound)
	if f.creates.Load()+f.runs.Load()+f.deliveries.Load() != 0 || f.row.Revision != r.Revision || f.row.SourceSelection != r.SourceSelection {
		t.Fatal("history request changed or executed the original handoff")
	}
}
