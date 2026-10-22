package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/computertask"
)

// ComputerDeliveryHandler is a candidate host adapter, not an application
// route. Register only behind a reviewed authenticator that supplies the
// process-local execution identity. Ordinary JWT/header/URL values do not.
type ComputerDeliveryHandler struct {
	service *computertask.DeliveryService
}

func NewComputerDeliveryHandler(service *computertask.DeliveryService) *ComputerDeliveryHandler {
	return &ComputerDeliveryHandler{service: service}
}

func (h *ComputerDeliveryHandler) Register(e *echo.Echo) {
	const prefix = "/internal/computer-deliveries"
	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			p := echo.GetPath(c.Request())
			if p == prefix || strings.HasPrefix(p, prefix+"/") {
				c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
			}
			return next(c)
		}
	})
	g := e.Group(prefix + "/:operation_id/:delivery_id")
	g.POST("/commit", h.commit)
	g.POST("/resolve", h.resolve)
}

func deliveryHTTPError(err error) error {
	status := http.StatusServiceUnavailable // Unknown outcome: query, never infer absence.
	switch {
	case errors.Is(err, computertask.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, computertask.ErrDeliveryBinding), errors.Is(err, computertask.ErrDenied):
		status = http.StatusForbidden
	case errors.Is(err, computertask.ErrDeliveryConflict), errors.Is(err, computertask.ErrDeliveryVersion), errors.Is(err, computertask.ErrDeliveryAsset):
		status = http.StatusConflict
	case errors.Is(err, computertask.ErrDeliveryLimit):
		status = http.StatusTooManyRequests
	}
	return echo.NewHTTPError(status, "delivery request was not acknowledged")
}

func readDeliveryBody(c echo.Context, dest any) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 4096)
	decoder := json.NewDecoder(c.Request().Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid delivery reference")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid delivery reference")
	}
	return nil
}

func (h *ComputerDeliveryHandler) commit(c echo.Context) error {
	var ref computertask.DeliveryRequest
	if err := readDeliveryBody(c, &ref); err != nil {
		return err
	}
	out, err := h.service.Commit(c.Request().Context(), c.Param("operation_id"), c.Param("delivery_id"), ref)
	if err != nil {
		return deliveryHTTPError(err)
	}
	return c.JSON(http.StatusOK, out)
}

func (h *ComputerDeliveryHandler) resolve(c echo.Context) error {
	var req struct {
		computertask.DeliveryRequest
		Cancel bool `json:"cancel"`
	}
	if err := readDeliveryBody(c, &req); err != nil {
		return err
	}
	out, err := h.service.Resolve(c.Request().Context(), c.Param("operation_id"), c.Param("delivery_id"), req.DeliveryRequest, req.Cancel)
	if err != nil {
		return deliveryHTTPError(err)
	}
	return c.JSON(http.StatusOK, out)
}
