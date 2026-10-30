package handlers

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/taskresult"
)

type recoveryHTTPHost struct {
	*delegationHTTPFixture
	recoveries int
	conflict   bool
}

func (h *recoveryHTTPHost) RecoverResult(_ context.Context, r botdelegation.Record, in botdelegation.RecoveryInput) (botdelegation.RecoveryView, error) {
	h.recoveries++
	if h.conflict {
		return botdelegation.RecoveryView{}, botdelegation.ErrConflict
	}
	return botdelegation.RecoveryView{EventID: in.EventID, Event: taskresult.Event{ReportState: "pending", Attempt: in.ExpectedAttempt + 1}}, nil
}

func TestSavedResultRecoveryHTTPBoundary(t *testing.T) {
	f := &delegationHTTPFixture{allowed: true}
	f.row = botdelegation.Record{ID: uuid.NewString(), Identity: botdelegation.Identity{TeamID: "team", UserID: taskUser, SourceBotID: taskBot, SourceSessionID: taskRoot, SourceRootID: taskRoot, TargetBotID: taskTarget, TargetSessionID: taskOp, SourceSelection: "native"}, State: "completed", Payload: botdelegation.Payload{ResultDeliveryVersion: 1}}
	a := &botdelegation.Authority{Permissions: f, Sessions: delegationHTTPSessions{f}}
	s := botdelegation.NewService(a, f, nil, slog.New(slog.DiscardHandler), time.Minute)
	t.Cleanup(s.Close)
	host := &recoveryHTTPHost{delegationHTTPFixture: f}
	s.Host = host
	h := NewBotDelegationHandler(s, bots.NewService(nil, &taskHTTPFixture{chat: true}), newTestAdminAccountService("user"))
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	e.Use(auth.JWTMiddleware("synthetic", func(echo.Context) bool { return false }))
	h.Register(e)
	path := "/bots/" + taskBot + "/sessions/" + taskRoot + "/delegations/" + f.row.ID + "/delivery/recover"
	body := `{"event_id":"` + strings.Repeat("a", 64) + `","expected_attempt":3}`
	call := func(user, raw string, want int) {
		t.Helper()
		req := httptest.NewRequest("POST", path, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if user != "" {
			token, _, err := auth.GenerateToken(user, "synthetic", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != want || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("got %d want %d body %s", rec.Code, want, rec.Body.String())
		}
	}
	call("", body, 401)
	call(taskUser, `{}`, 400)
	call(taskUser, `{"event_id":"bad","expected_attempt":3}`, 400)
	call(taskUser, `{"event_id":"`+strings.Repeat("a", 64)+`","expected_attempt":2}`, 400)
	call(taskUser, `{"event_id":"`+strings.Repeat("a", 64)+`","expected_attempt":2147483647}`, 400)
	call(taskUser, body, 202)
	host.conflict = true
	call(taskUser, body, 409)
	f.allowed = false
	call(taskUser, body, 404)
	f.allowed = true
	f.row.State = "unknown"
	call(taskUser, body, 409)
	f.row.State = "completed"
	f.row.Payload.ResultDeliveryVersion = 0
	call(taskUser, body, 409)
	if host.recoveries != 2 || f.creates.Load() != 0 || f.runs.Load() != 0 || f.deliveries.Load() != 0 {
		t.Fatalf("wrong boundary: recoveries=%d creates=%d runs=%d delivery=%d", host.recoveries, f.creates.Load(), f.runs.Load(), f.deliveries.Load())
	}
}
