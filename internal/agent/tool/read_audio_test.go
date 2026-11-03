package tools

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

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

func TestReadAudioBeyondLegacySizeGateStillTranscribes(t *testing.T) {
	t.Parallel()

	// The local size gate is gone (2026-11-03 ruling: no artificial limits
	// unless unavoidable). A payload larger than the deleted gate — sized as
	// the old (10 MB encoded - data URL prefix) / 4 * 3 raw budget — flows
	// to the transcriber whole; the provider's documented encoded-input
	// limit stays enforced by the shared ASR adapter, not by the read tool.
	prefix := len("data:audio/mpeg;base64,")
	legacyMaxRaw := (10_000_000 - prefix) / 4 * 3
	oversized := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, legacyMaxRaw)...)

	transcriber := &stubReadTranscriber{modelID: "qwen3-asr-flash"}
	provider := newReadAudioProvider(t, map[string][]byte{
		"media/long.mp3": oversized,
	}, transcriber)

	result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/media/long.mp3"})
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
	if len(transcriber.gotAudio) != len(oversized) {
		t.Fatalf("expected the full %d-byte payload to reach the transcriber, got %d bytes", len(oversized), len(transcriber.gotAudio))
	}
}

func TestReadAudioTranscribeBudgetScalesWithEstimatedDuration(t *testing.T) {
	t.Parallel()

	wav := newReadAudioWAVFixture(8000, 24_000) // 3 s at an 8 kB/s byte rate
	if got, want := readAudioTranscribeBudget(wav), 96*time.Second; got != want {
		t.Fatalf("wav budget = %v, want %v (90 s base + 2 s per second of 3 s audio)", got, want)
	}
	mp4 := newReadAudioMP4Fixture(1000, 2500) // 2.5 s at a 1000 Hz timescale
	if got, want := readAudioTranscribeBudget(mp4), 95*time.Second; got != want {
		t.Fatalf("mp4 budget = %v, want %v (90 s base + 2 s per second of 2.5 s audio)", got, want)
	}
	// Headerless payloads fall back to the floor data rate: 2000 bytes at
	// 1 kB/s estimates 2 s of audio.
	floor := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, 1990)...)
	if got, want := readAudioTranscribeBudget(floor), 94*time.Second; got != want {
		t.Fatalf("floor budget = %v, want %v (90 s base + 2 s per second of 2 s estimated audio)", got, want)
	}
	// The budget grows with the payload, so longer audio is never cut off
	// by a fixed number.
	bigger := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, 10_000)...)
	if readAudioTranscribeBudget(bigger) <= readAudioTranscribeBudget(floor) {
		t.Fatal("expected a larger payload to yield a larger transcription budget")
	}
}

func TestReadAudioEstimatedSecondsParserFallbacks(t *testing.T) {
	t.Parallel()

	// A RIFF header truncated before the data chunk falls back to the floor
	// estimate instead of trusting partial header fields.
	truncatedWav := newReadAudioWAVFixture(8000, 24_000)[:20]
	if got, want := readAudioEstimatedSeconds(truncatedWav), 20.0/readAudioFloorBytesPerSecond; got != want {
		t.Fatalf("truncated wav estimate = %v, want %v", got, want)
	}
	// An ISO-BMFF box size pointing beyond the file falls back the same way.
	wholeMp4 := newReadAudioMP4Fixture(1000, 2500)
	brokenMp4 := append([]byte{}, wholeMp4[:16]...)       // the ftyp box stays intact
	brokenMp4 = append(brokenMp4, 0xFF, 0xFF, 0xFF, 0xFF) // moov box size beyond the file
	brokenMp4 = append(brokenMp4, wholeMp4[20:]...)
	if got, want := readAudioEstimatedSeconds(brokenMp4), float64(len(brokenMp4))/readAudioFloorBytesPerSecond; got != want {
		t.Fatalf("broken mp4 estimate = %v, want %v", got, want)
	}
}

// newReadAudioWAVFixture builds a minimal RIFF/WAVE payload whose fmt chunk
// advertises byteRate bytes per second and whose data chunk holds dataBytes
// payload bytes.
func newReadAudioWAVFixture(byteRate uint32, dataBytes int) []byte {
	fmtBody := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtBody[0:2], 1) // PCM
	binary.LittleEndian.PutUint16(fmtBody[2:4], 1) // mono
	binary.LittleEndian.PutUint32(fmtBody[4:8], byteRate)
	binary.LittleEndian.PutUint32(fmtBody[8:12], byteRate)
	binary.LittleEndian.PutUint16(fmtBody[12:14], 1)
	binary.LittleEndian.PutUint16(fmtBody[14:16], 8)
	chunk := func(id string, body []byte) []byte {
		head := make([]byte, 8)
		copy(head[0:4], id)
		binary.LittleEndian.PutUint32(head[4:8], uint32(len(body)))
		return append(head, body...)
	}
	out := append([]byte("RIFF\x00\x00\x00\x00"), []byte("WAVE")...)
	out = append(out, chunk("fmt ", fmtBody)...)
	out = append(out, chunk("data", make([]byte, dataBytes))...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}

// newReadAudioMP4Fixture builds a minimal ftyp+moov/mvhd payload with a v0
// movie header carrying the given timescale and duration.
func newReadAudioMP4Fixture(timescale, duration uint32) []byte {
	box := func(typ string, body []byte) []byte {
		b := make([]byte, 8+len(body))
		binary.BigEndian.PutUint32(b[0:4], uint32(len(b)))
		copy(b[4:8], typ)
		copy(b[8:], body)
		return b
	}
	mvhdBody := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhdBody[12:16], timescale)
	binary.BigEndian.PutUint32(mvhdBody[16:20], duration)
	out := box("ftyp", []byte("M4A \x00\x00\x00\x00"))
	out = append(out, box("moov", box("mvhd", mvhdBody))...)
	return out
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
