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

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/computertask"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/toolcontext"
)

type nativeHTTPFixture struct {
	observationReaderFunc
	read   func(context.Context, toolcontext.ComputerBinding, string) (computertask.NativeContent, error)
	follow func(context.Context, toolcontext.ComputerBinding, computertask.Followup) (computertask.Result, error)
}

func (f nativeHTTPFixture) NativeContent(ctx context.Context, b toolcontext.ComputerBinding, id string) (computertask.NativeContent, error) {
	return f.read(ctx, b, id)
}

func (f nativeHTTPFixture) FollowupNative(ctx context.Context, b toolcontext.ComputerBinding, in computertask.Followup) (computertask.Result, error) {
	return f.follow(ctx, b, in)
}

func TestSessionTaskNativeHTTPAuthorityAndStrictInput(t *testing.T) {
	f := &taskHTTPFixture{chat: true, exec: true, source: true, grant: true, registration: agenvo.Target{ConnectionID: taskTarget, DeviceID: "synthetic", InstanceID: "synthetic", Session: "synthetic", WorkDir: `C:\synthetic`, BashPath: `C:\Git\bin\bash.exe`}}
	f.op = dbstore.ComputerOperationRecord{ID: taskOp, BotID: taskBot, UserID: taskUser, SessionID: taskChild, TargetID: taskTarget, TargetFingerprint: agenvo.TargetFingerprint(f.registration), State: "unknown", Details: []byte(`{"kind":"herdr_pi_task"}`)}
	relay := agenvo.NewService(f, f, func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
		t.Fatal("ownership lookup called native")
		return nil, nil
	})
	calls := 0
	late := false
	check := func(b toolcontext.ComputerBinding, id string) {
		t.Helper()
		if b.UserID != taskUser || b.SessionID != taskChild || b.TargetID != taskTarget || id != taskOp {
			t.Fatal("forged native scope")
		}
		calls++
		if late {
			f.chat = false
		}
	}
	native := nativeHTTPFixture{
		read: func(_ context.Context, b toolcontext.ComputerBinding, id string) (computertask.NativeContent, error) {
			check(b, id)
			return computertask.NativeContent{OperationID: id, Supported: true, Observation: &agenvo.AgentObservation{Output: "synthetic terminal", OutputFidelity: "terminal_snapshot_unverified"}}, nil
		},
		follow: func(_ context.Context, b toolcontext.ComputerBinding, in computertask.Followup) (computertask.Result, error) {
			check(b, in.OperationID)
			return computertask.Result{ID: in.OperationID, Followup: &computertask.FollowupResult{RequestID: in.RequestID, State: "unknown", BusinessOutcome: "unknown"}}, agenvo.ErrUnknown
		},
	}
	h := NewSessionTaskHandler(sessiontask.NewService(f, f, taskHTTPWorkdirs{}, relay, "test-secret"), bots.NewService(nil, f), newTestAdminAccountService("user"), native)
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.Default())
	e.Use(auth.JWTMiddleware("test-secret", func(echo.Context) bool { return false }))
	h.Register(e)
	path := "/bots/" + taskBot + "/sessions/" + taskRoot + "/tasks/computer_operation/" + taskOp
	token, _, err := auth.GenerateToken(taskUser, "test-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, suffix, body string, authenticated bool, want int) string {
		t.Helper()
		req := httptest.NewRequest(method, path+suffix, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set("X-Session-ID", "forged")
		if authenticated {
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != want || rec.Header().Get(echo.HeaderCacheControl) != "no-store" {
			t.Fatalf("status %d want %d cache=%s body=%s", rec.Code, want, rec.Header().Get(echo.HeaderCacheControl), rec.Body.String())
		}
		return rec.Body.String()
	}
	call(http.MethodGet, "/content", "", false, 401)
	call(http.MethodPost, "/followup", `{}`, false, 401)
	if calls != 0 {
		t.Fatal("anonymous native access")
	}
	body := call(http.MethodGet, "/content?pane_id=forged", "", true, 200)
	if !strings.Contains(body, "terminal_snapshot_unverified") {
		t.Fatal(body)
	}
	in := `{"request_id":"22222222-2222-4222-8222-222222222222","mode":"bound_session","task":"synthetic"}`
	body = call(http.MethodPost, "/followup", in, true, 200)
	if !strings.Contains(body, `"state":"unknown"`) || strings.Contains(body, `"state":"accepted"`) {
		t.Fatal(body)
	}
	before := calls
	call(http.MethodPost, "/followup", strings.TrimSuffix(in, "}")+`,"target_id":"forged"}`, true, 400)
	if calls != before {
		t.Fatal("unknown fields reached native")
	}
	late = true
	body = call(http.MethodGet, "/content", "", true, 403)
	if strings.Contains(body, "synthetic terminal") {
		t.Fatal("late revocation leaked output")
	}
	late = false
	f.chat = true
	f.grant = false
	before = calls
	call(http.MethodPost, "/followup", in, true, 404)
	if calls != before {
		t.Fatal("revoked target submitted")
	}
}
