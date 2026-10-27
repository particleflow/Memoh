package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agent/application"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	sessionpkg "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
)

type userInputRecoveryService interface {
	RecoverAcceptedUserInput(context.Context, application.UserInputRecoveryInput) (application.UserInputRecoveryResult, error)
}

var _ userInputRecoveryService = (*application.Service)(nil)

type AcceptedUserInputRecoveryInput struct {
	OriginalRunID string `json:"original_run_id" validate:"required" format:"uuid"`
}

func (h *SessionHandler) authorizeUserInputRecovery(ctx context.Context, actor, botID, sessionID string) error {
	if _, err := AuthorizeBotAccessWithPermission(ctx, h.botService, h.accountService, actor, botID, bots.PermissionChat); err != nil {
		return err
	}
	sess, err := h.sessionService.Get(ctx, sessionID)
	if err != nil || sess.BotID != botID || sess.CreatedByUserID != actor {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	mode, runtime := normalizedSessionDescriptor(sess)
	if mode != sessionpkg.TypeChat || runtime != sessionpkg.RuntimeModel {
		return apperror.New(apperror.CodeHTTPConflict, nil)
	}
	return nil
}

// RecoverAcceptedUserInput godoc
// @Summary Explicitly continue a submitted ask_user answer after history failure
// @Description Account/session-authenticated owner action for a failed native retry run whose accepted answer has no persisted tool result. Uses only the original saved input and accepted decision. Reconciles its exact hidden history and admits one idempotent continuation run; keeps the original run failed. Never resubmits the answer or retries the whole original turn. Repeated requests return the same recovery run, including failed/unknown outcomes. No automatic invocation on reconnect. A successful response acknowledges admission, not model or business completion.
// @Tags sessions
// @Accept json
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param session_id path string true "Owned chat session ID"
// @Param decision_id path string true "Original submitted ask_user decision ID"
// @Param body body AcceptedUserInputRecoveryInput true "Original failed run identity"
// @Success 202 {object} application.UserInputRecoveryResult
// @Failure 400,401,403,404,409,500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/decisions/{decision_id}/recover [post].
func (h *SessionHandler) RecoverAcceptedUserInput(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	if _, err := auth.ChatTokenFromContext(c); err == nil {
		return apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	actor, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	botID, sessionID, decisionID := c.Param("bot_id"), c.Param("session_id"), c.Param("decision_id")
	if _, err := db.ParseUUID(botID); err != nil {
		return apperror.FieldInvalid("bot_id", err)
	}
	if _, err := db.ParseUUID(sessionID); err != nil {
		return apperror.FieldInvalid("session_id", err)
	}
	if _, err := db.ParseUUID(decisionID); err != nil {
		return apperror.FieldInvalid("decision_id", err)
	}
	reauthorize := func(ctx context.Context) error { return h.authorizeUserInputRecovery(ctx, actor, botID, sessionID) }
	if err := reauthorize(c.Request().Context()); err != nil {
		return err
	}
	var input AcceptedUserInputRecoveryInput
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 128*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return apperror.FieldInvalid("body", nil)
	}
	if _, err := db.ParseUUID(input.OriginalRunID); err != nil {
		return apperror.FieldInvalid("original_run_id", err)
	}
	service, ok := h.agentRuntimes.(userInputRecoveryService)
	if !ok {
		return apperror.New(apperror.CodeInternal, nil)
	}
	result, err := service.RecoverAcceptedUserInput(c.Request().Context(), application.UserInputRecoveryInput{BotID: botID, SessionID: sessionID, OriginalRunID: input.OriginalRunID, DecisionID: decisionID, ActorID: actor, ChatToken: "Bearer " + auth.RawTokenFromContext(c), Reauthorize: reauthorize})
	switch {
	case errors.Is(err, application.ErrUserInputRecoveryUnavailable):
		return apperror.New(apperror.CodeHTTPConflict, nil)
	case errors.Is(err, sessionruntime.ErrSessionBusy):
		return apperror.New(apperror.CodeSessionBusy, nil)
	case errors.Is(err, pgx.ErrNoRows):
		return apperror.New(apperror.CodeSessionNotFound, nil)
	case err != nil:
		return err
	}
	return c.JSON(http.StatusAccepted, result)
}
