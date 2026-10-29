package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/felinics/twilight/sdk"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/apperror"
	audiopkg "github.com/felinics/memoh/internal/audio"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/errs"
	"github.com/felinics/memoh/internal/httpx"
	"github.com/felinics/memoh/internal/settings"
)

type botSpeechService interface {
	StreamToFile(context.Context, string, string, io.Writer) (string, error)
	Synthesize(context.Context, string, string, map[string]any) ([]byte, string, error)
	StreamPlayback(context.Context, string, string) (*sdk.SpeechStreamResult, error)
}
type botSpeechSettings interface {
	GetBot(context.Context, string) (settings.Settings, error)
}

// BotAudioHandler handles per-bot speech synthesis requests from the agent tool.
type BotAudioHandler struct {
	audioService    botSpeechService
	settingsService botSpeechSettings
	playbackAccess  func(context.Context, string, string) error
	tempStore       *audiopkg.TempStore
	logger          *slog.Logger
}

func NewBotAudioHandler(log *slog.Logger, audioService *audiopkg.Service, settingsService *settings.Service, tempStore *audiopkg.TempStore) *BotAudioHandler {
	return &BotAudioHandler{
		audioService:    audioService,
		settingsService: settingsService,
		tempStore:       tempStore,
		logger:          log.With(slog.String("handler", "bot_audio")),
	}
}

func (h *BotAudioHandler) Register(e *echo.Echo) {
	e.POST("/bots/:bot_id/tts/synthesize", h.Synthesize)
	e.POST("/bots/:bot_id/tts/playback", h.Playback)
}

type synthesizeRequest struct {
	Text string `json:"text"`
}

