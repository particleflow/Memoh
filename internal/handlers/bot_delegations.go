package handlers

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
)

type BotDelegationHandler struct {
	service  *botdelegation.Service
	bots     *bots.Service
	accounts *accounts.Service
}

func NewBotDelegationHandler(service *botdelegation.Service, bots *bots.Service, accounts *accounts.Service) *BotDelegationHandler {
	return &BotDelegationHandler{service: service, bots: bots, accounts: accounts}
}

func (h *BotDelegationHandler) Register(e *echo.Echo) {
	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			parts := strings.SplitN(echo.GetPath(c.Request()), "/", 7)
			if len(parts) >= 6 && parts[0] == "" && parts[1] == "bots" && parts[2] != "" && parts[3] == "sessions" && parts[4] != "" && parts[5] == "delegations" {
				c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
			}
			return next(c)
		}
	})
	g := e.Group("/bots/:bot_id/sessions/:session_id/delegations")
	g.POST("", h.Submit)
	g.GET("", h.List)
	g.GET("/:delegation_id", h.Get)
	g.POST("/:delegation_id/delivery", h.Deliver)
	g.POST("/:delegation_id/delivery/recover", h.RecoverDelivery)
	g.GET("/:delegation_id/attachments/:content_hash", h.Attachment)
	g.HEAD("/:delegation_id/attachments/:content_hash", h.Attachment)
}

func (h *BotDelegationHandler) authorize(c echo.Context) (string, error) {
	user, err := RequireChannelIdentityID(c)
	if err != nil {
		return "", err
	}
	if _, err = uuid.Parse(c.Param("bot_id")); err != nil {
		return "", apperror.FieldInvalid("bot_id", err)
	}
	if _, err = uuid.Parse(c.Param("session_id")); err != nil {
		return "", apperror.FieldInvalid("session_id", err)
	}
	_, err = AuthorizeBotAccessWithPermission(c.Request().Context(), h.bots, h.accounts, user, c.Param("bot_id"), bots.PermissionChat)
	if err == nil && c.Param("delegation_id") != "" {
		if _, parseErr := uuid.Parse(c.Param("delegation_id")); parseErr != nil {
			return "", apperror.New(apperror.CodeSessionNotFound, nil)
		}
	}
	return user, err
}

func delegationHTTPError(err error) error {
	switch {
	case errors.Is(err, botdelegation.ErrDenied):
		return apperror.New(apperror.CodeSessionNotFound, nil)
	case errors.Is(err, botdelegation.ErrConflict):
		return apperror.New(apperror.CodeHTTPConflict, nil)
	case errors.Is(err, botdelegation.ErrCursor):
		return apperror.FieldInvalid("cursor", nil)
	case errors.Is(err, botdelegation.ErrInput):
		return apperror.New(apperror.CodeHTTPBadRequest, nil)
	default:
		return err
	}
}

// Submit godoc
// @Summary Delegate one task to an authorized Bot
// @Description Durable asynchronous handoff. Acceptance is not completion; repeat the same request_id to observe the same task. Attachments reference existing source conversation originals.
// @Tags sessions
// @Accept json
// @Produce json
// @Param bot_id path string true "Supervisor Bot ID"
// @Param session_id path string true "Owned source session"
// @Param body body botdelegation.Input true "Handoff"
// @Success 202 {object} botdelegation.View
// @Failure 400,401,403,404,409,500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/delegations [post].
func (h *BotDelegationHandler) Submit(c echo.Context) error {
	user, err := h.authorize(c)
	if err != nil {
		return err
	}
	var input botdelegation.Input
	if err = c.Bind(&input); err != nil {
		return err
	}
	v, err := h.service.Submit(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), input)
	if err != nil {
		return delegationHTTPError(err)
	}
	return c.JSON(http.StatusAccepted, v)
}

// List godoc
// @Summary List authorized Bot handoffs from a conversation
// @Description Persisted facts only. Follow nonempty next_cursor even for an empty page. Reads do not dispatch, poll devices or retry delivery.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Supervisor Bot ID"
// @Param session_id path string true "Owned source session"
// @Param limit query int false "Page size 1-100" default(20)
// @Param cursor query string false "Opaque continuation"
// @Success 200 {object} botdelegation.Page
// @Failure 400,401,403,404,500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/delegations [get].
func (h *BotDelegationHandler) List(c echo.Context) error {
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
	v, err := h.service.List(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), limit, c.QueryParam("cursor"))
	if err != nil {
		return delegationHTTPError(err)
	}
	return c.JSON(http.StatusOK, v)
}

