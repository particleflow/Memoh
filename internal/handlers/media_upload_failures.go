package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/media"
)

// ReportMediaUploadFailure stores client-reported failure metadata without
// creating a media receipt, session, message, asset or model turn.
// @Summary Record a scoped attachment failure
// @Tags messages
// @Accept json
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param operation path string true "Existing client operation ID"
// @Param request body media.UploadFailureInput true "Whitelisted failure metadata"
// @Success 200 {object} media.UploadFailure
// @Failure 400,401,403,409,413,429,500,503 {object} server.Problem
// @Router /bots/{bot_id}/media/upload-failures/{operation} [put].
func (h *MessageHandler) ReportMediaUploadFailure(c echo.Context) error {
	store, owner, bot, err := h.uploadScope(c)
	if err != nil {
		return err
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 2048)
	var input media.UploadFailureInput
	decoder := json.NewDecoder(c.Request().Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return apperror.FieldInvalid("failure", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return apperror.FieldInvalid("failure", err)
	}
	result, err := store.ReportFailure(c.Request().Context(), owner, bot, c.Param("operation"), input)
	if err != nil {
		return resumableError(err)
	}
	return c.JSON(http.StatusOK, result)
}

// ListMediaUploadFailures returns only the current identity's Bot-scoped client reports.
// @Summary Read scoped attachment failure reports
// @Tags messages
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Success 200 {array} media.UploadFailure
// @Failure 400,401,403,404,429,500,503 {object} server.Problem
// @Router /bots/{bot_id}/media/upload-failures [get].
func (h *MessageHandler) ListMediaUploadFailures(c echo.Context) error {
	return h.mediaUploadFailures(c, "")
}

// GetMediaUploadFailure reads an existing operation's report in the same owner/Bot scope.
// @Summary Read one scoped attachment failure report
// @Tags messages
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param operation path string true "Existing client operation ID"
// @Success 200 {array} media.UploadFailure
// @Failure 400,401,403,404,429,500,503 {object} server.Problem
// @Router /bots/{bot_id}/media/upload-failures/{operation} [get].
func (h *MessageHandler) GetMediaUploadFailure(c echo.Context) error {
	return h.mediaUploadFailures(c, c.Param("operation"))
}

func (h *MessageHandler) mediaUploadFailures(c echo.Context, operation string) error {
	store, owner, bot, err := h.uploadScope(c)
	if err != nil {
		return err
	}
	result, err := store.Failures(c.Request().Context(), owner, bot, operation)
	if err != nil {
		return resumableError(err)
	}
	return c.JSON(http.StatusOK, result)
}
