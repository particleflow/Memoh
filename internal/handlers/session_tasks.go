package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/toolcontext"
)

type ComputerTaskObserver interface {
	Recover(context.Context, toolcontext.ComputerBinding, string) (computertask.Recovery, error)
}

type SessionTaskHandler struct {
	computers ComputerTaskObserver
	service   *sessiontask.Service
	bots      *bots.Service
	accounts  *accounts.Service
}

func NewSessionTaskHandler(service *sessiontask.Service, bots *bots.Service, accounts *accounts.Service, computers ComputerTaskObserver) *SessionTaskHandler {
	return &SessionTaskHandler{service: service, bots: bots, accounts: accounts, computers: computers}
}

func (h *SessionTaskHandler) Register(e *echo.Echo) {
	// Global JWT middleware can reject before a route handler runs. Apply the
	// task response policy before routing/auth, including namespace 404/405s.
	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			parts := strings.SplitN(echo.GetPath(c.Request()), "/", 7)
			if len(parts) >= 6 && parts[0] == "" && parts[1] == "bots" && parts[2] != "" && parts[3] == "sessions" && parts[4] != "" && parts[5] == "tasks" {
				c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
			}
			return next(c)
		}
	})
	g := e.Group("/bots/:bot_id/sessions/:session_id/tasks")
	g.GET("", h.List)
	g.GET("/:task_type/:task_id", h.Get)
	g.GET("/:task_type/:task_id/deliveries", h.Deliveries)
	g.GET("/:task_type/:task_id/observation", h.Observe)
	g.GET("/:task_type/:task_id/content", h.Content)
	g.POST("/:task_type/:task_id/followup", h.Followup)
}

func (h *SessionTaskHandler) authorize(c echo.Context) (string, error) {
	user, err := RequireChannelIdentityID(c)
	if err != nil {
		return "", err
	}
	if _, err := uuid.Parse(c.Param("bot_id")); err != nil {
		return "", apperror.FieldInvalid("bot_id", err)
	}
	if _, err := uuid.Parse(c.Param("session_id")); err != nil {
		return "", apperror.FieldInvalid("session_id", err)
	}
	_, err = AuthorizeBotAccessWithPermission(c.Request().Context(), h.bots, h.accounts, user, c.Param("bot_id"), bots.PermissionChat)
	return user, err
}

func taskHTTPError(err error) error {
	switch {
	case errors.Is(err, sessiontask.ErrUnavailable):
		return apperror.New(apperror.CodeSessionNotFound, nil)
	case errors.Is(err, sessiontask.ErrCursor):
		return apperror.FieldInvalid("cursor", nil)
	default:
		return err
	}
}

// List godoc
// @Summary List safe task summaries under an owned conversation
// @Description Read-only persisted facts. Includes owned subagent descendants and computer receipts; rechecks current target grants. No dispatch, device polling, reasoning, raw output or attachments. Continue while next_cursor is nonempty, even for an empty page. States are live; creation order is stable.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param session_id path string true "Parent session ID"
// @Param limit query int false "Page size (1-100)" default(20)
// @Param cursor query string false "Opaque continuation for this caller, Bot and parent"
// @Success 200 {object} sessiontask.Page
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/tasks [get].
func (h *SessionTaskHandler) List(c echo.Context) error {
	user, err := h.authorize(c)
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
	page, err := h.service.List(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), limit, c.QueryParam("cursor"))
	if err != nil {
		return taskHTTPError(err)
	}
	return c.JSON(http.StatusOK, page)
}

