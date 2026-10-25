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
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

type nativeBatchResumer interface {
	ResumeNative(context.Context, toolcontext.ComputerBinding, string, computertask.Submit) (computertask.Result, error)
}

// NativeTaskResumeInput carries the original tool arguments, not identity or
// native references. The existing service verifies the original request hash.
type NativeTaskResumeInput struct {
	Kind      string                  `json:"kind" validate:"required" enums:"herdr_pi_task"`
	Task      string                  `json:"task" validate:"required"`
	TargetID  string                  `json:"target_id,omitempty"`
	WorkDir   string                  `json:"work_dir,omitempty"`
	Artifact  *deliverymeta.Artifact  `json:"artifact,omitempty"`
	Artifacts []deliverymeta.Artifact `json:"artifacts,omitempty"`
}

type NativeTaskResumeResponse struct {
	OperationID     string `json:"operation_id"`
	State           string `json:"state"`
	Phase           string `json:"phase,omitempty"`
	Outcome         string `json:"outcome" enums:"submitted,unchanged,unknown,unavailable,blocked"`
	AutomaticRetry  bool   `json:"automatic_retry"`
	BusinessOutcome string `json:"business_outcome" enums:"unknown"`
}

// Resume godoc
// @Summary Explicitly continue one original native batch before prompt submission
// @Description Requires a current account JWT and the complete unchanged original computer_task_submit arguments. Chat-route reply credentials cannot resume execution. Resolves host authority from the authenticated user, conversation ancestry, operation ownership and target grant; reuses the persisted original run even when terminal. No model call, new run, new operation or workspace. Existing admission/CAS and uncertain-dispatch fences apply. submitted is input acknowledgement, not business success; never automatically retry unknown outcomes. Disconnected Runtime remains unavailable. Read the same task afterward. No raw native output is returned.
// @Tags sessions
// @Accept json
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param session_id path string true "Owned parent session ID"
// @Param task_type path string true "Task discriminator" Enums(computer_operation)
// @Param task_id path string true "Original computer operation ID"
// @Param body body NativeTaskResumeInput true "Complete original tool arguments"
// @Success 200 {object} NativeTaskResumeResponse
// @Failure 400,401,403,404,409,500 {object} server.Problem
// @Router /bots/{bot_id}/sessions/{session_id}/tasks/{task_type}/{task_id}/resume [post].
func (h *SessionTaskHandler) Resume(c echo.Context) error {
	// This is an explicit logged-in management action. Chat-route reply
	// credentials skip account-session validation and cannot authorize it.
	if _, err := auth.ChatTokenFromContext(c); err == nil {
		return apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	ctx, b, err := h.nativeBinding(c)
	if err != nil {
		return err
	}
	native, ok := h.computers.(nativeBatchResumer)
	if !ok {
		return apperror.New(apperror.CodeHTTPNotFound, nil)
	}
	var in NativeTaskResumeInput
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 128*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return apperror.FieldInvalid("body", err)
	}
	if !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return apperror.FieldInvalid("body", nil)
	}
	if in.Kind != "herdr_pi_task" {
		return apperror.FieldInvalid("kind", nil)
	}
	if in.Task == "" {
		return apperror.FieldRequired("task")
	}
	out, callErr := native.ResumeNative(ctx, b, c.Param("task_id"), computertask.Submit{Kind: in.Kind, Task: in.Task, TargetID: in.TargetID, WorkDir: in.WorkDir, Artifact: in.Artifact, Artifacts: in.Artifacts})
	if err := h.recheckNative(c, b); err != nil {
		return err
	}
	if errors.Is(callErr, computertask.ErrDenied) || errors.Is(callErr, agenvo.ErrTargetChanged) || errors.Is(callErr, computertask.ErrApproval) || errors.Is(callErr, computertask.ErrInvalid) {
		return nativeTaskError(callErr)
	}
	if errors.Is(callErr, computertask.ErrConflict) {
		return apperror.New(apperror.CodeHTTPConflict, nil)
	}
	response := NativeTaskResumeResponse{OperationID: c.Param("task_id"), State: "unknown", Outcome: "unknown", BusinessOutcome: "unknown"}
	if out.ID == response.OperationID {
		response.State, response.Phase = out.State, out.Phase
	}
	switch {
	case callErr == nil:
		response.Outcome = "unchanged"
		if out.State == "submitted" && out.Phase == "batch_submitted" {
			response.Outcome = "submitted"
		}
	case errors.Is(callErr, agenvo.ErrUnavailable), errors.Is(callErr, agenvo.ErrNativeUnavailable), errors.Is(callErr, agenvo.ErrAuthenticationUnavailable), errors.Is(callErr, computertask.ErrPreflightUnavailable):
		response.Outcome = "unavailable"
	case errors.Is(callErr, db.ErrComputerAdmissionPaused), errors.Is(callErr, db.ErrComputerAdmissionScope), errors.Is(callErr, agenvo.ErrUnsupported):
		response.Outcome = "blocked"
	}
	return c.JSON(http.StatusOK, response)
}
