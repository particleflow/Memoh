package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/workdir"
)

const (
	taskBot    = "10000000-0000-4000-8000-000000000001"
	taskUser   = "10000000-0000-4000-8000-000000000002"
	taskRoot   = "10000000-0000-4000-8000-000000000003"
	taskChild  = "10000000-0000-4000-8000-000000000004"
	taskOp     = "10000000-0000-4000-8000-000000000005"
	taskTarget = "10000000-0000-4000-8000-000000000006"
	taskOwner  = "10000000-0000-4000-8000-000000000007"
)

type taskHTTPFixture struct {
	dbstore.Queries
	dbstore.AgenvoRuntimeStore
	chat, exec, source, grant bool
	reads, relayCalls         int
	registration              agenvo.Target
	op                        dbstore.ComputerOperationRecord
	pcScope                   bool
}

func (*taskHTTPFixture) GetBotByID(context.Context, pgtype.UUID) (sqlc.GetBotByIDRow, error) {
	row := testBotRow(taskBot, map[string]any{})
	row.OwnerUserID = testUUID(taskOwner)
	return row, nil
}

func (f *taskHTTPFixture) ListBotUserGrantsForUser(context.Context, sqlc.ListBotUserGrantsForUserParams) ([]sqlc.ListBotUserGrantsForUserRow, error) {
	if !f.chat {
		return nil, nil
	}
	return []sqlc.ListBotUserGrantsForUserRow{{Permissions: []byte(`["chat"]`)}}, nil
}

func (f *taskHTTPFixture) Get(_ context.Context, id string) (thread.Thread, error) {
	v := thread.Thread{ID: id, BotID: taskBot, CreatedByUserID: taskUser, Type: "chat"}
	if id == taskChild {
		v.Type = "subagent"
		v.ParentThreadID = taskRoot
	} else if id != taskRoot {
		return v, errors.New("SECRET")
	}
	if f.pcScope && id == taskRoot {
		v.WorkdirID = "pc-workdir"
	}
	return v, nil
}

type taskHTTPWorkdirs struct{}

func (*taskHTTPFixture) GetPrimaryMount(context.Context, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	return dbstore.BotRemoteRuntimeBindingRecord{}, db.ErrNotFound
}

func (taskHTTPWorkdirs) Get(context.Context, string, string) (workdir.Workdir, error) {
	return workdir.Workdir{ID: "pc-workdir", BotID: taskBot, WorkspaceTargetID: taskTarget}, nil
}

func (f *taskHTTPFixture) ListSessionTaskCandidates(_ context.Context, q dbstore.SessionTaskFilter) ([]dbstore.SessionTaskCandidate, error) {
	f.reads++
	if q.ID != "" && (q.ID != taskOp || q.Type != "computer_operation") {
		return nil, nil
	}
	return []dbstore.SessionTaskCandidate{{Type: "computer_operation", ID: taskOp, SessionID: taskChild, CreatedAt: time.Now().Add(-time.Minute)}}, nil
}

func (f *taskHTTPFixture) ReadComputerOperation(context.Context, dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	f.reads++
	return f.op, nil
}

func (f *taskHTTPFixture) GetMount(_ context.Context, bot, id string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	if !f.grant || bot != taskBot || id != taskTarget {
		return dbstore.BotRemoteRuntimeBindingRecord{}, errors.New("SECRET")
	}
	raw, _ := json.Marshal(f.registration)
	return dbstore.BotRemoteRuntimeBindingRecord{ID: taskTarget, BotID: taskBot, RuntimeUserID: taskOwner, BotOwnerUserID: taskOwner, RuntimeProvider: "agenvo", RuntimeProviderConfig: raw}, nil
}

func (f *taskHTTPFixture) OwnedHTTPMCPConnectionBot(context.Context, string, string) (string, error) {
	if !f.source {
		return "", errors.New("SECRET")
	}
	return taskBot, nil
}

func (f *taskHTTPFixture) HasBotPermission(_ context.Context, bot, user, permission string) (bool, error) {
	return f.exec && bot == taskBot && user == taskUser && permission == "workspace_exec", nil
}

