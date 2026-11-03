package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/workspace/bridge"
)

// ReadTranscriber is the narrow seam through which the read tool's audio
// branch reaches the existing transcription service. The concrete wiring
// lives with the provider construction; the tool package only sees these
// two calls, mirroring the transcribe_audio admission semantics.
type ReadTranscriber interface {
	// TranscriptionModelID returns the transcription model configured for the
	// bot, or an empty string when the bot has none.
	TranscriptionModelID(ctx context.Context, botID string) string
	// Transcribe converts audio bytes to text. It has the same contract as the
	// service entry the transcribe_audio tool already uses.
	Transcribe(ctx context.Context, modelID string, audio []byte, filename string, contentType string) (*sdk.TranscriptionResult, error)
}

// readAudioTranscribeBudget bounds the whole audio branch. It aligns with the
// transcription adapter's own 90s client timeout; other read branches keep the
// generic 30s containerOpTimeout.
const readAudioTranscribeBudget = 90 * time.Second

// readAudioMaxEncodedBytes is the transcription input limit after base64
// encoding, matching the upstream ASR adapter's hard cap.
const readAudioMaxEncodedBytes = 10_000_000

// SetTranscriber wires the audio transcription seam. Without it, or without a
// bot-level transcription model, the audio branch fails with a machine error.
func (p *ContainerProvider) SetTranscriber(transcriber ReadTranscriber) {
	p.transcriber = transcriber
}

// execReadAudio re-reads the file under its own 90s budget and returns the
// transcription as a text-shaped result. The initial probe stream is left to
// execRead's defer; this branch opens a fresh reader so its lifetime is not
// bound to the 30s operation context.
func (p *ContainerProvider) execReadAudio(ctx context.Context, client *bridge.Client, filePath, mimeType string, session SessionContext) (any, error) {
	audioCtx, audioCancel := context.WithTimeout(ctx, readAudioTranscribeBudget)
	defer audioCancel()

	reader, err := client.ReadRaw(audioCtx, filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	audio, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}

	if p.transcriber == nil {
		return nil, errors.New("audio transcription is not configured for this bot")
	}
	modelID := strings.TrimSpace(p.transcriber.TranscriptionModelID(audioCtx, session.BotID))
	if modelID == "" {
		return nil, errors.New("audio transcription is not configured for this bot")
	}
	// The adapter re-validates, but a local check returns the machine-readable
	// limit message without paying for a doomed upstream round trip.
	prefix := len("data:" + mimeType + ";base64,")
	maxRaw := (readAudioMaxEncodedBytes - prefix) / 4 * 3
	if len(audio) > maxRaw {
		return nil, errors.New("audio exceeds transcription limits (10 MB encoded, 5 minutes)")
	}

	result, err := p.transcriber.Transcribe(audioCtx, modelID, audio, path.Base(filePath), mimeType)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":               true,
		"path":             filePath,
		"mime":             mimeType,
		"size":             len(audio),
		"content":          result.Text,
		"language":         result.Language,
		"duration_seconds": result.DurationSeconds,
	}, nil
}

// readAudioContentType reports the audio MIME type for the read sniff stage.
// Content sniffing decides when it recognizes audio; otherwise a binary probe
// plus a known audio extension routes container types the sniffer cannot name
// (m4a, flac, ogg payloads). Plain text keeps the text path even when the
// extension suggests audio.
func readAudioContentType(probe []byte, filePath string) (string, bool) {
	sniffed := strings.ToLower(strings.TrimSpace(http.DetectContentType(probe)))
	if strings.HasPrefix(sniffed, "audio/") {
		return sniffed, true
	}
	if bytes.IndexByte(probe, 0) < 0 {
		return "", false
	}
	switch strings.ToLower(path.Ext(filePath)) {
	case ".mp3":
		return "audio/mpeg", true
	case ".m4a":
		return "audio/mp4", true
	case ".wav":
		return "audio/wav", true
	case ".ogg", ".opus":
		return "audio/ogg", true
	case ".flac":
		return "audio/flac", true
	case ".aac":
		return "audio/aac", true
	}
	return "", false
}
