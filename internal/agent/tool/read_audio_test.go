package tools

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/felinics/twilight/sdk"
)

// stubReadTranscriber records the calls the read audio branch makes against the
// narrow transcriber interface and returns canned responses.
type stubReadTranscriber struct {
	modelID        string
	transcribeErr  error
	gotModelID     string
	gotAudio       []byte
	gotFilename    string
	gotContentType string
}

func (s *stubReadTranscriber) TranscriptionModelID(context.Context, string) string {
	return s.modelID
}

func (s *stubReadTranscriber) Transcribe(_ context.Context, modelID string, audio []byte, filename string, contentType string) (*sdk.TranscriptionResult, error) {
	s.gotModelID = modelID
	s.gotAudio = audio
	s.gotFilename = filename
	s.gotContentType = contentType
	if s.transcribeErr != nil {
		return nil, s.transcribeErr
	}
	return &sdk.TranscriptionResult{Text: "hello from the stub", Language: "zh", DurationSeconds: 12.5}, nil
}

func newReadAudioProvider(t *testing.T, files map[string][]byte, transcriber ReadTranscriber) *ContainerProvider {
	t.Helper()
	client := newReadMediaTestClient(t, files)
	provider := NewContainerProvider(nil, containerTestBridgeProvider{client: client}, nil, "")
	provider.SetTranscriber(transcriber)
	return provider
}

func TestReadAudioTranscribesViaExistingService(t *testing.T) {
	t.Parallel()

	mp3Bytes := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, 64)...)
	transcriber := &stubReadTranscriber{modelID: "qwen3-asr-flash"}
	provider := newReadAudioProvider(t, map[string][]byte{
		"media/voice.mp3": mp3Bytes,
	}, transcriber)

	result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/media/voice.mp3"})
	if err != nil {
		t.Fatalf("execRead() error = %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if got := m["ok"]; got != true {
		t.Fatalf("expected ok=true, got %v", got)
	}
	if got := m["content"]; got != "hello from the stub" {
		t.Fatalf("expected transcription text, got %v", got)
	}
	if got := m["mime"]; got != "audio/mpeg" {
		t.Fatalf("expected audio/mpeg, got %v", got)
	}
	if got := m["language"]; got != "zh" {
		t.Fatalf("expected language zh, got %v", got)
	}
	if got := m["duration_seconds"]; got != 12.5 {
		t.Fatalf("expected duration 12.5, got %v", got)
	}
	if got, _ := m["size"].(int); got != len(mp3Bytes) {
		t.Fatalf("expected size %d, got %v", len(mp3Bytes), got)
	}
	if transcriber.gotModelID != "qwen3-asr-flash" {
		t.Fatalf("expected model id from bot settings, got %q", transcriber.gotModelID)
	}
	if string(transcriber.gotAudio) != string(mp3Bytes) {
		t.Fatal("expected the audio bytes to reach the transcriber")
	}
	if transcriber.gotFilename != "voice.mp3" {
		t.Fatalf("expected filename voice.mp3, got %q", transcriber.gotFilename)
	}
	if transcriber.gotContentType != "audio/mpeg" {
		t.Fatalf("expected content type audio/mpeg, got %q", transcriber.gotContentType)
	}
}

func TestReadAudioUnconfiguredBotReturnsMachineError(t *testing.T) {
	t.Parallel()

	provider := newReadAudioProvider(t, map[string][]byte{
		"media/voice.mp3": append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, 64)...),
	}, &stubReadTranscriber{modelID: ""})

	_, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/media/voice.mp3"})
	if err == nil {
		t.Fatal("expected error for bot without transcription model")
	}
	if !strings.Contains(err.Error(), "audio transcription is not configured for this bot") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadAudioExceedingLimitReturnsMachineError(t *testing.T) {
	t.Parallel()

	// The DashScope limit is 10 MB after base64 encoding; with the data URL
	// prefix the raw budget is (10_000_000 - prefix) / 4 * 3 bytes.
	prefix := len("data:audio/mpeg;base64,")
	maxRaw := (10_000_000 - prefix) / 4 * 3
	oversized := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, maxRaw)...)

	provider := newReadAudioProvider(t, map[string][]byte{
		"media/long.mp3": oversized,
	}, &stubReadTranscriber{modelID: "qwen3-asr-flash"})

	_, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/media/long.mp3"})
	if err == nil {
		t.Fatal("expected error for oversized audio")
	}
	if !strings.Contains(err.Error(), "audio exceeds transcription limits (10 MB encoded, 5 minutes)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadAudioSurfacesTranscriberFailure(t *testing.T) {
	t.Parallel()

	provider := newReadAudioProvider(t, map[string][]byte{
		"media/voice.mp3": append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, 64)...),
	}, &stubReadTranscriber{modelID: "qwen3-asr-flash", transcribeErr: context.DeadlineExceeded})

	_, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/media/voice.mp3"})
	if err == nil {
		t.Fatal("expected upstream transcription error to surface")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadAudioSniffRouting(t *testing.T) {
	t.Parallel()

	t.Run("id3 header routes to transcription", func(t *testing.T) {
		t.Parallel()
		transcriber := &stubReadTranscriber{modelID: "qwen3-asr-flash"}
		provider := newReadAudioProvider(t, map[string][]byte{
			"media/voice.mp3": append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, 64)...),
		}, transcriber)
		if _, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/media/voice.mp3"}); err != nil {
			t.Fatalf("execRead() error = %v", err)
		}
		if transcriber.gotAudio == nil {
			t.Fatal("expected the audio branch to call the transcriber")
		}
	})

	t.Run("binary m4a with audio extension routes to transcription", func(t *testing.T) {
		t.Parallel()
		transcriber := &stubReadTranscriber{modelID: "qwen3-asr-flash"}
		provider := newReadAudioProvider(t, map[string][]byte{
			"media/clip.m4a": append([]byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}, make([]byte, 64)...),
		}, transcriber)
		if _, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/media/clip.m4a"}); err != nil {
			t.Fatalf("execRead() error = %v", err)
		}
		if transcriber.gotAudio == nil {
			t.Fatal("expected the audio branch to call the transcriber")
		}
		if transcriber.gotContentType != "audio/mp4" {
			t.Fatalf("expected audio/mp4, got %q", transcriber.gotContentType)
		}
	})

	t.Run("text file with audio extension still reads as text", func(t *testing.T) {
		t.Parallel()
		transcriber := &stubReadTranscriber{modelID: "qwen3-asr-flash"}
		provider := newReadAudioProvider(t, map[string][]byte{
			"notes/lyrics.mp3": []byte("just plain lyrics, no binary bytes"),
		}, transcriber)
		result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/notes/lyrics.mp3"})
		if err != nil {
			t.Fatalf("execRead() error = %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map result, got %T", result)
		}
		if _, exists := m["content"]; !exists {
			t.Fatalf("expected text content result, got %v", m)
		}
		if transcriber.gotAudio != nil {
			t.Fatal("plain text must not reach the transcriber")
		}
	})
}

func TestReadDescriptionMentionsAudioTranscription(t *testing.T) {
	t.Parallel()

	desc := readToolDescription(t, false)
	if !strings.Contains(desc, "Audio files are transcribed to text") {
		t.Fatalf("expected read description to mention audio transcription, got:\n%s", desc)
	}
}

// compile-time guard: the stub satisfies the narrow interface the provider
// consumes, so tests exercise the same seam production wiring uses.
var _ ReadTranscriber = (*stubReadTranscriber)(nil)
