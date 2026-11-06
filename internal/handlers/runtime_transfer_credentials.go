package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/userruntime"
)

// RuntimeTransferCredentialHandler is an owner-only setup surface under the
// existing account JWT middleware. Device credentials cannot administer it.
type RuntimeTransferCredentialHandler struct {
	credentials *userruntime.TransferCredentials
}

func NewRuntimeTransferCredentialHandler(credentials *userruntime.TransferCredentials) *RuntimeTransferCredentialHandler {
	return &RuntimeTransferCredentialHandler{credentials: credentials}
}

func (h *RuntimeTransferCredentialHandler) Register(e *echo.Echo) {
	g := e.Group("/users/me/runtimes/:id/transfer-credential", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
			return next(c)
		}
	})
	g.GET("", h.status)
	g.POST("", h.enroll)
	g.DELETE("", h.revoke)
}

// status godoc
// @Summary Read owned computer file credential status (no secret)
// @Tags user-runtimes
// @Produce json
// @Param id path string true "Existing native Runtime ID"
// @Success 200 {object} userruntime.TransferCredentialStatus
// @Failure 403 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/runtimes/{id}/transfer-credential [get].
func (h *RuntimeTransferCredentialHandler) status(c echo.Context) error {
	owner, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	status, err := h.credentials.Status(c.Request().Context(), owner, c.Param("id"))
	if err != nil {
		return transferCredentialHTTPError(err)
	}
	return c.JSON(http.StatusOK, status)
}

// enroll godoc
// @Summary Enroll or explicitly rotate an owned computer file credential
// @Description Returns the secret once. Existing credentials require expected_id CAS; a lost response requires status read and explicit rotation. Never a per-file action.
// @Tags user-runtimes
// @Accept json
// @Produce json
// @Param id path string true "Existing native Runtime ID"
// @Param request body userruntime.ChangeTransferCredentialRequest true "Expected current credential ID and optional device expiry"
// @Success 201 {object} userruntime.TransferCredentialEnrollment
// @Failure 400 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/runtimes/{id}/transfer-credential [post].
func (h *RuntimeTransferCredentialHandler) enroll(c echo.Context) error {
	owner, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var req userruntime.ChangeTransferCredentialRequest
	if err = c.Bind(&req); err != nil {
		return err
	}
	result, err := h.credentials.Enroll(c.Request().Context(), owner, c.Param("id"), req)
	if err != nil {
		return transferCredentialHTTPError(err)
	}
	return c.JSON(http.StatusCreated, result)
}

// revoke godoc
// @Summary Revoke an owned computer file credential with current-ID CAS
// @Tags user-runtimes
// @Param id path string true "Existing native Runtime ID"
// @Param expected_id query string true "Current non-secret credential ID"
// @Success 204 "Revoked"
// @Failure 403 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/runtimes/{id}/transfer-credential [delete].
func (h *RuntimeTransferCredentialHandler) revoke(c echo.Context) error {
	owner, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	if err = h.credentials.Revoke(c.Request().Context(), owner, c.Param("id"), c.QueryParam("expected_id")); err != nil {
		return transferCredentialHTTPError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func transferCredentialHTTPError(err error) error {
	switch {
	case errors.Is(err, userruntime.ErrTransferUnavailable):
		return echo.NewHTTPError(http.StatusServiceUnavailable, "device transfers disabled")
	case errors.Is(err, userruntime.ErrTransferCredential):
		return echo.NewHTTPError(http.StatusForbidden, "computer unavailable")
	case errors.Is(err, userruntime.ErrTransferConflict):
		return echo.NewHTTPError(http.StatusConflict, "credential changed; read current status")
	case errors.Is(err, userruntime.ErrInvalidInput):
		return apperror.FieldInvalid("transfer_credential", err)
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, "credential update outcome unknown; read current status")
	}
}

// RuntimeDeviceTransferVerifier adapts the account registry without making that
// lower-level registry depend on execution/Agent packages.
type RuntimeDeviceTransferVerifier struct {
	Credentials *userruntime.TransferCredentials
}

func (v RuntimeDeviceTransferVerifier) VerifyDevice(ctx context.Context, secret string) (computertask.VerifiedTransferDevice, error) {
	d, err := v.Credentials.VerifyDevice(ctx, secret)
	if errors.Is(err, userruntime.ErrTransferUnavailable) {
		return computertask.VerifiedTransferDevice{}, ErrTransferVerificationUnavailable
	}
	return computertask.VerifiedTransferDevice(d), err
}

func (v RuntimeDeviceTransferVerifier) RevalidateDevice(ctx context.Context, d computertask.VerifiedTransferDevice) error {
	err := v.Credentials.RevalidateDevice(ctx, userruntime.TransferDevice(d))
	if errors.Is(err, userruntime.ErrTransferUnavailable) {
		return ErrTransferVerificationUnavailable
	}
	return err
}