type synthesizeResponse struct {
	TempID      string `json:"temp_id"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// Synthesize godoc
// @Summary Synthesize speech for a bot
// @Description Stream-synthesize text using the bot's configured TTS model, write to temp file
// @Tags bots
// @Accept json
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param request body synthesizeRequest true "Text to synthesize"
// @Success 200 {object} synthesizeResponse
// @Failure 400 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Router /bots/{bot_id}/tts/synthesize [post].
func (h *BotAudioHandler) Synthesize(c echo.Context) error {
	botID, err := httpx.RequiredParam(c, "bot_id")
	if err != nil {
		return err
	}

	var req synthesizeRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return apperror.FieldRequired("text")
	}
	const maxTextLen = 500
	if len([]rune(text)) > maxTextLen {
		return apperror.New(apperror.CodeTTSTextTooLong, map[string]string{"max": strconv.Itoa(maxTextLen)})
	}

	botSettings, err := h.settingsService.GetBot(c.Request().Context(), botID)
	if err != nil {
		return errs.Wrap(err, "load bot settings", slog.String("bot_id", botID))
	}
	if botSettings.TtsModelID == "" {
		return apperror.New(apperror.CodeTTSModelNotConfigured, nil)
	}

	tempID, f, err := h.tempStore.Create()
	if err != nil {
		return errs.Wrap(err, "create temp file")
	}

	contentType, streamErr := h.audioService.StreamToFile(c.Request().Context(), botSettings.TtsModelID, text, f)
	closeErr := f.Close()
	if streamErr != nil {
		h.tempStore.Delete(tempID)
		return errs.Wrap(streamErr, "synthesize speech", slog.String("bot_id", botID), slog.String("model_id", botSettings.TtsModelID))
	}
	if closeErr != nil {
		h.tempStore.Delete(tempID)
		return errs.Wrap(closeErr, "finalize audio file", slog.String("bot_id", botID))
	}

	size, _ := h.tempStore.FileSize(tempID)

	return c.JSON(http.StatusOK, synthesizeResponse{
		TempID:      tempID,
		ContentType: contentType,
		Size:        size,
	})
}

// SetPlaybackAccess uses the same Bot chat permission as local conversation access.
// A missing authorizer fails closed; the legacy synthesize route is unchanged.
func (h *BotAudioHandler) SetPlaybackAccess(botService *bots.Service, accountService *accounts.Service) {
	h.playbackAccess = func(ctx context.Context, actorID, botID string) error {
		_, err := AuthorizeBotAccessWithPermission(ctx, botService, accountService, actorID, botID, bots.PermissionChat)
		return err
	}
}

// Playback godoc
// @Summary Read text aloud using a bot's configured voice
// @Description Ephemeral playback only; does not create a message, attachment or history record. Uses the configured TTS model and voice. Maximum 500 Unicode characters per request. Requires Bot chat permission.
// @Tags bots
// @Accept json
// @Produce octet-stream
// @Param bot_id path string true "Bot ID"
// @Param request body synthesizeRequest true "Text to read aloud"
// @Success 200 {file} file "Audio bytes; Content-Type is the configured model's audio format"
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Router /bots/{bot_id}/tts/playback [post].
func (h *BotAudioHandler) Playback(c echo.Context) error {
	timing := newPlaybackTiming(c, h.logger)
	timing.mark("handler_start")
	// Playback uses configured Bot credentials and requires an account JWT.
	// Chat-route JWTs skip account session validation in the JWT middleware;
	// reject them before Bot ownership or grants can authorize synthesis.
	if _, err := auth.ChatTokenFromContext(c); err == nil {
		return apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	botID, err := httpx.RequiredParam(c, "bot_id")
	if err != nil {
		return err
	}
	actorID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	if h.playbackAccess == nil {
		return echo.NewHTTPError(http.StatusForbidden)
	}
	if err := h.playbackAccess(c.Request().Context(), actorID, botID); err != nil {
		return err
	}
	var req synthesizeRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return apperror.FieldRequired("text")
	}
	if len([]rune(text)) > 500 {
		return apperror.New(apperror.CodeTTSTextTooLong, map[string]string{"max": "500"})
	}
	botSettings, err := h.settingsService.GetBot(c.Request().Context(), botID)
	if err != nil {
		return errs.Wrap(err, "load bot playback settings")
	}
	if botSettings.TtsModelID == "" {
		return apperror.New(apperror.CodeTTSModelNotConfigured, nil)
	}
	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel() // Close the upstream body on client cancellation or write failure.
	timing.mark("adapter_request")
	stream, err := h.audioService.StreamPlayback(ctx, botSettings.TtsModelID, text)
	if err != nil {
		return errs.Wrap(err, "synthesize bot playback")
	}
	if !strings.HasPrefix(stream.ContentType, "audio/") {
		return echo.NewHTTPError(http.StatusBadGateway, "invalid playback content type")
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "private, no-store")
	c.Response().Header().Set(echo.HeaderXContentTypeOptions, "nosniff")
	c.Response().Header().Set(echo.HeaderContentType, stream.ContentType)
	c.Response().Header().Set("X-Accel-Buffering", "no")
	firstAudio := true
	for {
		select {
		case <-ctx.Done():
			timing.mark("canceled")
			return ctx.Err()
		case chunk, ok := <-stream.Stream:
			if !ok {
				if err := stream.Err(); err != nil {
					if c.Response().Committed {
						// A clean HTTP EOF would falsely accept a truncated utterance.
						// The server recovery middleware preserves ErrAbortHandler.
						panic(http.ErrAbortHandler)
					}
					return errs.Wrap(err, "stream bot playback")
				}
				if !c.Response().Committed {
					return echo.NewHTTPError(http.StatusBadGateway, "empty playback stream")
				}
				timing.mark("upstream_eof")
				return nil
			}
			if len(chunk) == 0 {
				continue
			}
			if firstAudio {
				timing.firstAudio(c)
			}
			if _, err := c.Response().Write(chunk); err != nil {
				return err
			}
			c.Response().Flush()
			if firstAudio {
				timing.mark("http_first_flush")
				firstAudio = false
			}
		}
	}
}