// Get godoc
// @Summary Observe a Bot handoff and its final visible summary
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Supervisor Bot ID"
// @Param session_id path string true "Owned source session"
// @Param delegation_id path string true "Delegation ID"
// @Success 200 {object} botdelegation.View
// @Failure 400,401,403,404,500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/delegations/{delegation_id} [get].
func (h *BotDelegationHandler) Get(c echo.Context) error {
	user, err := h.authorize(c)
	if err != nil {
		return err
	}
	v, err := h.service.Get(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), c.Param("delegation_id"))
	if err != nil {
		return delegationHTTPError(err)
	}
	return c.JSON(http.StatusOK, v)
}

// Deliver godoc
// @Summary Retry only result delivery to the supervisor
// @Description Uses the original stable delivery ID. Never reruns the specialist; unknown execution cannot be retried here. Accepted delivery means queued, not supervisor completion.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Supervisor Bot ID"
// @Param session_id path string true "Owned source session"
// @Param delegation_id path string true "Delegation ID"
// @Success 200 {object} botdelegation.View
// @Failure 400,401,403,404,409,500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/delegations/{delegation_id}/delivery [post].
func (h *BotDelegationHandler) Deliver(c echo.Context) error {
	user, err := h.authorize(c)
	if err != nil {
		return err
	}
	v, err := h.service.RetryDelivery(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), c.Param("delegation_id"))
	if err != nil {
		return delegationHTTPError(err)
	}
	return c.JSON(http.StatusOK, v)
}

// RecoverDelivery godoc
// @Summary Authorize one summary attempt for a saved completed result
// @Description Requires a completed native Web result, original event and exhausted expected attempt (at least 3). New authorizations conflict while active; repeating the same authorization resumes it. Published results conflict. Never reruns the specialist.
// @Tags sessions
// @Accept json
// @Produce json
// @Param bot_id path string true "Supervisor Bot ID"
// @Param session_id path string true "Owned source session"
// @Param delegation_id path string true "Delegation ID"
// @Param body body botdelegation.RecoveryInput true "Exact saved event and expected attempt"
// @Success 202 {object} botdelegation.RecoveryView
// @Failure 400,401,403,404,409,500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/delegations/{delegation_id}/delivery/recover [post].
func (h *BotDelegationHandler) RecoverDelivery(c echo.Context) error {
	user, err := h.authorize(c)
	if err != nil {
		return err
	}
	var input botdelegation.RecoveryInput
	if err = c.Bind(&input); err != nil {
		return err
	}
	v, err := h.service.RecoverDelivery(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), c.Param("delegation_id"), input)
	if err != nil {
		return delegationHTTPError(err)
	}
	return c.JSON(http.StatusAccepted, v)
}

// Attachment godoc
// @Summary Read one original returned by an authorized Bot handoff
// @Description Checks current source ancestry, both Bot grants and the trusted returned reference. No public URL or copied original; authenticated download only.
// @Tags sessions
// @Produce application/octet-stream
// @Param bot_id path string true "Supervisor Bot ID"
// @Param session_id path string true "Owned source session"
// @Param delegation_id path string true "Delegation ID"
// @Param content_hash path string true "Returned content hash"
// @Param Range header string false "Requested byte range"
// @Param If-Range header string false "Previously received ETag"
// @Success 200 {file} binary
// @Success 206 {file} binary
// @Failure 400,401,403,404,500 {object} server.Problem
// @Failure 416 {string} string "Unsatisfiable byte range"
// @Router /bots/{bot_id}/sessions/{session_id}/delegations/{delegation_id}/attachments/{content_hash} [get].
// @Router /bots/{bot_id}/sessions/{session_id}/delegations/{delegation_id}/attachments/{content_hash} [head].
func (h *BotDelegationHandler) Attachment(c echo.Context) error {
	user, err := h.authorize(c)
	if err != nil {
		return err
	}
	r, err := h.service.ReadRecord(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), c.Param("delegation_id"))
	if err != nil {
		return delegationHTTPError(err)
	}
	hash := c.Param("content_hash")
	result := r.State == "completed" && slices.Contains(r.Result.Outputs, hash)
	if !result && !slices.Contains(r.Payload.Inputs, hash) {
		return delegationHTTPError(botdelegation.ErrDenied)
	}
	reader, asset, err := h.service.Attachments(r).OpenSeekable(c.Request().Context(), hash, result)
	if err != nil {
		return delegationHTTPError(err)
	}
	defer func() { _ = reader.Close() }()
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="result"`)
	return serveOriginalMedia(c, reader, asset.Mime, asset.ContentHash, "no-store")
}
