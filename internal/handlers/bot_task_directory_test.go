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
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/sessiontask"
)

type directoryHTTPFixture struct {
	*taskHTTPFixture
	permissionReadError bool
	denyDuringRead      bool
}

func (directoryHTTPFixture) ReadBotTaskRun(context.Context, string, string, string) (dbstore.BotTaskRunReference, error) {
	return dbstore.BotTaskRunReference{BotID: taskBot, SessionID: taskChild, RunID: taskOp, TurnID: taskOp, InvocationID: "subagent:bg_original"}, nil
}

func (f directoryHTTPFixture) ListBotUserGrantsForUser(ctx context.Context, p sqlc.ListBotUserGrantsForUserParams) ([]sqlc.ListBotUserGrantsForUserRow, error) {
	if f.reads > 0 {
		if f.permissionReadError {
			return nil, errors.New("SECRET permission storage unavailable")
		}
		if f.denyDuringRead {
			return nil, nil
		}
	}
	return f.taskHTTPFixture.ListBotUserGrantsForUser(ctx, p)
}

func (f directoryHTTPFixture) ListBotTaskDirectoryCandidates(_ context.Context, q dbstore.SessionTaskFilter) ([]dbstore.BotTaskDirectoryCandidate, error) {
	f.reads++
	if q.ID != "" && (q.ID != taskOp || q.Type != "native_agent") {
		return nil, nil
	}
	return []dbstore.BotTaskDirectoryCandidate{{Type: "native_agent", ID: "bg_original", RecordID: taskOp, SessionID: taskChild, SourceSessionID: taskRoot, SourceBotID: taskBot, RunID: taskOp, CreatedAt: time.Now().Add(-time.Minute), State: "unknown", Name: "Original work"}}, nil
}

func TestBotTaskDirectoryHTTPAccountRevocationAndNoStore(t *testing.T) {
	f := directoryHTTPFixture{taskHTTPFixture: &taskHTTPFixture{chat: true}}
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
	call := func(token, method, path string, want int) string {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
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
		return rec.Body.String()
	}
	path := "/bots/" + taskBot + "/tasks"
	detail := path + "/native_agent/" + taskOp
	for _, p := range []string{path, detail} {
		call("", "GET", p, 401)
		call(chat, "GET", p, 403)
	}
	if f.reads != 0 {
		t.Fatal("unauthorized discovery")
	}
	for _, p := range []string{path, detail} {
		body := call(account, "GET", p, 200)
		if !strings.Contains(body, "bg_original") || !strings.Contains(body, "unknown") || !strings.Contains(body, taskChild) {
			t.Fatal(body)
		}
	}
	reads := f.reads
	call(account, "GET", path+"?limit=101", 400)
	call(account, "GET", path+"/native_agent/not-a-uuid", 404)
	call(account, "GET", path+"/configuration/"+taskOp, 404)
	call(account, "POST", detail, 405)
	if f.reads != reads {
		t.Fatal("invalid input read store")
	}
	f.chat = false
	for _, p := range []string{path, detail} {
		call(account, "GET", p, 403)
	}
	if f.reads != reads || f.relayCalls != 0 {
		t.Fatal("revocation read or runtime dispatch")
	}
}

func TestBotTaskDirectoryPermissionReadFailureIsNotEmptySuccess(t *testing.T) {
	for _, detail := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			f := directoryHTTPFixture{taskHTTPFixture: &taskHTTPFixture{chat: true}, permissionReadError: failed, denyDuringRead: !failed}
			h := NewSessionTaskHandler(sessiontask.NewService(f, f, taskHTTPWorkdirs{}, nil, "test-secret"), bots.NewService(nil, f), newTestAdminAccountService("user"), nil)
			e := echo.New()
			e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.Default())
			e.Use(auth.JWTMiddleware("test-secret", func(echo.Context) bool { return false }))
			h.Register(e)
			token, _, err := auth.GenerateToken(taskUser, "test-secret", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			path := "/bots/" + taskBot + "/tasks"
			if detail {
				path += "/native_agent/" + taskOp
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			want := 200
			if detail {
				want = 404
			}
			if failed {
				want = 500
			}
			if rec.Code != want || strings.Contains(rec.Body.String(), "SECRET") {
				t.Fatalf("detail=%v failed=%v: %d %s", detail, failed, rec.Code, rec.Body.String())
			}
			if !failed && !detail && !strings.Contains(rec.Body.String(), `"items":[]`) {
				t.Fatal(rec.Body.String())
			}
		}
	}
}
