package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
)

type privateChatResolver interface {
	EnsurePrivateChat(context.Context, string, string) (thread.Thread, error)
}

func (h *SessionHandler) SetPrivateChatResolver(resolver privateChatResolver) {
	h.privateChats = resolver
}

// ResolvePrivateChat godoc
// @Summary Resolve this account's persistent private chat with a Bot
// @Description Reuses a server-owned Web route for this authenticated account and Bot across devices. Atomically creates its chat only when absent. No model call, task dispatch, adoption or mutation of historical sessions. Chat-route tokens and client-supplied identity/route/body are not accepted.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Success 200 {object} thread.Thread
// @Failure 400,401,403,404,500,503 {object} server.Problem
// @Router /bots/{bot_id}/sessions/contact [post].
func (h *SessionHandler) ResolvePrivateChat(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	if _, err := auth.ChatTokenFromContext(c); err == nil {
		return apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	user, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	bot := c.Param("bot_id")
	if _, err = uuid.Parse(bot); err != nil {
		return apperror.FieldInvalid("bot_id", err)
	}
	if c.Request().ContentLength != 0 {
		return apperror.New(apperror.CodeHTTPBadRequest, nil)
	}
	if _, err = AuthorizeBotAccessWithPermission(c.Request().Context(), h.botService, h.accountService, user, bot, bots.PermissionChat); err != nil {
		return err
	}
	if h.privateChats == nil {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	session, err := h.privateChats.EnsurePrivateChat(c.Request().Context(), bot, user)
	if errors.Is(err, route.ErrContactUnavailable) {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, session)
}
