package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/felinics/twilight/sdk"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

type timingPlaybackAudio struct {
	*playbackAudio
	mp3, wav []byte
}

func (a *timingPlaybackAudio) StreamPlayback(ctx context.Context, _, text string) (*sdk.SpeechStreamResult, error) {
	data, contentType, first, eof := a.mp3, "audio/mpeg", 70*time.Millisecond, 450*time.Millisecond
	switch text {
	case "slow":
		first = 620 * time.Millisecond
		eof = 900 * time.Millisecond
	case "wav":
		data = a.wav
		contentType = "audio/wav"
		first = 40 * time.Millisecond
		eof = 780 * time.Millisecond
	case "fast", "auto":
	default:
		panic("unexpected timing fixture profile")
	}
	chunks := make(chan []byte)
	failures := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(failures)
		wait := func(delay time.Duration) bool {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
				return true
			case <-ctx.Done():
				failures <- ctx.Err()
				return false
			}
		}
		if !wait(first) {
			return
		}
		select {
		case chunks <- data[:8403]:
		case <-ctx.Done():
			return
		}
		if !wait(eof - first) {
			return
		}
		select {
		case chunks <- data[8403:]:
		case <-ctx.Done():
			return
		}
	}()
	return sdk.NewSpeechStreamResult(chunks, contentType, failures), nil
}

// Only the new timing seam: existing MP3 adapter proof is reused. Controlled
// provider-channel delays are deliberate, not measurements of a cloud model.
func TestPlaybackTimingControlledBrowser(t *testing.T) {
	if os.Getenv("MEMOH_SPEECH_TIMING_BROWSER") != "1" {
		t.Skip("opt-in diagnostic timing with local controlled audio")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	mp3, err := os.ReadFile(filepath.Join(root, "internal/handlers/testdata/read-aloud-wss-seam.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	wav, err := os.ReadFile(filepath.Join(root, "docs/testing/chat-read-aloud/streaming/wav-compat/fixture.wav"))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	h := &BotAudioHandler{audioService: &timingPlaybackAudio{playbackAudio: &playbackAudio{}, mp3: mp3, wav: wav}, settingsService: &playbackSettings{model: "synthetic-only"}, logger: slog.New(slog.NewJSONHandler(&logs, nil)), playbackAccess: func(context.Context, string, string) error { return nil }}
	e := echo.New()
	e.POST("/bots/:bot_id/tts/playback", func(c echo.Context) error {
		c.Set("user", &jwt.Token{Valid: true, Claims: jwt.MapClaims{
			"sub":     "11111111-1111-1111-1111-111111111111",
			"user_id": "11111111-1111-1111-1111-111111111111",
		}})
		if err := h.Playback(c); err != nil {
			t.Errorf("timing fixture playback: %v", err)
			return err
		}
		return nil
	})
	mux := http.NewServeMux()
	mux.Handle("/bots/bot/tts/playback", e)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<!doctype html><div id="root"></div>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	// Keep reports inside the test-owned directory, rather than trusting an
	// environment-provided write path. The runner logs its timing summaries.
	out := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "docs/testing/chat-read-aloud/streaming/timing/native-timing.mjs")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TIMING_URL="+server.URL, "TIMING_OUT="+out)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("timing runner: %v", err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		if err = json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["event"] == "adapter_first_audio" {
			count++
		}
	}
	if count != 4 {
		t.Fatalf("expected four correlated server observations, got %d", count)
	}
	if err = os.WriteFile(filepath.Join(out, "server-events.jsonl"), logs.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
