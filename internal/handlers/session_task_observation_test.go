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

type observationReaderFunc func(context.Context, toolcontext.ComputerBinding, string) (computertask.Recovery, error)

func (f observationReaderFunc) Recover(ctx context.Context, b toolcontext.ComputerBinding, id string) (computertask.Recovery, error) {
	return f(ctx, b, id)
}

func TestSessionTaskObservationHTTPHostBindingAndLateRevocation(t *testing.T) {
	f := &taskHTTPFixture{chat: true, exec: true, source: true, grant: true, registration: taskHTTPNativeRegistration()}
	f.op = dbstore.ComputerOperationRecord{ID: taskOp, BotID: taskBot, UserID: taskUser, SessionID: taskChild, TargetID: taskTarget, TargetFingerprint: agenvo.TargetFingerprint(f.registration), State: "unknown", Details: []byte(`{"kind":"herdr_pi_task"}`)}
	relay := agenvo.NewService(f, f, func(context.Context, agenvo.Scope, agenvo.Target, string, map[string]any) (agenvo.Receipt, error) {
		t.Fatal("HTTP ownership lookup contacted relay")
		return agenvo.Receipt{}, nil
	})
	calls := 0
	revoke := false
	revokeChat := false
	changeInstance := false
	availability := "online"
	observer := observationReaderFunc(func(ctx context.Context, b toolcontext.ComputerBinding, id string) (computertask.Recovery, error) {
		calls++
		if b.BotID != taskBot || b.UserID != taskUser || b.SessionID != taskChild || b.TargetID != taskTarget || id != taskOp {
			t.Fatalf("wrong host binding: %+v", b)
		}
		if err := toolcontext.CheckComputerTarget(ctx, "foreign-pc"); err == nil {
			t.Fatal("scope not narrowed")
		}
		if revoke {
			f.grant = false
		}
		if revokeChat {
			f.chat = false
		}
		if changeInstance {
			f.registration.InstanceID = "replacement-instance"
		}
		return computertask.Recovery{OperationID: id, TargetID: b.TargetID, State: "unknown", BusinessOutcome: "unknown", WorkspaceID: "w-synthetic", PaneID: "p-synthetic", Native: agenvo.ReferenceObservation{Availability: availability, AgentStatus: "done", Match: "observed"}, Reconciliation: &computertask.OperationReconciliation{Revision: "9007199254740993", TargetFingerprint: f.op.TargetFingerprint, ReceiptState: "missing", Files: []computertask.RecoveryFile{}}}, nil
	})
	h := NewSessionTaskHandler(sessiontask.NewService(f, f, taskHTTPWorkdirs{}, relay, "test-secret"), bots.NewService(nil, f), newTestAdminAccountService("user"), observer)
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.Default())
	e.Use(auth.JWTMiddleware("test-secret", func(echo.Context) bool { return false }))
	h.Register(e)
	path := "/bots/" + taskBot + "/sessions/" + taskRoot + "/tasks/computer_operation/" + taskOp + "/observation"
	call := func(user string, want int) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path+"?target_id=foreign-pc&pane_id=forged", nil)
		req.Header.Set("X-Session-ID", "foreign-session")
		if user != "" {
			token, _, err := auth.GenerateToken(user, "test-secret", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != want || rec.Header().Get(echo.HeaderCacheControl) != "no-store" {
			t.Fatalf("status=%d want=%d cache=%s body=%s", rec.Code, want, rec.Header().Get(echo.HeaderCacheControl), rec.Body.String())
		}
		return rec.Body.String()
	}
	call("", 401)
	if calls != 0 {
		t.Fatal("anonymous observed")
	}
	before, _ := json.Marshal(f.op)
	body := call(taskUser, 200)
	if !strings.Contains(body, `"agent_status":"done"`) || !strings.Contains(body, `"business_outcome":"unknown"`) {
		t.Fatal(body)
	}
	if !strings.Contains(body, `"revision":"9007199254740993"`) || !strings.Contains(body, `"receipt_state":"missing"`) || strings.Contains(body, `"stop_authorized"`) {
		t.Fatal("HTTP adapter lost exact revision or invented stop authorization")
	}
	after, _ := json.Marshal(f.op)
	if string(before) != string(after) {
		t.Fatal("observation changed receipt")
	}
	availability = "authentication_unavailable"
	body = call(taskUser, 200)
	if !strings.Contains(body, `"availability":"authentication_unavailable"`) || !strings.Contains(body, `"state":"unknown"`) || !strings.Contains(body, `"business_outcome":"unknown"`) {
		t.Fatal("HTTP 200 misrepresented native authentication or task outcome")
	}
	after, _ = json.Marshal(f.op)
	if string(before) != string(after) {
		t.Fatal("unavailable observation changed receipt")
	}
	availability = "online"
	changeInstance = true
	body = call(taskUser, 404)
	if strings.Contains(body, "w-synthetic") {
		t.Fatal("late instance replacement leaked observation")
	}
	identityCalls := calls
	changeInstance = false
	call(taskUser, 404)
	if calls != identityCalls {
		t.Fatal("replacement instance reached observer")
	}
	f.registration.InstanceID = "synthetic"
	revokeChat = true
	body = call(taskUser, 403)
	if strings.Contains(body, "w-synthetic") {
		t.Fatal("late Bot revocation leaked observation")
	}
	revokeChat = false
	f.chat = true
	revoke = true
	body = call(taskUser, 404)
	if strings.Contains(body, "w-synthetic") {
		t.Fatal("late revocation leaked observation")
	}
	callsBefore := calls
	call(taskUser, 404)
	if calls != callsBefore {
		t.Fatal("revoked grant reached observer")
	}
	f.grant = true
	f.chat = false
	call(taskUser, 403)
	if calls != callsBefore {
		t.Fatal("revoked Bot permission reached observer")
	}
	f.chat = true
	f.op.SessionID = taskRoot
	call(taskUser, 404)
	if calls != callsBefore {
		t.Fatal("owner mismatch reached observer")
	}
}