// Get godoc
// @Summary Read a safe task summary within an owned conversation
// @Description Same projection as a list item, with fresh authorization. Unknown execution and cancel intent never assert termination or business completion. Subagents expose their own latest persisted run and bounded visible assistant text; computer tasks expose verified result summaries. No raw logs or attachment bytes.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param session_id path string true "Parent session ID"
// @Param task_type path string true "Task discriminator" Enums(subagent,computer_operation)
// @Param task_id path string true "Subagent session or computer operation ID"
// @Success 200 {object} sessiontask.Task
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/tasks/{task_type}/{task_id} [get].
func (h *SessionTaskHandler) Get(c echo.Context) error {
	user, err := h.authorize(c)
	if err != nil {
		return err
	}
	if _, err := uuid.Parse(c.Param("task_id")); err != nil {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	item, err := h.service.Get(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), c.Param("task_type"), c.Param("task_id"))
	if err != nil {
		return taskHTTPError(err)
	}
	return c.JSON(http.StatusOK, item)
}

// Deliveries godoc
// @Summary Read one page of persisted computer task file deliveries
// @Description Rechecks parent/descendant ownership and current target grants. At most 16 files per page, with opaque next_after and has_more. References appear only after committed delivery; they are not download URLs or authority. has_more=false does not assert task completion or a frozen snapshot; refresh from the first page to observe changes to earlier entries. No device polling or dispatch.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param session_id path string true "Parent session ID"
// @Param task_type path string true "Task discriminator" Enums(computer_operation)
// @Param task_id path string true "Computer operation ID"
// @Param after query string false "Opaque next_after from the previous page; omit for first page"
// @Success 200 {object} computertask.DeliveryPage
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/tasks/{task_type}/{task_id}/deliveries [get].
func (h *SessionTaskHandler) Deliveries(c echo.Context) error {
	user, err := h.authorize(c)
	if err != nil {
		return err
	}
	if _, err := uuid.Parse(c.Param("task_id")); err != nil {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	record, entries, err := h.service.DeliveryEntries(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), c.Param("task_type"), c.Param("task_id"), c.QueryParam("after"))
	if errors.Is(err, sessiontask.ErrCursor) {
		return apperror.FieldInvalid("after", nil)
	}
	if err != nil {
		return taskHTTPError(err)
	}
	page, err := computertask.ProjectDeliveryPage(record, c.QueryParam("after"), entries, "")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, page)
}

// Observe godoc
// @Summary Observe a computer task's original native references
// @Description Read-only, bounded Herdr metadata and an operation-revision-bound persisted reconciliation snapshot. Rechecks parent/descendant ownership and current device grants before and after observation. A concurrent operation revision change fails closed. No dispatch, terminal input, logs, cancellation, result collection or receipt writes. Native idle/done and matching references never prove business success or restored conversation history. Reconciliation neither pauses target admission nor authorizes host stop/upgrade; recorded file references are not live media checks.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param session_id path string true "Owned parent session ID"
// @Param task_type path string true "Task discriminator" Enums(computer_operation)
// @Param task_id path string true "Computer operation ID"
// @Success 200 {object} computertask.Recovery
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/tasks/{task_type}/{task_id}/observation [get].
func (h *SessionTaskHandler) Observe(c echo.Context) error {
	user, err := h.authorize(c)
	if err != nil {
		return err
	}
	if _, err := uuid.Parse(c.Param("task_id")); err != nil || h.computers == nil {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	ctx := c.Request().Context()
	bot, parent, kind, id := c.Param("bot_id"), c.Param("session_id"), c.Param("task_type"), c.Param("task_id")
	binding, err := h.service.ComputerBinding(ctx, bot, user, parent, kind, id)
	if err != nil {
		return taskHTTPError(err)
	}
	ctx, err = toolcontext.WithComputerBinding(ctx, binding)
	if err != nil {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	observation, err := h.computers.Recover(ctx, binding, id)
	if errors.Is(err, computertask.ErrDenied) || errors.Is(err, agenvo.ErrTargetChanged) {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	if err != nil {
		return err
	}
	// Native reads can outlive a parent/child relation or grant. Recheck the
	// requested root too, not just the operation's own session inside Recover.
	if _, err := h.authorize(c); err != nil {
		return err
	}
	current, err := h.service.ComputerBinding(ctx, bot, user, parent, kind, id)
	if err != nil || current != binding {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	return c.JSON(http.StatusOK, observation)
}
