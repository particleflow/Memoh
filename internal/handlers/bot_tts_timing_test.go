package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestPlaybackTimingOptInUsesOnlyOpaqueIDAndMonotonicDurations(t *testing.T) {
	c := fieldErrorContext(http.MethodPost, `{"text":"do-not-log-body"}`, "actor", nil)
	if newPlaybackTiming(c, nil) != nil {
		t.Fatal("diagnostics must be opt-in")
	}
	c.Request().Header.Set(echo.HeaderXRequestID, "speech-timing-secret/not-a-uuid")
	if newPlaybackTiming(c, nil) != nil {
		t.Fatal("arbitrary request identity must not enter diagnostic log")
	}
	c.Request().Header.Set(echo.HeaderXRequestID, "speech-timing-11111111-1111-4111-8111-111111111111")
	var out bytes.Buffer
	timing := newPlaybackTiming(c, slog.New(slog.NewJSONHandler(&out, nil)))
	timing.firstAudio(c)
	if !strings.HasPrefix(c.Response().Header().Get("Server-Timing"), "speech_first_audio;dur=") {
		t.Fatal("first-audio duration missing")
	}
	var row map[string]any
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["speech_trace_id"] != "11111111-1111-4111-8111-111111111111" || row["clock"] != "server-monotonic" || row["elapsed_ms"].(float64) < 0 {
		t.Fatal(row)
	}
	if strings.Contains(out.String(), "do-not-log-body") || strings.Contains(out.String(), "actor") || strings.Contains(out.String(), "http") {
		t.Fatal("private request content leaked")
	}
}