func TestSessionTaskHTTPAuthorizationRevocationAndNoDispatch(t *testing.T) {
	f := &taskHTTPFixture{chat: true, exec: true, source: true, grant: true, registration: agenvo.Target{ConnectionID: taskTarget, DeviceID: "synthetic", InstanceID: "synthetic", Session: "synthetic", WorkDir: `C:\synthetic`, BashPath: `C:\Git\bin\bash.exe`}}
	f.op = dbstore.ComputerOperationRecord{ID: taskOp, BotID: taskBot, UserID: taskUser, SessionID: taskChild, TargetID: taskTarget, TargetFingerprint: agenvo.TargetFingerprint(f.registration), State: "unknown", CancelRequested: true, Details: []byte(`{"kind":"herdr_pi_task","output":"SECRET","artifact_hash":"SECRET"}`)}
	relay := agenvo.NewService(f, f, func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
		f.relayCalls++
		return nil, errors.New("unexpected dispatch")
	})
	h := NewSessionTaskHandler(sessiontask.NewService(f, f, taskHTTPWorkdirs{}, relay, "test-secret"), bots.NewService(nil, f), newTestAdminAccountService("user"))
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.Default())
	e.Use(auth.JWTMiddleware("test-secret", func(echo.Context) bool { return false }))
	h.Register(e)
	base := "/bots/" + taskBot + "/sessions/" + taskRoot + "/tasks"
	detail := base + "/computer_operation/" + taskOp
	call := func(method, path, user string, want int) string {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if user != "" {
			token, _, err := auth.GenerateToken(user, "test-secret", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "SECRET") {
			t.Fatal("diagnostic/content leak")
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: status %d missing no-store", method, path, rec.Code)
		}
		return rec.Body.String()
	}
	call("GET", base, "", 401)
	call("GET", detail, "", 401)
	if f.reads != 0 {
		t.Fatal("unauthenticated read")
	}
	for _, path := range []string{base, detail} {
		call("GET", path, taskUser, 200)
		call("GET", path, taskOwner, 404)
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=bad", "?cursor=bad"} {
		call("GET", base+query, taskUser, 400)
	}
	call("GET", strings.Replace(base, taskRoot, taskOp, 1), taskUser, 404)
	call("GET", strings.Replace(base, taskBot, taskOwner, 1), taskUser, 404)
	call("GET", detail+"/attachments", taskUser, 404)
	call("POST", base, taskUser, 405)
	for _, scope := range []bool{false, true} {
		f.pcScope = scope
		for _, revoke := range []*bool{&f.exec, &f.source, &f.grant} {
			*revoke = false
			call("GET", detail, taskUser, 404)
			if scope {
				call("GET", base, taskUser, 404)
			} else if body := call("GET", base, taskUser, 200); strings.Contains(body, taskOp) {
				t.Fatal("revoked task listed")
			}
			*revoke = true
		}
	}
	f.chat = false
	for _, path := range []string{base, detail} {
		call("GET", path, taskUser, 403)
	}
	if f.relayCalls != 0 || f.op.Revision != 0 {
		t.Fatal("GET caused an external call or mutation")
	}
}

func TestSessionTaskNoStoreBeforeJWTAndOnlyWithinTaskNamespace(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.Default())
	// Match the production order: JWT is installed globally before handlers
	// register. Neither missing/invalid JWTs nor revoked accounts reach them.
	e.Use(auth.JWTMiddleware("test-secret", func(echo.Context) bool { return false }, func(_ context.Context, user string) error {
		if user == taskUser {
			return errors.New("SECRET revoked account")
		}
		return nil
	}))
	NewSessionTaskHandler(nil, nil, nil).Register(e)
	sign := func(secret string, expiry time.Time) string {
		t.Helper()
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": taskUser, "exp": expiry.Unix()}).SignedString([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	base := "/bots/" + taskBot + "/sessions/" + taskRoot
	for name, token := range map[string]string{
		"anonymous": "", "malformed": "invalid",
		"bad_signature": sign("wrong-secret", time.Now().Add(time.Minute)),
		"expired":       sign("test-secret", time.Now().Add(-time.Minute)),
		"revoked":       sign("test-secret", time.Now().Add(time.Minute)),
	} {
		t.Run(name, func(t *testing.T) {
			for _, route := range []struct {
				path    string
				noStore bool
			}{
				{base + "/tasks", true},
				{base + "/tasks/", true},
				{base + "/tasks/computer_operation/" + taskOp, true},
				{base + "/tasks/computer_operation/" + taskOp + "/attachments", true},
				{base + "/tasks-extra", false},
				{base + "/messages", false},
				{"/other" + base + "/tasks", false},
			} {
				req := httptest.NewRequest(http.MethodGet, route.path, nil)
				if token != "" {
					req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
				}
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				if rec.Code != 401 || rec.Header().Get(echo.HeaderContentType) != "application/problem+json" {
					t.Fatalf("%s: status=%d type=%s", route.path, rec.Code, rec.Header().Get(echo.HeaderContentType))
				}
				if (rec.Header().Get(echo.HeaderCacheControl) == "no-store") != route.noStore {
					t.Fatalf("%s: wrong cache policy %q", route.path, rec.Header().Get(echo.HeaderCacheControl))
				}
				if strings.Contains(rec.Body.String(), "SECRET") {
					t.Fatal("account diagnostic leaked")
				}
			}
		})
	}
	// Existing policies on adjacent routes remain intact for authenticated users.
	e.GET(base+"/messages", func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderCacheControl, "private, max-age=60")
		return c.NoContent(200)
	})
	token, _, err := auth.GenerateToken(taskOwner, "test-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, base+"/messages", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get(echo.HeaderCacheControl) != "private, max-age=60" {
		t.Fatal("task policy changed adjacent route response")
	}
}
