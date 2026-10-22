package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/toolcontext"
)

// ComputerDeliveryHandler is a candidate host adapter, not an application
// route. Register only behind a reviewed authenticator that supplies the
// process-local execution identity. Ordinary JWT/header/URL values do not.
type ComputerDeliveryHandler struct {
	service *computertask.DeliveryService
	async   *computertask.AsyncDeliveryService
}

func NewComputerDeliveryHandler(service *computertask.DeliveryService) *ComputerDeliveryHandler {
	return &ComputerDeliveryHandler{service: service}
}

// NewAsyncComputerDeliveryHandler remains unregistered in the application.
func NewAsyncComputerDeliveryHandler(service *computertask.AsyncDeliveryService) *ComputerDeliveryHandler {
	return &ComputerDeliveryHandler{async: service}
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
	return echo.NewHTTPError(status)
}

func readDeliveryBody(c echo.Context, dest any) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 4096)
	decoder := json.NewDecoder(c.Request().Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return apperror.FieldInvalid("reference", nil)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return apperror.FieldInvalid("reference", nil)
	}
	return nil
}

func (h *ComputerDeliveryHandler) commit(c echo.Context) error {
	var ref struct {
		computertask.DeliveryRequest
		Publication string `json:"publication,omitempty"`
		BotID       string `json:"bot_id,omitempty"`
	}
	if err := readDeliveryBody(c, &ref); err != nil {
		return err
	}
	var out computertask.DeliveryOutcome
	var err error
	switch {
	case h.async != nil:
		if b, ok := toolcontext.HostComputerBinding(c.Request().Context()); !ok || b.BotID != ref.BotID {
			return deliveryHTTPError(computertask.ErrDeliveryBinding)
		}
		out, err = h.async.Commit(c.Request().Context(), c.Param("operation_id"), c.Param("delivery_id"), ref.DeliveryRequest, ref.Publication)
	case h.service != nil && ref.Publication == "" && ref.BotID == "":
		out, err = h.service.Commit(c.Request().Context(), c.Param("operation_id"), c.Param("delivery_id"), ref.DeliveryRequest)
	default:
		err = computertask.ErrDeliveryBinding
	}
	if err != nil {
		return deliveryHTTPError(err)
	}
	if err := finishTaskDelivery(c, out); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

func (h *ComputerDeliveryHandler) resolve(c echo.Context) error {
	var req struct {
		computertask.DeliveryRequest
		Cancel      bool   `json:"cancel"`
		Publication string `json:"publication,omitempty"`
		BotID       string `json:"bot_id,omitempty"`
	}
	if err := readDeliveryBody(c, &req); err != nil {
		return err
	}
	var out computertask.DeliveryOutcome
	var err error
	switch {
	case h.async != nil:
		if b, ok := toolcontext.HostComputerBinding(c.Request().Context()); !ok || b.BotID != req.BotID {
			return deliveryHTTPError(computertask.ErrDeliveryBinding)
		}
		out, err = h.async.Resolve(c.Request().Context(), c.Param("operation_id"), c.Param("delivery_id"), req.DeliveryRequest, req.Publication, req.Cancel)
	case h.service != nil && req.Publication == "" && req.BotID == "":
		out, err = h.service.Resolve(c.Request().Context(), c.Param("operation_id"), c.Param("delivery_id"), req.DeliveryRequest, req.Cancel)
	default:
		err = computertask.ErrDeliveryBinding
	}
	if err != nil {
		return deliveryHTTPError(err)
	}
	if err := finishTaskDelivery(c, out); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}
