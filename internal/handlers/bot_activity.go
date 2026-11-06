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

// Activity godoc
// @Summary List readable active Bot runs, Agent runs and computer tasks
// @Description Read-only existing persisted states, independent of the open conversation. Ordinary session/ancestor permissions and current computer target grants are rechecked. Unknown and terminal entries are not active. Names and original session references only; no transcripts, dispatch or device calls. Continue while next_cursor is nonempty, including an empty filtered page. Account authentication required.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param limit query int false "Page size (1-100)" default(100)
// @Param cursor query string false "Opaque continuation scoped to caller and Bot"
// @Success 200 {object} sessiontask.ActivityPage
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Router /bots/{bot_id}/activity [get].
func (h *SessionTaskHandler) Activity(c echo.Context) error {
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
	entry, err := AuthorizeBotAccessWithPermission(c.Request().Context(), h.bots, h.accounts, user, bot, bots.PermissionChat)
	if err != nil {
		if entry, err = AuthorizeBotAccessWithPermission(c.Request().Context(), h.bots, h.accounts, user, bot, bots.PermissionWorkspaceExec); err != nil {
			return err
		}
	}
	admin, err := h.accounts.IsAdmin(c.Request().Context(), user)
	if err != nil {
		return err
	}
	perms, err := h.bots.ResolveUserPermissions(c.Request().Context(), bot, user, admin)
	if err != nil {
		return err
	}
	limit := 100
	if raw := c.QueryParam("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return apperror.FieldInvalid("limit", nil)
		}
	}
	var page sessiontask.ActivityPage
	page, err = h.service.ListActivity(c.Request().Context(), bot, user, limit, c.QueryParam("cursor"), func(s thread.Thread) bool { return canAccessSession(s, user, perms) })
	if err != nil {
		return taskHTTPError(err)
	}
	for i := range page.Items {
		if page.Items[i].Kind == "bot" {
			page.Items[i].Name = entry.DisplayName
			if page.Items[i].Name == "" {
				page.Items[i].Name = entry.Name
			}
		}
	}
	return c.JSON(http.StatusOK, page)
}
