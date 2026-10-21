package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/media"
)

func (h *MessageHandler) uploadScope(c echo.Context) (*media.UploadStore, string, string, error) {
	c.Response().Header().Set("Cache-Control", "no-store")
	bot, err := h.mediaUploadBot(c)
	if err != nil {
		return nil, "", "", err
	}
	owner, err := h.requireChannelIdentityID(c)
	if err != nil {
		return nil, "", "", err
	}
	store := h.mediaService.Uploads()
	if store == nil {
		return nil, "", "", apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	return store, owner, bot, nil
}

func resumableError(err error) error {
	switch {
	case errors.Is(err, media.ErrUploadNotFound):
		return apperror.Wrap(apperror.CodeHTTPNotFound, err, nil)
	case errors.Is(err, media.ErrUploadConflict):
		return apperror.Wrap(apperror.CodeHTTPConflict, err, nil)
	case errors.Is(err, media.ErrUploadInvalid):
		return apperror.FieldInvalid("upload", err)
	case errors.Is(err, media.ErrUploadQuota), errors.Is(err, media.ErrUploadBusy):
		return apperror.Wrap(apperror.CodeHTTPTooManyRequests, err, nil)
	default:
		return uploadMediaError(err)
	}
}

// CreateMediaUpload starts an identity- and Bot-scoped resumable upload.
// @Summary Create a resumable attachment upload
// @Tags messages
// @Accept json
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param request body media.UploadInput true "Expected size, SHA-256 and MIME"
// @Success 201 {object} media.Upload
// @Failure 400,401,403,404,413,429,500,503 {object} server.Problem
// @Router /bots/{bot_id}/media/uploads [post].
func (h *MessageHandler) CreateMediaUpload(c echo.Context) error {
	store, owner, bot, err := h.uploadScope(c)
	if err != nil {
		return err
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 4096)
	var in media.UploadInput
	if err := c.Bind(&in); err != nil {
		return err
	}
	result, err := store.Create(c.Request().Context(), owner, bot, in)
	if err != nil {
		return resumableError(err)
	}
	return c.JSON(http.StatusCreated, result)
}

// GetMediaUpload reports only durably acknowledged bytes.
// @Summary Get resumable upload status
// @Tags messages
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param upload_id path string true "Upload ID"
// @Success 200 {object} media.Upload
// @Failure 401,403,404,429,500,503 {object} server.Problem
// @Router /bots/{bot_id}/media/uploads/{upload_id} [get].
func (h *MessageHandler) GetMediaUpload(c echo.Context) error {
	store, owner, bot, err := h.uploadScope(c)
	if err != nil {
		return err
	}
	result, err := store.Get(c.Request().Context(), owner, bot, c.Param("upload_id"))
	if err != nil {
		return resumableError(err)
	}
	return c.JSON(http.StatusOK, result)
}

// AppendMediaUpload streams an exact bounded chunk. Retry after querying status.
// @Summary Append a verified upload chunk
// @Tags messages
// @Accept application/octet-stream
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param upload_id path string true "Upload ID"
// @Param Upload-Offset header integer true "Acknowledged byte offset"
// @Param Upload-Length header integer true "Exact chunk byte count, at most 8 MiB"
// @Param Upload-SHA256 header string true "Lowercase chunk SHA-256"
// @Param chunk body string true "Raw bytes" Format(binary)
// @Success 200 {object} media.Upload
// @Failure 400,401,403,404,409,413,429,500,503 {object} server.Problem
// @Router /bots/{bot_id}/media/uploads/{upload_id} [patch].
func (h *MessageHandler) AppendMediaUpload(c echo.Context) error {
	store, owner, bot, err := h.uploadScope(c)
	if err != nil {
		return err
	}
	offset, err := strconv.ParseInt(c.Request().Header.Get("Upload-Offset"), 10, 64)
	if err != nil {
		return apperror.FieldInvalid("Upload-Offset", err)
	}
	size, err := strconv.ParseInt(c.Request().Header.Get("Upload-Length"), 10, 64)
	if err != nil {
		return apperror.FieldInvalid("Upload-Length", err)
	}
	// Bound lock occupancy even if a client stops sending midway through a body.
	controller := http.NewResponseController(c.Response().Writer)
	if err := controller.SetReadDeadline(time.Now().Add(2 * time.Minute)); err == nil {
		defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, media.UploadChunkBytes+1)
	result, err := store.Append(c.Request().Context(), owner, bot, c.Param("upload_id"), offset, size, c.Request().Header.Get("Upload-SHA256"), c.Request().Body)
	if err != nil {
		return resumableError(err)
	}
	return c.JSON(http.StatusOK, result)
}

// CompleteMediaUpload verifies the final SHA and publishes through media.Service.
// @Summary Complete a resumable upload idempotently
// @Tags messages
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param upload_id path string true "Upload ID"
// @Success 200 {object} media.Upload
// @Failure 400,401,403,404,409,429,500,503 {object} server.Problem
// @Router /bots/{bot_id}/media/uploads/{upload_id}/complete [post].
func (h *MessageHandler) CompleteMediaUpload(c echo.Context) error {
	store, owner, bot, err := h.uploadScope(c)
	if err != nil {
		return err
	}
	result, err := store.Complete(c.Request().Context(), owner, bot, c.Param("upload_id"))
	if err != nil {
		return resumableError(err)
	}
	return c.JSON(http.StatusOK, result)
}

// CancelMediaUpload cancels staging only; ready originals are never removed.
// @Summary Cancel a resumable upload
// @Tags messages
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param upload_id path string true "Upload ID"
// @Success 200 {object} media.Upload
// @Failure 401,403,404,409,429,500,503 {object} server.Problem
// @Router /bots/{bot_id}/media/uploads/{upload_id} [delete].
func (h *MessageHandler) CancelMediaUpload(c echo.Context) error {
	store, owner, bot, err := h.uploadScope(c)
	if err != nil {
		return err
	}
	result, err := store.Cancel(c.Request().Context(), owner, bot, c.Param("upload_id"))
	if err != nil {
		return resumableError(err)
	}
	return c.JSON(http.StatusOK, result)
}
