package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/toolcontext"
)

type nativeTaskAccess interface {
	NativeContent(context.Context, toolcontext.ComputerBinding, string) (computertask.NativeContent, error)
	FollowupNative(context.Context, toolcontext.ComputerBinding, computertask.Followup) (computertask.Result, error)
}

type NativeTaskFollowupInput struct {
	RequestID string `json:"request_id" validate:"required" format:"uuid"`
	Mode      string `json:"mode" validate:"required" enums:"current_agent,bound_session"`
	Task      string `json:"task" validate:"required"`
}

type NativeTaskFollowupResponse struct {
	OperationID      string                       `json:"operation_id"`
	State            string                       `json:"state"`
	RequestConflict  bool                         `json:"request_conflict"`
	BlockedByRequest string                       `json:"blocked_by_request,omitempty"`
	Followup         *computertask.FollowupResult `json:"followup,omitempty"`
}

func (h *SessionTaskHandler) nativeBinding(c echo.Context) (context.Context, toolcontext.ComputerBinding, error) {
	user, err := h.authorize(c)
	if err != nil {
		return nil, toolcontext.ComputerBinding{}, err
	}
	b, err := h.service.ComputerBinding(c.Request().Context(), c.Param("bot_id"), user, c.Param("session_id"), c.Param("task_type"), c.Param("task_id"))
	if err != nil {
		return nil, b, taskHTTPError(err)
	}
	ctx, err := toolcontext.WithComputerBinding(c.Request().Context(), b)
	if err != nil {
		return nil, b, apperror.New(apperror.CodeSessionNotFound, nil)
	}
	return ctx, b, nil
}

func (h *SessionTaskHandler) recheckNative(c echo.Context, b toolcontext.ComputerBinding) error {
	_, current, err := h.nativeBinding(c)
	if err != nil {
		return err
	}
	if current != b {
		return apperror.New(apperror.CodeSessionNotFound, nil)
	}
	return nil
}

func nativeTaskError(err error) error {
	switch {
	case errors.Is(err, computertask.ErrDenied), errors.Is(err, agenvo.ErrTargetChanged):
		return apperror.New(apperror.CodeSessionNotFound, nil)
	case errors.Is(err, computertask.ErrInvalid):
		return apperror.New(apperror.CodeHTTPBadRequest, nil)
	case errors.Is(err, computertask.ErrApproval):
		return apperror.New(apperror.CodeHTTPForbidden, nil)
	default:
		return err
	}
}

// Content godoc
// @Summary Read a bounded native Agent terminal sample
// @Description Uses existing agent.get/read without recording evidence or collecting files. A terminal_snapshot_unverified sample can include tool output and is not a complete log, verified assistant answer or task completion. Batch Pi returns supported=false.
// @Tags sessions
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param session_id path string true "Owned parent session ID"
// @Param task_type path string true "Task discriminator" Enums(computer_operation)
// @Param task_id path string true "Computer operation ID"
// @Success 200 {object} computertask.NativeContent
// @Failure 401,403,404,500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/tasks/{task_type}/{task_id}/content [get].
func (h *SessionTaskHandler) Content(c echo.Context) error {
	ctx, b, err := h.nativeBinding(c)
	if err != nil {
		return err
	}
	native, ok := h.computers.(nativeTaskAccess)
	if !ok {
		return apperror.New(apperror.CodeHTTPNotFound, nil)
	}
	out, err := native.NativeContent(ctx, b, c.Param("task_id"))
	if err != nil {
		return nativeTaskError(err)
	}
	if err = h.recheckNative(c, b); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

// Followup godoc
// @Summary Read a retained interactive Agent input receipt
// @Description Returns the saved receipt for an exact request ID and matching input. New requests are unsupported. An unknown outcome remains unknown and must not be resent.
// @Tags sessions
// @Accept json
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param session_id path string true "Owned parent session ID"
// @Param task_type path string true "Task discriminator" Enums(computer_operation)
// @Param task_id path string true "Computer operation ID"
// @Param body body NativeTaskFollowupInput true "Original input and retained request ID"
// @Success 200 {object} NativeTaskFollowupResponse
// @Failure 400,401,403,404,500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/tasks/{task_type}/{task_id}/followup [post].
func (h *SessionTaskHandler) Followup(c echo.Context) error {
	ctx, b, err := h.nativeBinding(c)
	if err != nil {
		return err
	}
	native, ok := h.computers.(nativeTaskAccess)
	if !ok {
		return apperror.New(apperror.CodeHTTPNotFound, nil)
	}
	var in NativeTaskFollowupInput
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 128*1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return apperror.New(apperror.CodeHTTPBadRequest, nil)
	}
	out, callErr := native.FollowupNative(ctx, b, computertask.Followup{OperationID: c.Param("task_id"), RequestID: in.RequestID, Mode: in.Mode, Task: in.Task})
	if err = h.recheckNative(c, b); err != nil {
		return err
	}
	if errors.Is(callErr, computertask.ErrDenied) || errors.Is(callErr, agenvo.ErrTargetChanged) || errors.Is(callErr, computertask.ErrApproval) {
		return nativeTaskError(callErr)
	}
	state := "unknown"
	switch {
	case out.RequestConflict:
		state = "conflict"
	case out.BlockedByRequest != "":
		state = "blocked"
	case out.Followup != nil:
		state = out.Followup.State
	case errors.Is(callErr, agenvo.ErrUnsupported):
		state = "unsupported"
	case errors.Is(callErr, computertask.ErrInvalid):
		return nativeTaskError(callErr)
	}
	return c.JSON(http.StatusOK, NativeTaskFollowupResponse{OperationID: c.Param("task_id"), State: state, RequestConflict: out.RequestConflict, BlockedByRequest: out.BlockedByRequest, Followup: out.Followup})
}
