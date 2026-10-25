package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/httpx"
	"github.com/felinics/memoh/internal/providers"
)

// DiscoverMediaCatalog reads a remote media catalog without importing it.
// @Summary Read provider media catalog
// @Description Read the saved provider's Magpie catalog. Does not import models or call a model; transcription support remains unverified.
// @Tags providers
// @Produce json
// @Param id path string true "Provider ID"
// @Param protocol query string true "Catalog protocol" Enums(magpie)
// @Success 200 {object} providers.MediaCatalogResponse
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 502 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /providers/{id}/media-catalog [get].
func (h *ProvidersHandler) DiscoverMediaCatalog(c echo.Context) error {
	id, err := httpx.RequiredParam(c, "id")
	if err != nil {
		return err
	}
	if c.QueryParam("protocol") != providers.MediaCatalogProtocolMagpie {
		return apperror.FieldInvalid("protocol", nil)
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	result, err := h.service.DiscoverMediaCatalog(c.Request().Context(), id)
	if err == nil {
		return c.JSON(http.StatusOK, result)
	}
	if errors.Is(err, providers.ErrMediaCatalogConfig) {
		return apperror.FieldInvalid("provider", err)
	}
	code := apperror.Code("")
	var upstream *providers.MediaCatalogHTTPError
	switch {
	case errors.As(err, &upstream):
		code = apperror.CodeAgentProviderRequestRejected
		if upstream.Status == http.StatusUnauthorized {
			code = apperror.CodeAgentProviderAuthFailed
		}
		if upstream.Status == http.StatusForbidden {
			code = apperror.CodeAgentProviderPermissionDenied
		}
	case errors.Is(err, providers.ErrMediaCatalogTransport):
		code = apperror.CodeAgentProviderUnreachable
	case errors.Is(err, providers.ErrMediaCatalogResponse):
		code = apperror.CodeAgentProviderRequestRejected
	}
	if code != "" {
		return apperror.Wrap(code, err, nil)
	}
	return resourceLookupError(err, "id", apperror.CodeProviderNotFound, "read provider media catalog")
}
