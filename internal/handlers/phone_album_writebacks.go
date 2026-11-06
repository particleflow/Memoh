package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/albumwriteback"
	"github.com/felinics/memoh/internal/apperror"
)

// PhoneAlbumWritebackHandler serves the phone side of the album write-back
// queue: the pending work list the app drains when it comes forward, and the
// per-item "written" report. Enqueueing is not exposed here; the bot enqueues
// through its own tool.
type PhoneAlbumWritebackHandler struct {
	logger  *slog.Logger
	service *albumwriteback.Service
}

func NewPhoneAlbumWritebackHandler(log *slog.Logger, service *albumwriteback.Service) *PhoneAlbumWritebackHandler {
	if log == nil {
		log = slog.Default()
	}
	return &PhoneAlbumWritebackHandler{
		logger:  log.With(slog.String("handler", "phone_album_writeback")),
		service: service,
	}
}

func (h *PhoneAlbumWritebackHandler) Register(e *echo.Echo) {
	g := e.Group("/users/me/album-writebacks")
	g.GET("", h.ListPending)
	g.POST("/written", h.MarkWritten)
}

type phoneAlbumWritebackWrittenRequest struct {
	PairKey     string `json:"pair_key"`
	ContentHash string `json:"content_hash"`
}

// ListPending godoc
// @Summary List the caller's pending phone album write-backs
// @Description Returns queued items in enqueue order: the identity is the existing media content hash, plus the source bot and the size/mime/name needed to create the asset. A pending item stays until the phone reports it written; there is no expiry.
// @Tags album-writebacks
// @Produce json
// @Param state query string false "Only `pending` is defined"
// @Success 200 {object} albumwriteback.PendingResponse
// @Failure 400 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/album-writebacks [get].
func (h *PhoneAlbumWritebackHandler) ListPending(c echo.Context) error {
	if h == nil || h.service == nil {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	if state := strings.TrimSpace(c.QueryParam("state")); state != "" && state != albumwriteback.StatePending {
		return apperror.FieldInvalid("state", errors.New("only pending is defined"))
	}
	out, err := h.service.Pending(c.Request().Context(), userID)
	if err != nil {
		return albumWritebackHTTPError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// MarkWritten godoc
// @Summary Report one phone album write-back as written
// @Description Idempotent per (pairing, content hash): a repeat keeps the first written timestamp. A pairing/hash the caller has no queued row for is not found. Write failures are not reported here; they stay pending for the next pull.
// @Tags album-writebacks
// @Accept json
// @Produce json
// @Param request body phoneAlbumWritebackWrittenRequest true "Written report"
// @Success 200 {object} albumwriteback.Item
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/album-writebacks/written [post].
func (h *PhoneAlbumWritebackHandler) MarkWritten(c echo.Context) error {
	if h == nil || h.service == nil {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var req phoneAlbumWritebackWrittenRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 4096))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&req); err != nil {
		return apperror.FieldInvalid("request", err)
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return apperror.FieldInvalid("request", err)
	}
	out, err := h.service.MarkWritten(c.Request().Context(), userID, req.PairKey, req.ContentHash)
	if err != nil {
		return albumWritebackHTTPError(err)
	}
	return c.JSON(http.StatusOK, out)
}

func albumWritebackHTTPError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, albumwriteback.ErrInvalid):
		return apperror.FieldInvalid("request", err)
	case errors.Is(err, albumwriteback.ErrNotFound):
		return apperror.Wrap(apperror.CodeHTTPNotFound, err, nil)
	default:
		return err
	}
}
