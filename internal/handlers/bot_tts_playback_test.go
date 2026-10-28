package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/settings"
)

type playbackSettings struct {
	model string
	calls int
}

func (s *playbackSettings) GetBot(_ context.Context, bot string) (settings.Settings, error) {
	s.calls++
	if bot != "bot" {
		return settings.Settings{}, errors.New("unexpected bot")
	}
	return settings.Settings{TtsModelID: s.model}, nil
}

type playbackAudio struct {
	calls       int
	model, text string
	config      map[string]any
	signal      context.Context
	err         error
}

func (*playbackAudio) StreamToFile(context.Context, string, string, io.Writer) (string, error) {
	panic("playback must not create temporary attachments")
}

func (a *playbackAudio) Synthesize(ctx context.Context, model, text string, config map[string]any) ([]byte, string, error) {
	a.calls++
	a.model = model
	a.text = text
	a.config = config
	a.signal = ctx
	return []byte("synthetic-audio"), "audio/wav", a.err
}

func TestBotPlaybackUsesConfiguredModelAndEphemeralAudio(t *testing.T) {
	t.Parallel()
	config := &playbackSettings{model: "existing-configured-model"}
	audio := &playbackAudio{}
	h := &BotAudioHandler{settingsService: config, audioService: audio, playbackAccess: func(_ context.Context, actor, bot string) error {
		if actor != "11111111-1111-1111-1111-111111111111" || bot != "bot" {
			t.Fatal("authorization target changed")
		}
		return nil
	}}
	c := fieldErrorContext(http.MethodPost, `{"text":"  你好🙂  "}`, "11111111-1111-1111-1111-111111111111", map[string]string{"bot_id": "bot"})
	if err := h.Playback(c); err != nil {
		t.Fatal(err)
	}
	rec := c.Response().Writer.(*httptest.ResponseRecorder)
	if rec.Code != 200 || rec.Body.String() != "synthetic-audio" || rec.Header().Get("Content-Type") != "audio/wav" || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("response: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
	if audio.calls != 1 || audio.model != config.model || audio.text != "你好🙂" || audio.config != nil || audio.signal != c.Request().Context() {
		t.Fatalf("synthesis contract changed: %+v", audio)
	}
}

func TestBotPlaybackFailsClosedBeforeSynthesis(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, user, text, model string
		deny                    bool
		status                  int
		code                    apperror.Code
	}{
		{name: "no identity", text: "hello", model: "model", status: 401},
		{name: "permission denied", user: "11111111-1111-1111-1111-111111111111", text: "hello", model: "model", deny: true, status: 403},
		{name: "empty", user: "11111111-1111-1111-1111-111111111111", text: " ", model: "model", code: apperror.CodeRequestFieldRequired},
		{name: "Unicode limit", user: "11111111-1111-1111-1111-111111111111", text: strings.Repeat("🙂", 501), model: "model", code: apperror.CodeTTSTextTooLong},
		{name: "missing model", user: "11111111-1111-1111-1111-111111111111", text: "hello", code: apperror.CodeTTSModelNotConfigured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			audio := &playbackAudio{}
			config := &playbackSettings{model: tc.model}
			h := &BotAudioHandler{settingsService: config, audioService: audio, playbackAccess: func(context.Context, string, string) error {
				if tc.deny {
					return echo.NewHTTPError(403)
				}
				return nil
			}}
			c := fieldErrorContext(http.MethodPost, `{"text":"`+tc.text+`"}`, tc.user, map[string]string{"bot_id": "bot"})
			err := h.Playback(c)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if tc.status != 0 {
				var httpErr *echo.HTTPError
				if !errors.As(err, &httpErr) || httpErr.Code != tc.status {
					t.Fatalf("error: %v", err)
				}
			} else if apperror.CodeOf(err) != tc.code {
				t.Fatalf("code: %v", err)
			}
			if audio.calls != 0 {
				t.Fatal("rejected request synthesized")
			}
			if (tc.status == 401 || tc.status == 403) && config.calls != 0 {
				t.Fatal("unauthorized request loaded settings")
			}
		})
	}
	c := fieldErrorContext(http.MethodPost, `{"text":"hello"}`, "11111111-1111-1111-1111-111111111111", map[string]string{"bot_id": "bot"})
	if err := (&BotAudioHandler{}).Playback(c); err == nil {
		t.Fatal("missing authorizer must fail closed")
	}
}
