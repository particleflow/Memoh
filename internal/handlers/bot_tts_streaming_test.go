package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/felinics/twilight/sdk"
)

type streamingPlaybackAudio struct {
	*playbackAudio
	chunks   <-chan []byte
	failures <-chan error
	started  chan context.Context
}

func (s *streamingPlaybackAudio) StreamPlayback(ctx context.Context, _, _ string) (*sdk.SpeechStreamResult, error) {
	s.started <- ctx
	return sdk.NewSpeechStreamResult(s.chunks, "audio/mpeg", s.failures), nil
}

type playbackFlushRecorder struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
}

func (w *playbackFlushRecorder) Flush() {
	w.ResponseRecorder.Flush()
	select {
	case w.flushed <- struct{}{}:
	default:
	}
}

// No network listener or provider: controlled channels prove the handler does
// not wait for the utterance to finish before forwarding its first bytes.
func TestBotPlaybackStreamingFirstChunkBeforeEOFAndCancellation(t *testing.T) {
	chunks := make(chan []byte)
	started := make(chan context.Context, 1)
	service := &streamingPlaybackAudio{playbackAudio: &playbackAudio{}, chunks: chunks, started: started}
	h := &BotAudioHandler{audioService: service, settingsService: &playbackSettings{model: "existing-cloned-voice"}, playbackAccess: func(context.Context, string, string) error { return nil }}
	c := fieldErrorContext(http.MethodPost, `{"text":"visible label"}`, "11111111-1111-1111-1111-111111111111", map[string]string{"bot_id": "bot"})
	requestCtx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()
	c.SetRequest(c.Request().WithContext(requestCtx))
	writer := &playbackFlushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{}, 1)}
	c.Response().Writer = writer
	done := make(chan error, 1)
	go func() { done <- h.Playback(c) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	chunks <- []byte("first-encoded-audio-frames")
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("first audio held until EOF")
	}
	if writer.Body.String() != "first-encoded-audio-frames" || writer.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatal("first chunk was not forwarded")
	}
	select {
	case <-done:
		t.Fatal("stream ended before provider EOF")
	default:
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not release handler")
	}
}

func TestBotPlaybackStreamingFailureAfterBytesAbortsHTTP(t *testing.T) {
	chunks, failures := make(chan []byte, 1), make(chan error, 1)
	chunks <- []byte("partial")
	close(chunks)
	failures <- errors.New("synthetic upstream truncation")
	close(failures)
	service := &streamingPlaybackAudio{playbackAudio: &playbackAudio{}, chunks: chunks, failures: failures, started: make(chan context.Context, 1)}
	h := &BotAudioHandler{audioService: service, settingsService: &playbackSettings{model: "configured"}, playbackAccess: func(context.Context, string, string) error { return nil }}
	c := fieldErrorContext(http.MethodPost, `{"text":"visible"}`, "11111111-1111-1111-1111-111111111111", map[string]string{"bot_id": "bot"})
	defer func() {
		value := recover()
		err, ok := value.(error)
		if !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("truncation must abort committed HTTP, got %v", value)
		}
	}()
	_ = h.Playback(c)
	t.Fatal("truncated stream returned a clean EOF")
}
