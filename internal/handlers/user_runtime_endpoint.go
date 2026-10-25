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

// ReadAgenvoEndpoint godoc
// @Summary Read the owned endpoint location configuration
// @Description No native call. Configuration is not filesystem verification, host identity proof or stop authorization. An absent locator means unconfigured.
// @Tags user-runtimes
// @Produce json
// @Param id path string true "Existing owned runtime ID"
// @Success 200 {object} agenvo.EndpointConfiguration
// @Failure 400 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Router /users/me/runtimes/{id}/agenvo-endpoint [get].
func (h *UserRuntimeHandler) ReadAgenvoEndpoint(c echo.Context) error {
	owner, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	out, err := h.service.ReadAgenvoEndpoint(c.Request().Context(), owner, strings.TrimSpace(c.Param("id")))
	if err != nil {
		return runtimeHTTPError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// ConfigureAgenvoEndpoint godoc
// @Summary Configure an existing owned endpoint location
// @Description Compare-and-swap the existing registration's owner-only program_path and host_root. Explicit empty locator clears configuration; null/omitted locator is invalid. Only an existing native read-only probe is issued. No program runs, process stops, workspace changes or credentials are created. Live admission pause blocks changes. Unknown responses require GET reconciliation, not host/task replay.
// @Tags user-runtimes
// @Accept json
// @Produce json
// @Param id path string true "Existing owned runtime ID"
// @Param request body agenvo.ChangeEndpointConfiguration true "Expected configuration fingerprint and locator"
// @Success 200 {object} agenvo.EndpointConfiguration
// @Failure 400 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/runtimes/{id}/agenvo-endpoint [put].
func (h *UserRuntimeHandler) ConfigureAgenvoEndpoint(c echo.Context) error {
	owner, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var in agenvo.ChangeEndpointConfiguration
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 32768))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&in); err != nil {
		return apperror.FieldInvalid("request", err)
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return apperror.FieldInvalid("request", err)
	}
	out, err := h.service.ConfigureAgenvoEndpoint(c.Request().Context(), owner, strings.TrimSpace(c.Param("id")), in)
	if err != nil {
		return runtimeHTTPError(err)
	}
	return c.JSON(http.StatusOK, out)
}
