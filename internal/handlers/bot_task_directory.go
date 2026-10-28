package handlers

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/sessiontask"
)

func (h *SessionTaskHandler) directoryAuthority(c echo.Context) (string, func(thread.Thread) (bool, error), bool, error) {
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	if _, err := auth.ChatTokenFromContext(c); err == nil {
		return "", nil, false, apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	user, err := RequireChannelIdentityID(c)
	if err != nil {
		return "", nil, false, err
	}
	bot := c.Param("bot_id")
	if id, err := uuid.Parse(bot); err != nil || id == uuid.Nil {
		return "", nil, false, apperror.FieldInvalid("bot_id", err)
	}
	if _, err = AuthorizeBotAccessWithPermission(c.Request().Context(), h.bots, h.accounts, user, bot, bots.PermissionChat); err != nil {
		if _, err = AuthorizeBotAccessWithPermission(c.Request().Context(), h.bots, h.accounts, user, bot, bots.PermissionWorkspaceExec); err != nil {
			return "", nil, false, err
		}
	}
	admin, err := h.accounts.IsAdmin(c.Request().Context(), user)
	if err != nil {
		return "", nil, false, err
	}
	perms, err := h.bots.ResolveUserPermissions(c.Request().Context(), bot, user, admin)
	if err != nil {
		return "", nil, false, err
	}
	// Incoming/outgoing handoffs can name another Bot. Its current session read
	// permission must be checked as well as the delegation's dual-Bot authority.
	readable := func(session thread.Thread) (bool, error) {
		current, err := h.bots.ResolveUserPermissions(c.Request().Context(), session.BotID, user, admin)
		if err != nil {
			return false, err
		}
		return canAccessSession(session, user, current), nil
	}
	return user, readable, bots.HasPermission(perms, bots.PermissionManage), nil
}

// Directory godoc
// @Summary List original persisted tasks across this Bot's history
// @Description Account-only read of native Agent admissions, computer receipts, incoming/outgoing Bot handoffs and schedule fires. Includes terminal and unknown records and multiple tasks per session. Sessions, configured Agents and ordinary chat runs are not tasks. Each source rechecks its existing permissions. Native task_id and provider run_id are opaque strings; record_id identifies the durable occurrence. Continue nonempty next_cursor even on an empty page. Queued in-memory background work without a persisted admission is not included. No execution, device polling, result text or attachments.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param limit query int false "Page size 1-100" default(20)
// @Param cursor query string false "Opaque Bot and account scoped continuation"
// @Success 200 {object} sessiontask.DirectoryPage
// @Failure 400,401,403,404,500 {object} server.Problem
// @Router /bots/{bot_id}/tasks [get].
func (h *SessionTaskHandler) Directory(c echo.Context) error {
	user, readable, schedules, err := h.directoryAuthority(c)
	if err != nil {
		return err
	}
	limit := 20
	if raw := c.QueryParam("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return apperror.FieldInvalid("limit", nil)
		}
	}
	var page sessiontask.DirectoryPage
	page, err = h.service.ListDirectory(c.Request().Context(), c.Param("bot_id"), user, limit, c.QueryParam("cursor"), readable, schedules)
	if err != nil {
		return taskHTTPError(err)
	}
	return c.JSON(http.StatusOK, page)
}

// DirectoryTask godoc
// @Summary Recheck one original task occurrence before navigation
// @Description Same source-specific live authorization as the directory. A missing destination does not remove a real failed or unknown task. This read never submits or resumes work.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Directory Bot ID"
// @Param task_type path string true "Original task type" Enums(native_agent,computer_operation,bot_delegation,schedule_fire)
// @Param record_id path string true "Durable occurrence ID"
// @Success 200 {object} sessiontask.DirectoryTask
// @Failure 400,401,403,404,500 {object} server.Problem
// @Router /bots/{bot_id}/tasks/{task_type}/{record_id} [get].
func (h *SessionTaskHandler) DirectoryTask(c echo.Context) error {
	user, readable, schedules, err := h.directoryAuthority(c)
	if err != nil {
		return err
	}
	item, err := h.service.GetDirectoryTask(c.Request().Context(), c.Param("bot_id"), user, c.Param("task_type"), c.Param("record_id"), readable, schedules)
	if err != nil {
		return taskHTTPError(err)
	}
	return c.JSON(http.StatusOK, item)
}
