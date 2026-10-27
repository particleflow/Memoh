package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/errs"
	"github.com/felinics/memoh/internal/userruntime"
)

// UserRuntimeHandler manages account computer registrations. Native clients
// use reverse-RPC credentials.
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
	// Apply before global JWT middleware and route selection so account device
	// responses, including namespace 401/404/405s, share the same cache policy.
	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := echo.GetPath(c.Request())
			if path == "/users/me/runtimes" || strings.HasPrefix(path, "/users/me/runtimes/") {
				c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
			}
			return next(c)
		}
	})
	g := e.Group("/users/me/runtimes")
	g.POST("", h.Create)
	g.GET("", h.List)
	g.DELETE("/:id", h.Delete)
	g.PUT("/:id/native-herdr", h.ConfigureNativeHerdr)
	g.GET("/:id/admission", h.ReadAdmission)
	g.PUT("/:id/admission", h.SetAdmission)
}

// ConfigureNativeHerdr godoc
// @Summary Configure Herdr on an existing owned native Runtime
// @Description Stores non-secret local references on an activated Runtime. Does not enroll a device, grant Bots, start Herdr, or change credentials. Native Pi tasks require pi-json-v1.
// @Tags user-runtimes
// @Accept json
// @Produce json
// @Param id path string true "Existing native Runtime ID"
// @Param request body agenvo.NativeHerdrProfile true "Private deployment profile"
// @Success 200 {object} userruntime.Runtime
// @Failure 400 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Router /users/me/runtimes/{id}/native-herdr [put].
func (h *UserRuntimeHandler) ConfigureNativeHerdr(c echo.Context) error {
	// Chat-route credentials may send replies but deliberately bypass account
	// session revalidation. They cannot configure the owner's executable paths.
	if _, err := auth.ChatTokenFromContext(c); err == nil {
		return apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	owner, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var profile agenvo.NativeHerdrProfile
	if err = c.Bind(&profile); err != nil {
		return err
	}
	runtime, err := h.service.ConfigureNativeHerdr(c.Request().Context(), owner, strings.TrimSpace(c.Param("id")), profile)
	if err != nil {
		return runtimeHTTPError(err)
	}
	return c.JSON(http.StatusOK, runtime)
}

// Create godoc
// @Summary Register an account computer
// @Description Register a native Memoh Remote Runtime credential. Legacy providers are retired.
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
	case errors.Is(err, agenvo.ErrNativeProfileConflict):
		return apperror.Wrap(apperror.CodeHTTPConflict, err, nil)
	case errors.Is(err, db.ErrComputerAdmissionPaused):
		return apperror.Wrap(apperror.CodeComputerAdmissionPaused, err, nil)
	case errors.Is(err, db.ErrComputerAdmissionConflict):
		return apperror.Wrap(apperror.CodeComputerAdmissionConflict, err, nil)
	case errors.Is(err, db.ErrComputerAdmissionScope):
		return apperror.Wrap(apperror.CodeComputerAdmissionScope, err, nil)
	case errors.Is(err, agenvo.ErrLegacyRetired):
		return apperror.FieldInvalid("provider", err)
	case errors.Is(err, agenvo.ErrInvalidTarget):
		return apperror.FieldInvalid("computer", err)
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
