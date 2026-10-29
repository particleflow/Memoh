package handlers

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Only an explicit diagnostic request enables these content-free observations.
// Server durations never share an epoch with the browser performance clock.
type playbackTiming struct {
	start  time.Time
	id     string
	logger *slog.Logger
}

func newPlaybackTiming(c echo.Context, log *slog.Logger) *playbackTiming {
	id, ok := strings.CutPrefix(c.Request().Header.Get(echo.HeaderXRequestID), "speech-timing-")
	if !ok {
		return nil
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return nil
	}
	return &playbackTiming{start: time.Now(), id: id, logger: log}
}
func (t *playbackTiming) mark(event string) float64 {
	if t == nil {
		return 0
	}
	ms := float64(time.Since(t.start)) / float64(time.Millisecond)
	if t.logger != nil {
		t.logger.Info("speech_timing", slog.String("speech_trace_id", t.id), slog.String("event", event), slog.Float64("elapsed_ms", ms), slog.String("clock", "server-monotonic"))
	}
	return ms
}
func (t *playbackTiming) firstAudio(c echo.Context) {
	if t == nil {
		return
	}
	ms := t.mark("adapter_first_audio")
	c.Response().Header().Add("Server-Timing", fmt.Sprintf("speech_first_audio;dur=%.3f", ms))
	c.Response().Header().Add("Access-Control-Expose-Headers", "Server-Timing")
}
