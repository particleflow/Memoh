package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/audio/input"
)

type playbackInputService interface {
	PlaybackInputInfo(context.Context, string) (string, string, error)
	OpenPlaybackInput(context.Context, string) (input.Session, string, error)
}

type playbackInputRequest struct {
	ID string `json:"id"`
	input.Frame
}

type playbackInputSessions struct {
	sync.Mutex
	entries map[string]*playbackInputSession
}
type playbackInputSession struct {
	session           input.Session
	identity, modelID string
	cancel            context.CancelFunc
}

func inputRequest(c echo.Context) (playbackInputRequest, error) {
	var request playbackInputRequest
	if err := c.Bind(&request); err != nil {
		return request, err
	}
	id, err := uuid.Parse(request.ID)
	if err != nil || id == uuid.Nil {
		return request, echo.NewHTTPError(http.StatusBadRequest, "invalid playback input ID")
	}
	request.ID = id.String()
	if !utf8.ValidString(request.Text) || utf8.RuneCountInString(request.Text) > 20000 || (strings.TrimSpace(request.Text) == "" && !request.Final) {
		return request, echo.NewHTTPError(http.StatusBadRequest, "invalid playback input text")
	}
	return request, nil
}

func inputOwner(c echo.Context, botID, id string) (string, error) {
	actor, err := RequireChannelIdentityID(c)
	if err != nil {
		return "", err
	}
	return actor + ":" + botID + ":" + id, nil
}

// StartPlaybackInput godoc
// @Summary Start one configured native incremental speech task
// @Description Requires an active account JWT and Bot chat permission. Streams one task's audio until final input and provider completion. Maximum 20000 Unicode characters per frame, within the native protocol limit. Never retry an uncertain synthesis. No message or attachment is created.
// @Tags bots
// @Accept json
// @Produce octet-stream
// @Param bot_id path string true "Bot ID"
// @Param request body playbackInputRequest true "Initial exact spoken-text frame, sequence and Unicode offset zero"
// @Success 200 {file} file "Configured audio bytes"
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Router /bots/{bot_id}/tts/playback/input [post].
func (h *BotAudioHandler) StartPlaybackInput(c echo.Context) error {
	botID, err := h.authorizePlayback(c)
	if err != nil {
		return err
	}
	request, err := inputRequest(c)
	if err != nil {
		return err
	}
	if strings.TrimSpace(request.Text) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "initial speech text is required")
	}
	if request.Sequence != 0 || request.Offset != 0 {
		return echo.NewHTTPError(http.StatusConflict, "playback input must start at zero")
	}
	service, ok := h.audioService.(playbackInputService)
	if !ok {
		return echo.NewHTTPError(http.StatusConflict, "native input is unavailable")
	}
	settings, err := h.settingsService.GetBot(c.Request().Context(), botID)
	if err != nil {
		return err
	}
	if settings.TtsModelID == "" {
		return echo.NewHTTPError(http.StatusConflict, "speech model is not configured")
	}
	key, err := inputOwner(c, botID, request.ID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()
	active := &playbackInputSession{modelID: settings.TtsModelID, cancel: cancel}
	h.inputSessions.Lock()
	if h.inputSessions.entries == nil {
		h.inputSessions.entries = make(map[string]*playbackInputSession)
	}
	if h.inputSessions.entries[key] != nil {
		h.inputSessions.Unlock()
		return echo.NewHTTPError(http.StatusConflict, "playback input is already active")
	}
	h.inputSessions.entries[key] = active
	h.inputSessions.Unlock()
	defer func() { h.inputSessions.Lock(); delete(h.inputSessions.entries, key); h.inputSessions.Unlock() }()
	session, identity, err := service.OpenPlaybackInput(ctx, settings.TtsModelID)
	if errors.Is(err, input.ErrUnsupported) {
		return echo.NewHTTPError(http.StatusConflict, "native input is unavailable")
	}
	if err != nil {
		return err
	}
	defer session.Cancel()
	if err = session.Append(ctx, request.Frame); err != nil {
		return err
	}
	h.inputSessions.Lock()
	active.session = session
	active.identity = identity
	h.inputSessions.Unlock()
	stream := session.Audio()
	if !strings.HasPrefix(stream.ContentType, "audio/") {
		return echo.NewHTTPError(http.StatusBadGateway, "invalid playback content type")
	}
	c.Response().Header().Set("X-Speech-Playback-Identity", identity)
	c.Response().Header().Set(echo.HeaderContentType, stream.ContentType)
	c.Response().Header().Set(echo.HeaderCacheControl, "private, no-store")
	c.Response().Header().Set(echo.HeaderXContentTypeOptions, "nosniff")
	c.Response().Header().Set("X-Accel-Buffering", "no")
	// Publish readiness before audio: a native model may need further text before
	// producing its first frame. Waiting for audio here deadlocks duplex ingress.
	c.Response().WriteHeader(http.StatusOK)
	c.Response().Flush()
	for {
		select {
		case <-ctx.Done():
			// An internal config cancellation must not become clean HTTP EOF:
			// clients may otherwise cache truncated audio as a full reply.
			panic(http.ErrAbortHandler)
		case bytes, open := <-stream.Stream:
			if !open {
				if stream.Err() != nil {
					panic(http.ErrAbortHandler)
				}
				return nil
			}
			if len(bytes) == 0 {
				continue
			}
			if _, err = c.Response().Write(bytes); err != nil {
				return err
			}
			c.Response().Flush()
		}
	}
}

// AppendPlaybackInput godoc
// @Summary Append or finish the current native speech input
// @Description Revalidates the active account, Bot chat permission and current effective configuration. Exact next sequence and Unicode offset are required. Missing/closed input never creates another synthesis.
// @Tags bots
// @Accept json
// @Param bot_id path string true "Bot ID"
// @Param request body playbackInputRequest true "Next frame; final closes text input, not audio output"
// @Success 204
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Router /bots/{bot_id}/tts/playback/input [put].
func (h *BotAudioHandler) AppendPlaybackInput(c echo.Context) error {
	botID, err := h.authorizePlayback(c)
	if err != nil {
		return err
	}
	request, err := inputRequest(c)
	if err != nil {
		return err
	}
	key, err := inputOwner(c, botID, request.ID)
	if err != nil {
		return err
	}
	h.inputSessions.Lock()
	active := h.inputSessions.entries[key]
	var session input.Session
	var identity, modelID string
	var cancel context.CancelFunc
	if active != nil {
		session, identity, modelID, cancel = active.session, active.identity, active.modelID, active.cancel
	}
	h.inputSessions.Unlock()
	if active == nil {
		return echo.NewHTTPError(http.StatusNotFound, "playback input is unavailable")
	}
	if session == nil {
		return echo.NewHTTPError(http.StatusConflict, "playback input is not ready")
	}
	settings, err := h.settingsService.GetBot(c.Request().Context(), botID)
	if err != nil {
		cancel()
		return err
	}
	service, ok := h.audioService.(playbackInputService)
	if !ok {
		cancel()
		return echo.NewHTTPError(http.StatusConflict)
	}
	current, _, err := service.PlaybackInputInfo(c.Request().Context(), settings.TtsModelID)
	if err != nil || modelID != settings.TtsModelID || current != identity {
		cancel()
		return echo.NewHTTPError(http.StatusConflict, "playback configuration changed")
	}
	if err = session.Append(c.Request().Context(), request.Frame); err != nil {
		if errors.Is(err, context.Canceled) {
			cancel()
		}
		return echo.NewHTTPError(http.StatusConflict, "playback input rejected")
	}
	return c.NoContent(http.StatusNoContent)
}
