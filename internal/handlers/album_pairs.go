package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/albumpairs"
	"github.com/felinics/memoh/internal/apperror"
)

// AlbumPairHandler serves the phone's pairing surface: which pairing label the
// user designated for a processing line and whether it is active. The album
// itself is resolved on the phone; the request body never carries an album uri
// or name.
//
// Pairing is written only with the user's own identity. There is no model tool
// for it: "the user designated this album" is a user statement, not something a
// model may make on their behalf.
type AlbumPairHandler struct {
	logger  *slog.Logger
	service *albumpairs.Service
}

func NewAlbumPairHandler(log *slog.Logger, service *albumpairs.Service) *AlbumPairHandler {
	if log == nil {
		log = slog.Default()
	}
	return &AlbumPairHandler{logger: log.With(slog.String("handler", "album_pair")), service: service}
}

func (h *AlbumPairHandler) Register(e *echo.Echo) {
	g := e.Group("/users/me/album-pairs")
	g.GET("", h.List)
	g.PUT("/:pair_key", h.Upsert)
	g.DELETE("/:pair_key", h.Revoke)
}

type AlbumPairResponse struct {
	Pairs []albumpairs.Pair `json:"pairs"`
}

// List godoc
// @Summary List the caller's album pairings
// @Description Every pairing this user designated, active and unbound, oldest first. A pairing records intent only: the pairing label plus the Bot whose finished products go there. The album uri and name stay on the phone.
// @Tags album-pairs
// @Produce json
// @Success 200 {object} AlbumPairResponse
// @Failure 400 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/album-pairs [get].
func (h *AlbumPairHandler) List(c echo.Context) error {
	if h == nil || h.service == nil {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	pairs, err := h.service.List(c.Request().Context(), userID)
	if err != nil {
		return albumPairHTTPError(err)
	}
	return c.JSON(http.StatusOK, AlbumPairResponse{Pairs: pairs})
}

// Upsert godoc
// @Summary Designate (or re-designate) an album pairing
// @Description Activates the pairing label for the given Bot. Reusing a label for another Bot, or after an unbind, updates the existing pairing instead of creating a second one. Activating a label also retires that Bot's previous active pairing, because one processing line writes into one album.
// @Tags album-pairs
// @Accept json
// @Produce json
// @Param pair_key path string true "Pairing label the phone uses for this album"
// @Param request body albumPairUpsertRequest true "Target Bot"
// @Success 200 {object} albumpairs.Pair
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/album-pairs/{pair_key} [put].
func (h *AlbumPairHandler) Upsert(c echo.Context) error {
	if h == nil || h.service == nil {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var req albumPairUpsertRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 4096))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&req); err != nil {
		return apperror.FieldInvalid("request", err)
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return apperror.FieldInvalid("request", err)
	}
	pair, err := h.service.Upsert(c.Request().Context(), userID, c.Param("pair_key"), req.BotID)
	if err != nil {
		return albumPairHTTPError(err)
	}
	return c.JSON(http.StatusOK, pair)
}

// Revoke godoc
// @Summary Unbind an album pairing
// @Description Turns automatic queueing off for this label. The pairing row and its label stay, so naming the same album again reuses it, and already queued write-backs stay queued until the phone reports them written.
// @Tags album-pairs
// @Produce json
// @Param pair_key path string true "Pairing label to unbind"
// @Success 200 {object} albumpairs.Pair
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/album-pairs/{pair_key} [delete].
func (h *AlbumPairHandler) Revoke(c echo.Context) error {
	if h == nil || h.service == nil {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	pair, err := h.service.Revoke(c.Request().Context(), userID, c.Param("pair_key"))
	if err != nil {
		return albumPairHTTPError(err)
	}
	return c.JSON(http.StatusOK, pair)
}

type albumPairUpsertRequest struct {
	BotID string `json:"bot_id"`
}

func albumPairHTTPError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, albumpairs.ErrInvalid):
		return apperror.FieldInvalid("request", err)
	case errors.Is(err, albumpairs.ErrNotFound):
		return apperror.Wrap(apperror.CodeHTTPNotFound, err, nil)
	case errors.Is(err, albumpairs.ErrBotUnavailable):
		return apperror.Wrap(apperror.CodeBotNotFound, err, nil)
	default:
		return err
	}
}
