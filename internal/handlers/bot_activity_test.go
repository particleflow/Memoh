package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/sessiontask"
)

type activityHTTPFixture struct{ *taskHTTPFixture }

func (f activityHTTPFixture) ListSessionActivityCandidates(context.Context, dbstore.SessionTaskFilter) ([]dbstore.SessionTaskCandidate, error) {
	f.reads++
	return []dbstore.SessionTaskCandidate{{Type: "session_run", ID: taskOp, SessionID: taskRoot, CreatedAt: time.Now().Add(-time.Minute)}}, nil
}

func (activityHTTPFixture) ReadSubagentRun(context.Context, string, string) (dbstore.SubagentRun, error) {
	return dbstore.SubagentRun{BotID: taskBot, SessionID: taskRoot, RunID: taskOp, State: "running"}, nil
}

func TestBotActivityHTTPOriginalPermissionAndAccountBoundary(t *testing.T) {
	f := activityHTTPFixture{&taskHTTPFixture{chat: true}}
	h := NewSessionTaskHandler(sessiontask.NewService(f, f, taskHTTPWorkdirs{}, nil, "test-secret"), bots.NewService(nil, f), newTestAdminAccountService("user"), nil)
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.Default())
	e.Use(auth.JWTMiddleware("test-secret", func(echo.Context) bool { return false }))
	h.Register(e)
	account, _, err := auth.GenerateToken(taskUser, "test-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	chat, _, err := auth.GenerateChatToken(auth.ChatToken{BotID: taskBot, ChatID: taskRoot, UserID: taskUser}, "test-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	call := func(token, path string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != want || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "SECRET") {
			t.Fatal("diagnostic leak")
		}
	}
	path := "/bots/" + taskBot + "/activity"
	call("", path, 401)
	call(chat, path, 403)
	if f.reads != 0 {
		t.Fatal("unauthorized discovery read")
	}
	call(account, path, 200)
	if f.reads != 1 {
		t.Fatal(f.reads)
	}
	f.chat = false
	call(account, path, 403)
	if f.reads != 1 {
		t.Fatal("revoked discovery read")
	}
	if f.relayCalls != 0 {
		t.Fatal("activity called runtime")
	}
}
