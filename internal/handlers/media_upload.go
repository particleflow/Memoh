package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/errs"
	"github.com/felinics/memoh/internal/httpx"
	"github.com/felinics/memoh/internal/media"
)

// MediaUploadLimitsResponse is the authoritative per-file Web upload policy.
type MediaUploadLimitsResponse struct {
	MaxUploadBytes int64 `json:"max_upload_bytes" validate:"required"`
}

// MediaUploadResponse contains a Bot-scoped reference, never a workspace path.
type MediaUploadResponse struct {
	ContentHash string `json:"content_hash" validate:"required"`
	Mime        string `json:"mime" validate:"required"`
	SizeBytes   int64  `json:"size_bytes" validate:"required"`
}

// MediaUploadLimits reports the configured attachment policy.
//
// @Summary Get the attachment upload limit
// @Tags messages
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Success 200 {object} MediaUploadLimitsResponse
// @Failure 401,403,404,500 {object} server.Problem
// @Router /bots/{bot_id}/media/limits [get]
func (h *MessageHandler) MediaUploadLimits(c echo.Context) error {
	if _, err := h.mediaUploadBot(c); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, MediaUploadLimitsResponse{MaxUploadBytes: h.mediaService.MaxUploadBytes()})
}

// UploadMedia persists an attachment without creating a session or model turn.
//
// @Summary Upload an attachment before sending a chat message
// @Description Streams one file into Bot-scoped storage. No session or model turn is created.
// @Tags messages
// @Accept multipart/form-data
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param file formData file true "Attachment"
// @Success 201 {object} MediaUploadResponse
// @Failure 400,401,403,404,413,500 {object} server.Problem
// @Router /bots/{bot_id}/media [post]
func (h *MessageHandler) UploadMedia(c echo.Context) error {
	botID, err := h.mediaUploadBot(c)
	if err != nil {
		return err
	}
	limit := h.mediaService.MaxUploadBytes()
	// Bound the envelope too; the asset itself has its own exact streaming
	// limit. Never call ParseMultipartForm (which buffers the request).
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, limit+64*1024)
	mr, err := c.Request().MultipartReader()
	if err != nil {
		return apperror.FieldInvalid("file", err)
	}
	part, err := mr.NextPart()
	if err != nil {
		return uploadMediaError(fmt.Errorf("%w: %w", errInvalidMediaUpload, err))
	}
	if part.FormName() != "file" || part.FileName() == "" {
		return apperror.FieldInvalid("file", nil)
	}
	contentType := part.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	contentType, _, err = mime.ParseMediaType(contentType)
	if err != nil {
		return apperror.FieldInvalid("file", err)
	}
	ext := strings.ToLower(path.Ext(part.FileName()))
	if ext == "." || strings.ContainsAny(ext, `\/`) {
		return apperror.FieldInvalid("file", nil)
	}
	reader := &singleUploadPart{part: part, multipart: mr, body: c.Request().Body}
	asset, err := h.mediaService.Ingest(c.Request().Context(), media.IngestInput{
		BotID: botID, Mime: contentType, OriginalExt: ext, Reader: reader, MaxBytes: limit,
	})
	if err != nil {
		return uploadMediaError(err)
	}
	return c.JSON(http.StatusCreated, MediaUploadResponse{
		ContentHash: asset.ContentHash, Mime: asset.Mime, SizeBytes: asset.SizeBytes,
	})
}

func (h *MessageHandler) mediaUploadBot(c echo.Context) (string, error) {
	identityID, err := h.requireChannelIdentityID(c)
	if err != nil {
		return "", err
	}
	botID, err := httpx.RequiredParam(c, "bot_id")
	if err != nil {
		return "", err
	}
	bot, err := h.authorizeBotAccess(c.Request().Context(), identityID, botID)
	if err != nil {
		return "", err
	}
	if h.mediaService == nil {
		return "", errs.Wrap(media.ErrProviderUnavailable, "upload media")
	}
	return bot.ID, nil
}

var errInvalidMediaUpload = errors.New("invalid attachment multipart body")

// Validate the closing boundary before Ingest can publish the file. A truncated
// body, second part, canceled read or overlong epilogue cannot leave a partial
// original behind. Ingest owns removal of its temporary spool on every failure.
type singleUploadPart struct {
	part      *multipart.Part
	multipart *multipart.Reader
	body      io.Reader
	done      bool
	size      int64
}

func (r *singleUploadPart) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n, err := r.part.Read(p)
	r.size += int64(n)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("%w: %w", errInvalidMediaUpload, err)
	}
	if errors.Is(err, io.EOF) {
		r.done = true
		if _, nextErr := r.multipart.NextPart(); nextErr == nil {
			return n, errInvalidMediaUpload
		} else if !errors.Is(nextErr, io.EOF) {
			return n, fmt.Errorf("%w: unexpected trailing part: %w", errInvalidMediaUpload, nextErr)
		}
		if _, err := io.Copy(io.Discard, r.body); err != nil {
			return n, err
		}
		if r.size == 0 {
			return n, errInvalidMediaUpload
		}
	}
	return n, err
}

func uploadMediaError(err error) error {
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, media.ErrAssetTooLarge), errors.As(err, &maxBytesErr):
		return apperror.Wrap(apperror.CodeHTTPPayloadTooLarge, err, nil)
	case errors.Is(err, errInvalidMediaUpload):
		return apperror.FieldInvalid("file", err)
	default:
		return errs.Wrap(err, "upload media")
	}
}
