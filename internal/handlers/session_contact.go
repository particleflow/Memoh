package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	session, err := resolveContinuousContact(c.Request().Context(), h.privateChats, bot, user)
	if errors.Is(err, route.ErrContactUnavailable) {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	if err != nil {
		return err
	}
	// A contact keeps its identity after a legal descriptor change. Reuse the
	// normal read authorization, including workspace_exec for external runtimes.
	_, _, session, err = h.authorizeSession(c, user, bot, session.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, session)
}

type privateChatNavigator interface {
	InspectPrivateChat(context.Context, string, string, string, ...string) (route.PrivateChatEntry, error)
	BindPrivateChat(context.Context, string, string, string) (thread.Thread, error)
}

type BindPrivateChatRequest struct {
	SessionID string `json:"session_id"`
}

func (h *SessionHandler) contactAccount(c echo.Context) (string, string, error) {
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	if _, err := auth.ChatTokenFromContext(c); err == nil {
		return "", "", apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	user, err := RequireChannelIdentityID(c)
	if err != nil {
		return "", "", err
	}
	bot := c.Param("bot_id")
	if id, e := uuid.Parse(bot); e != nil || id == uuid.Nil {
		return "", "", apperror.FieldInvalid("bot_id", e)
	}
	_, err = AuthorizeBotAccessWithPermission(c.Request().Context(), h.botService, h.accountService, user, bot, bots.PermissionChat)
	return user, bot, err
}

func contactNavigationError(err error) error {
	if errors.Is(err, route.ErrContactConflict) {
		return apperror.New(apperror.CodeHTTPConflict, nil)
	}
	if errors.Is(err, route.ErrContactUnavailable) {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	return err
}

// InspectPrivateChat godoc
// @Summary Inspect this account's Bot contact without changing history
// @Description Returns the bound conversation or eligible user-owned local model chats. Candidates require explicit confirmation; ID ordering is not a preference. A known deleted contact is resolved by POST. No task or hook is dispatched.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param after query string false "Candidate ID cursor"
// @Param selected_session_id query string false "Explicitly opened conversation; eligibility is verified before offering"
// @Success 200 {object} route.PrivateChatEntry
// @Failure 400,401,403,404,409,500,503 {object} server.Problem
// @Router /bots/{bot_id}/sessions/contact [get].
func (h *SessionHandler) InspectPrivateChat(c echo.Context) error {
	user, bot, err := h.contactAccount(c)
	if err != nil {
		return err
	}
	after := c.QueryParam("after")
	if after != "" {
		if _, e := uuid.Parse(after); e != nil {
			return apperror.FieldInvalid("after", e)
		}
	}
	navigator, ok := h.privateChats.(privateChatNavigator)
	if !ok {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	selected := c.QueryParam("selected_session_id")
	if selected != "" {
		if _, e := uuid.Parse(selected); e != nil {
			return apperror.FieldInvalid("selected_session_id", e)
		}
	}
	entry, err := navigator.InspectPrivateChat(c.Request().Context(), bot, user, after, selected)
	if err != nil {
		return contactNavigationError(err)
	}
	if entry.Session != nil {
		_, _, s, e := h.authorizeSession(c, user, bot, entry.Session.ID)
		if e != nil {
			return e
		}
		entry.Session = &s
	}
	entry.Candidates = []thread.Thread{}
	entry.NextCursor = ""
	return c.JSON(http.StatusOK, entry)
}

// BindPrivateChat godoc
// @Summary Confirm an existing conversation as this account's Bot contact
// @Description First binding only. The server verifies account ownership, ordinary local model chat, user visibility, no parent/channel route/delegation. Does not merge, copy, retarget tasks, replay hooks or replace another device's binding. On conflict or uncertain outcome, inspect the contact before deciding again.
// @Tags sessions
// @Accept json
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param payload body BindPrivateChatRequest true "Explicitly selected conversation"
// @Success 200 {object} thread.Thread
// @Failure 400,401,403,404,409,500,503 {object} server.Problem
// @Router /bots/{bot_id}/sessions/contact/bind [post].
func (h *SessionHandler) BindPrivateChat(c echo.Context) error {
	user, bot, err := h.contactAccount(c)
	if err != nil {
		return err
	}
	var input BindPrivateChatRequest
	decoder := json.NewDecoder(io.LimitReader(c.Request().Body, 4096))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		return apperror.New(apperror.CodeHTTPBadRequest, nil)
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return apperror.New(apperror.CodeHTTPBadRequest, nil)
	}
	if id, e := uuid.Parse(input.SessionID); e != nil || id == uuid.Nil {
		return apperror.FieldInvalid("session_id", e)
	}
	navigator, ok := h.privateChats.(privateChatNavigator)
	if !ok {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	session, err := navigator.BindPrivateChat(c.Request().Context(), bot, user, input.SessionID)
	if err != nil {
		return contactNavigationError(err)
	}
	_, _, session, err = h.authorizeSession(c, user, bot, session.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, session)
}
