package handlers

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/labstack/echo/v4"
)

type directoryHTTPFixture struct{ *taskHTTPFixture }

func (f directoryHTTPFixture) ListBotTaskDirectoryCandidates(_ context.Context, q dbstore.SessionTaskFilter) ([]dbstore.BotTaskDirectoryCandidate, error) {
	f.reads++
	if q.ID != "" && (q.ID != taskOp || q.Type != "native_agent") {
		return nil, nil
	}
	return []dbstore.BotTaskDirectoryCandidate{{Type: "native_agent", ID: "bg_original", RecordID: taskOp, SessionID: taskChild, SourceSessionID: taskRoot, SourceBotID: taskBot, RunID: taskOp, CreatedAt: time.Now().Add(-time.Minute), State: "unknown", Name: "Original work"}}, nil
}

func TestBotTaskDirectoryHTTPAccountRevocationAndNoStore(t *testing.T) {
	f := directoryHTTPFixture{&taskHTTPFixture{chat: true}}
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
