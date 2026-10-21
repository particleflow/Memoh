package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/errs"
	"github.com/felinics/memoh/internal/userruntime"
)

// UserRuntimeHandler manages account computer registrations. Native clients
// use reverse-RPC credentials; limited providers refer to an existing connection.
// Bot grants and session selection remain separate concerns.
type UserRuntimeHandler struct {
	log     *slog.Logger
	service *userruntime.Service
}

func NewUserRuntimeHandler(log *slog.Logger, service *userruntime.Service) *UserRuntimeHandler {
	if log == nil {
		log = slog.Default()
	}
	return &UserRuntimeHandler{log: log.With(slog.String("handler", "user_runtime")), service: service}
}

func (h *UserRuntimeHandler) Register(e *echo.Echo) {
	g := e.Group("/users/me/runtimes")
	g.POST("", h.Create)
	g.GET("", h.List)
	g.DELETE("/:id", h.Delete)
	g.POST("/agenvo/discover", h.DiscoverAgenvo)
}

type DiscoverAgenvoRequest struct {
	ConnectionID string `json:"connection_id" validate:"required"`
}

// DiscoverAgenvo godoc
// @Summary List Herdr instances from an existing account-owned MCP connection
// @Tags user-runtimes
// @Accept json
// @Produce json
// @Param request body DiscoverAgenvoRequest true "Existing MCP connection"
// @Success 200 {array} agenvo.DiscoveredInstance
// @Failure 400 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/runtimes/agenvo/discover [post].
func (h *UserRuntimeHandler) DiscoverAgenvo(c echo.Context) error {
	ownerID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var req DiscoverAgenvoRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.ConnectionID == "" {
		return apperror.FieldRequired("connection_id")
	}
	items, err := h.service.DiscoverAgenvo(c.Request().Context(), ownerID, req.ConnectionID)
	if err != nil {
		return runtimeHTTPError(err)
	}
	return c.JSON(http.StatusOK, items)
}

// Create godoc
// @Summary Register an account computer
// @Description Register a native Remote Runtime credential or an Agenvo command target backed by an existing account-owned MCP connection. Agenvo registrations return no API token.
// @Tags user-runtimes
// @Accept json
// @Produce json
// @Param request body userruntime.CreateRuntimeRequest true "Runtime configuration"
// @Success 201 {object} userruntime.Runtime
// @Failure 400 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Router /users/me/runtimes [post].
func (h *UserRuntimeHandler) Create(c echo.Context) error {
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var req userruntime.CreateRuntimeRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	resp, err := h.service.CreateRuntime(c.Request().Context(), userID, req)
	if err != nil {
		if errors.Is(err, userruntime.ErrInvalidName) {
			return apperror.FieldInvalid("name", err)
		}
		return runtimeHTTPError(err)
	}
	return c.JSON(http.StatusCreated, resp)
}

// List godoc
// @Summary List Remote Runtime credentials
// @Tags user-runtimes
// @Produce json
// @Success 200 {array} userruntime.Runtime
// @Failure 500 {object} server.Problem
// @Router /users/me/runtimes [get].
func (h *UserRuntimeHandler) List(c echo.Context) error {
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	items, err := h.service.ListRuntimes(c.Request().Context(), userID)
	if err != nil {
		return runtimeHTTPError(err)
	}
	return c.JSON(http.StatusOK, items)
}

// Delete godoc
// @Summary Revoke a Remote Runtime credential
// @Tags user-runtimes
// @Param id path string true "Runtime ID"
// @Success 204 "No Content"
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Router /users/me/runtimes/{id} [delete].
func (h *UserRuntimeHandler) Delete(c echo.Context) error {
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	if err := h.service.RevokeRuntime(c.Request().Context(), userID, strings.TrimSpace(c.Param("id"))); err != nil {
		if errors.Is(err, userruntime.ErrInvalidInput) {
			return apperror.FieldInvalid("id", err)
		}
		return runtimeHTTPError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func runtimeHTTPError(err error) error {
	switch {
	case errors.Is(err, agenvo.ErrInvalidTarget):
		return apperror.FieldInvalid("agenvo", err)
	case errors.Is(err, agenvo.ErrForbidden):
		return echo.NewHTTPError(http.StatusForbidden).WithInternal(err)
	case errors.Is(err, agenvo.ErrUnavailable), errors.Is(err, agenvo.ErrUnknown), errors.Is(err, agenvo.ErrRejected):
		return workspaceUnavailableError(err)
	}
	if errors.Is(err, db.ErrNotFound) {
		return apperror.Wrap(apperror.CodeUserRuntimeNotFound, err, nil)
	}
	if db.IsUniqueViolation(err) {
		return echo.NewHTTPError(http.StatusConflict, "runtime already exists")
	}
	return errs.Wrap(err, "runtime request")
}
