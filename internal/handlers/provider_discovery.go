package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/providers"
)

func (h *ProvidersHandler) registerDiscovery(e *echo.Echo) {
	// Run before global JWT rejection, so neither success nor auth failures are
	// cacheable. Only this new read-only namespace is affected.
	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			parts := strings.Split(strings.TrimSuffix(echo.GetPath(c.Request()), "/"), "/")
			if len(parts) == 4 && parts[1] == "providers" && parts[2] != "" && parts[3] == "model-discovery" {
				c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
			}
			return next(c)
		}
	})
	e.GET("/providers/:id/model-discovery", h.DiscoverModels)
}

// DiscoverModels godoc
// @Summary Read upstream model metadata without importing or probing models
// @Description Uses the existing team's provider configuration server-side. GET-only: no inference, embedding probe, import or OAuth refresh. Only explicit exact-ID provider-template declarations are reported; undeclared model capabilities remain unknown. Adapter implementation is separate from model support. The SDK listing is not guaranteed exhaustive. OAuth and non-LLM adapters are unsupported.
// @Tags providers
// @Produce json
// @Param id path string true "Provider ID"
// @Success 200 {object} providers.DiscoverModelsResponse
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 501 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Failure 502 {object} server.Problem
// @Router /providers/{id}/model-discovery [get].
func (h *ProvidersHandler) DiscoverModels(c echo.Context) error {
	if _, err := auth.UserIDFromContext(c); err != nil {
		return err
	}
	// A channel reply token is not a provider-management login.
	if _, err := auth.ChatTokenFromContext(c); err == nil {
		return echo.NewHTTPError(http.StatusForbidden, "provider discovery requires an account session")
	}
	if _, err := db.ParseUUID(c.Param("id")); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid provider ID")
	}
	result, err := h.service.DiscoverModels(c.Request().Context(), c.Param("id"))
	if err == nil {
		return c.JSON(http.StatusOK, result)
	}
	switch {
	case errors.Is(err, providers.ErrDiscoveryUnavailable):
		return echo.NewHTTPError(http.StatusNotFound, "provider is unavailable")
	case errors.Is(err, providers.ErrDiscoveryUnsupported):
		return echo.NewHTTPError(http.StatusNotImplemented, "read-only discovery is unsupported for this adapter")
	case errors.Is(err, providers.ErrDiscoveryUpstream):
		return echo.NewHTTPError(http.StatusBadGateway, "provider model metadata could not be read")
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, "provider metadata could not be read")
	}
}
