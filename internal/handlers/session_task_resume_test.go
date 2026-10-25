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

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/toolcontext"
)

type resumeHTTPFixture struct {
	observationReaderFunc
	resume func(context.Context, toolcontext.ComputerBinding, string, computertask.Submit) (computertask.Result, error)
}

func (f resumeHTTPFixture) ResumeNative(ctx context.Context, b toolcontext.ComputerBinding, id string, in computertask.Submit) (computertask.Result, error) {
	return f.resume(ctx, b, id, in)
}

func TestSessionTaskResumeHTTPAuthorityAndNoModelRun(t *testing.T) {
	f := &taskHTTPFixture{chat: true, exec: true, source: true, grant: true, registration: agenvo.Target{ConnectionID: taskTarget, DeviceID: "synthetic", InstanceID: "synthetic", Session: "synthetic", WorkDir: `C:\synthetic`, BashPath: `C:\Git\bin\bash.exe`}}
	f.op = dbstore.ComputerOperationRecord{ID: taskOp, BotID: taskBot, UserID: taskUser, SessionID: taskChild, TargetID: taskTarget, RunID: "original-terminal-run", TargetFingerprint: agenvo.TargetFingerprint(f.registration), State: "unknown", Details: []byte(`{"kind":"herdr_pi_task"}`)}
	relay := agenvo.NewService(f, f, func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
		t.Fatal("HTTP ownership lookup contacted native")
		return nil, nil
	})
	calls := 0
	var failure error
	late := false
	native := resumeHTTPFixture{resume: func(ctx context.Context, b toolcontext.ComputerBinding, id string, in computertask.Submit) (computertask.Result, error) {
		trusted, ok := toolcontext.HostComputerBinding(ctx)
		if !ok || trusted != b || b.BotID != taskBot || b.UserID != taskUser || b.SessionID != taskChild || b.TargetID != taskTarget || b.RunID != "" || id != taskOp || in.ResumeOperationID != "" || in.Task != "original synthetic instruction" {
			t.Fatal("HTTP caller invented a run or escaped original task authority")
		}
		calls++
		if late {
			f.grant = false
		}
		state, phase := "submitted", "batch_submitted"
		if failure != nil {
			state, phase = "unknown", "batch_creating_workspace"
		}
		return computertask.Result{ID: id, State: state, Phase: phase, Output: "SECRET native output"}, failure
	}}
	h := NewSessionTaskHandler(sessiontask.NewService(f, f, taskHTTPWorkdirs{}, relay, "test-secret"), bots.NewService(nil, f), newTestAdminAccountService("user"), native)
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.Default())
	e.Use(auth.JWTMiddleware("test-secret", func(echo.Context) bool { return false }))
	h.Register(e)
	path := "/bots/" + taskBot + "/sessions/" + taskRoot + "/tasks/computer_operation/" + taskOp + "/resume"
	call := func(path, user, body string, want int) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set("X-Memoh-Run-Id", "forged-run")
		req.Header.Set("X-Memoh-Session-Id", "forged-session")
		if user != "" {
			token, _, err := auth.GenerateToken(user, "test-secret", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != want || rec.Header().Get(echo.HeaderCacheControl) != "no-store" || strings.Contains(rec.Body.String(), "SECRET") {
			t.Fatalf("status=%d want=%d cache=%s body=%s", rec.Code, want, rec.Header().Get(echo.HeaderCacheControl), rec.Body.String())
		}
		return rec.Body.String()
	}
	in := `{"kind":"herdr_pi_task","task":"original synthetic instruction","work_dir":"."}`
	call(path, "", in, 401)
	call(path, taskOwner, in, 404)
	call(strings.Replace(path, taskRoot, taskOwner, 1), taskUser, in, 404)
	call(strings.Replace(path, taskBot, taskOwner, 1), taskUser, in, 404)
	for _, extra := range []string{"run_id", "user_id", "session_id", "resume_operation_id", "operation_id", "workspace_id", "pane_id", "request_id"} {
		call(path, taskUser, strings.TrimSuffix(in, "}")+`,"`+extra+`":"forged"}`, 400)
	}
	for _, bad := range []string{"null", `{}`, in + `{}`, strings.Replace(in, "herdr_pi_task", "foreground_bash", 1), `{"kind":"herdr_pi_task","task":"` + strings.Repeat("x", 128*1024) + `"}`} {
		call(path, taskUser, bad, 400)
	}
	if calls != 0 {
		t.Fatal("refused request reached resume")
	}
	for _, tc := range []struct {
		err     error
		outcome string
	}{
		{nil, "submitted"}, {agenvo.ErrUnknown, "unknown"}, {agenvo.ErrUnavailable, "unavailable"}, {agenvo.ErrNativeUnavailable, "unavailable"}, {db.ErrComputerAdmissionPaused, "blocked"}, {agenvo.ErrUnsupported, "blocked"}, {errors.New("SECRET upstream failure"), "unknown"},
	} {
		failure = tc.err
		body := call(path, taskUser, in, 200)
		var out NativeTaskResumeResponse
		if json.Unmarshal([]byte(body), &out) != nil || out.OperationID != taskOp || out.Outcome != tc.outcome || out.AutomaticRetry || out.BusinessOutcome != "unknown" {
			t.Fatal(body)
		}
	}
	failure = computertask.ErrConflict
	call(path, taskUser, in, 409)
	failure = nil
	late = true
	call(path, taskUser, in, 404)
	before := calls
	call(path, taskUser, in, 404)
	f.grant = true
	f.chat = false
	call(path, taskUser, in, 403)
	f.chat = true
	f.exec = false
	call(path, taskUser, in, 404)
	if calls != before {
		t.Fatal("revocation reached continuation")
	}
}
