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

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/server"
)

type recordingUserInputRecovery struct {
	sessionAgentRuntimeService
	calls int
	input application.UserInputRecoveryInput
	err   error
}

func (s *recordingUserInputRecovery) RecoverAcceptedUserInput(ctx context.Context, input application.UserInputRecoveryInput) (application.UserInputRecoveryResult, error) {
	s.calls++
	s.input = input
	if err := input.Reauthorize(ctx); err != nil {
		return application.UserInputRecoveryResult{}, err
	}
	return application.UserInputRecoveryResult{OriginalRunID: input.OriginalRunID, DecisionID: input.DecisionID, RecoveryRunID: "cccccccc-1111-4111-8111-111111111111", InvocationID: "synthetic-recovery", State: "running", Started: true}, s.err
}

func TestAcceptedUserInputRecoveryHTTPUsesAccountAndOwnedSession(t *testing.T) {
	for _, scenario := range []string{"owner", "anonymous", "revoked_account", "chat_route", "other_creator", "cross_bot", "revoked_permission", "injected_answer", "extra_json", "unknown_checkpoint", "private_error"} {
		t.Run(scenario, func(t *testing.T) {
			const secret = "synthetic-recovery-http-signing-key"
			const originalRun = "aaaaaaaa-1111-4111-8111-111111111111"
			const decision = "bbbbbbbb-1111-4111-8111-111111111111"
			q := newInvocationTestQueries(invocationTestSession(invocationTestSessionA, invocationTestOwnerID))
			h := newInvocationTestHandler(q, nil)
			service := &recordingUserInputRecovery{}
			h.SetAgentRuntimeService(service)
			e := echo.New()
			e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
			e.Use(auth.JWTMiddleware(secret, nil, func(context.Context, string) error {
				if scenario == "revoked_account" {
					return errors.New("synthetic account revoked")
				}
				return nil
			}))
			h.Register(e)
			token, _, err := auth.GenerateToken(invocationTestOwnerID, secret, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			body := `{"original_run_id":"` + originalRun + `"}`
			botID := invocationTestBotID
			wantStatus := http.StatusAccepted
			switch scenario {
			case "anonymous":
				token, wantStatus = "", http.StatusUnauthorized
			case "revoked_account":
				wantStatus = http.StatusUnauthorized
			case "chat_route":
				token, _, err = auth.GenerateChatToken(auth.ChatToken{BotID: botID, ChatID: botID, UserID: invocationTestOwnerID}, secret, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				wantStatus = http.StatusForbidden
			case "other_creator":
				q.sessions[invocationTestSessionA] = invocationTestSession(invocationTestSessionA, invocationTestOtherUser)
				wantStatus = http.StatusNotFound
			case "cross_bot":
				botID, wantStatus = "cccccccc-2222-4222-8222-222222222222", http.StatusNotFound
			case "revoked_permission":
				q.permissions, wantStatus = []byte(`[]`), http.StatusForbidden
			case "injected_answer":
				body = `{"original_run_id":"` + originalRun + `","answers":[{"text":"must not replace the accepted answer"}]}`
				wantStatus = http.StatusBadRequest
			case "extra_json":
				body += `{}`
				wantStatus = http.StatusBadRequest
			case "unknown_checkpoint":
				service.err, wantStatus = application.ErrUserInputRecoveryUnavailable, http.StatusConflict
			case "private_error":
				service.err, wantStatus = errors.New("SYNTHETIC_PRIVATE_RECOVERY_CAUSE"), http.StatusInternalServerError
			}
			request := httptest.NewRequest(http.MethodPost, "/bots/"+botID+"/sessions/"+invocationTestSessionA+"/decisions/"+decision+"/recover", strings.NewReader(body))
			request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			if token != "" {
				request.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
			}
			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, request)
			if recorder.Code != wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, wantStatus, recorder.Body.String())
			}
			allowed := scenario == "owner" || scenario == "unknown_checkpoint" || scenario == "private_error"
			if (service.calls == 1) != allowed {
				t.Fatalf("recovery calls=%d allowed=%v", service.calls, allowed)
			}
			if strings.Contains(recorder.Body.String(), "SYNTHETIC_PRIVATE_RECOVERY_CAUSE") || strings.Contains(recorder.Body.String(), secret) || token != "" && strings.Contains(recorder.Body.String(), token) {
				t.Fatal("response leaked private diagnostics or credential material")
			}
			if scenario == "owner" {
				var result application.UserInputRecoveryResult
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.OriginalRunID != originalRun || result.DecisionID != decision || !result.Started || service.input.ActorID != invocationTestOwnerID || recorder.Header().Get(echo.HeaderCacheControl) != "no-store" {
					t.Fatalf("recovery contract mismatch: %+v %v", result, err)
				}
			}
		})
	}
}
