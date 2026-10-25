package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/apperror"
)

// ReadAdmission godoc
// @Summary Read owned computer admission state
// @Description Reads the existing durable admission gate without contacting or stopping the host. A pause is not proof that accepted work has drained.
// @Tags user-runtimes
// @Produce json
// @Param id path string true "Existing owner runtime ID"
// @Success 200 {object} agenvo.Admission
// @Failure 403 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Router /users/me/runtimes/{id}/admission [get].
func (h *UserRuntimeHandler) ReadAdmission(c echo.Context) error {
	owner, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	out, err := h.service.ReadComputerAdmission(c.Request().Context(), owner, strings.TrimSpace(c.Param("id")))
	if err != nil {
		return runtimeHTTPError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// SetAdmission godoc
// @Summary Pause or resume new work for an owned native instance
// @Description Atomically fences new operation claims and follow-up reservations across matching mounts. Agenvo uses the existing owner connection and a read-only probe; configured native Runtimes use the owned registry identity and remain manageable while offline. Retain request_id after an uncertain response. Resume requires the pause request ID and current revision. Does not stop processes, drain accepted work or authorize a host restart.
// @Tags user-runtimes
// @Accept json
// @Produce json
// @Param id path string true "Existing owner runtime ID"
// @Param request body agenvo.ChangeAdmission true "Compare-and-swap admission request"
// @Success 200 {object} agenvo.Admission
// @Failure 400 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/runtimes/{id}/admission [put].
func (h *UserRuntimeHandler) SetAdmission(c echo.Context) error {
	owner, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var in agenvo.ChangeAdmission
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 4096))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&in); err != nil {
		return apperror.FieldInvalid("request", err)
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return apperror.FieldInvalid("request", err)
	}
	out, err := h.service.SetComputerAdmission(c.Request().Context(), owner, strings.TrimSpace(c.Param("id")), in)
	if err != nil {
		return runtimeHTTPError(err)
	}
	return c.JSON(http.StatusOK, out)
}
