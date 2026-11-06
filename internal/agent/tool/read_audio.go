package tools

import (
	"bytes"
	"context"
	"encoding/binary"
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

const (
	// readAudioFetchBudget bounds pulling the audio payload out of the
	// workspace bridge. It is a local I/O bound for the fetch phase only, not
	// a transcription limit; the transcription phase gets its own
	// duration-scaled budget below.
	readAudioFetchBudget = 90 * time.Second

	// readAudioTranscribeBase is the fixed share of the transcription budget.
	// It covers the parts that do not scale with audio duration: connection
	// setup, base64 encoding and starting the upload.
	readAudioTranscribeBase = 90 * time.Second

	// readAudioTranscribePerSecond is the per-second-of-audio share of the
	// transcription budget. Genuine ASR work grows linearly with duration
	// while flash-tier ASR answers synchronously much faster than real time,
	// so two seconds of wall clock per second of audio assumes the service
	// runs no faster than half real time: an order-of-magnitude margin over
	// real processing that still fires on a stalled call.
	readAudioTranscribePerSecond = 2 * time.Second

	// readAudioFloorBytesPerSecond bounds audio duration from payload size
	// when the container header is not parsed. 8 kbps sits at or below the
	// lowest usable speech data rate (Opus 6 kbps, MP3 minimum 8 kbps), so
	// dividing the payload size by this rate over-estimates duration instead
	// of under-estimating it, and the budget never cuts off legitimate work.
	readAudioFloorBytesPerSecond = 1000.0
)

// SetTranscriber wires the audio transcription seam. Without it, or without a
// bot-level transcription model, the audio branch fails with a machine error.
func (p *ContainerProvider) SetTranscriber(transcriber ReadTranscriber) {
	p.transcriber = transcriber
}

// execReadAudio re-reads the file and returns the transcription as a
// text-shaped result. The fetch runs under the fixed local I/O bound; the
// transcription call then runs under the duration-scaled liveness budget from
// readAudioTranscribeBudget, so a long clip is not cut off by a fixed number
// while a stalled call still fails within a finite, content-scaled deadline.
// The initial probe stream is left to execRead's defer; this branch opens a
// fresh reader so its lifetime is not bound to the 30s operation context.
func (p *ContainerProvider) execReadAudio(ctx context.Context, client *bridge.Client, filePath, mimeType string, session SessionContext) (any, error) {
	fetchCtx, fetchCancel := context.WithTimeout(ctx, readAudioFetchBudget)
	defer fetchCancel()

	reader, err := client.ReadRaw(fetchCtx, filePath)
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
	transcribeCtx, transcribeCancel := context.WithTimeout(ctx, readAudioTranscribeBudget(audio))
	defer transcribeCancel()
	modelID := strings.TrimSpace(p.transcriber.TranscriptionModelID(transcribeCtx, session.BotID))
	if modelID == "" {
		return nil, errors.New("audio transcription is not configured for this bot")
	}

	result, err := p.transcriber.Transcribe(transcribeCtx, modelID, audio, path.Base(filePath), mimeType)
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

// readAudioTranscribeBudget returns the transcription-phase liveness budget
// for an audio payload: readAudioTranscribeBase plus
// readAudioTranscribePerSecond per estimated second of audio. The duration
// estimate is an upper bound, so genuine work always fits inside the budget.
func readAudioTranscribeBudget(audio []byte) time.Duration {
	return readAudioTranscribeBase + time.Duration(readAudioEstimatedSeconds(audio)*float64(readAudioTranscribePerSecond))
}

// readAudioEstimatedSeconds estimates the audio duration in seconds. RIFF/WAVE
// and ISO-BMFF (m4a/mp4) headers carry an exact duration, which keeps the
// budget tight for the common uncompressed and phone-recording formats.
// Everything else, including any header the parsers reject, falls back to the
// payload size at readAudioFloorBytesPerSecond, which over-estimates duration.
func readAudioEstimatedSeconds(audio []byte) float64 {
	if seconds, ok := readAudioWavSeconds(audio); ok {
		return seconds
	}
	if seconds, ok := readAudioMp4Seconds(audio); ok {
		return seconds
	}
	return float64(len(audio)) / readAudioFloorBytesPerSecond
}

// readAudioWavSeconds reads the duration from a RIFF/WAVE payload: the fmt
// chunk's average byte rate divides the data chunk size.
func readAudioWavSeconds(audio []byte) (float64, bool) {
	const headerLen = 12
	if len(audio) < headerLen || !bytes.Equal(audio[0:4], []byte("RIFF")) || !bytes.Equal(audio[8:headerLen], []byte("WAVE")) {
		return 0, false
	}
	var byteRate uint32
	pos := headerLen
	for pos+8 <= len(audio) {
		chunkID := audio[pos : pos+4]
		chunkSize := int64(binary.LittleEndian.Uint32(audio[pos+4 : pos+8]))
		body := pos + 8
		if chunkSize > int64(len(audio)-body) {
			return 0, false
		}
		switch string(chunkID) {
		case "fmt ":
			// The average byte rate sits 8 bytes into the fmt payload,
			// behind the format tag and channel count.
			if chunkSize < 16 {
				return 0, false
			}
			byteRate = binary.LittleEndian.Uint32(audio[body+8 : body+12])
		case "data":
			if byteRate == 0 {
				return 0, false
			}
			return float64(chunkSize) / float64(byteRate), true
		}
		// RIFF chunks are 2-byte aligned.
		pos = body + int(chunkSize) + int(chunkSize&1)
	}
	return 0, false
}

// readAudioMp4Seconds reads the duration from an ISO-BMFF movie header
// (moov/mvhd), which every m4a/mp4 recording carries.
func readAudioMp4Seconds(audio []byte) (float64, bool) {
	if len(audio) < 8 || !bytes.Equal(audio[4:8], []byte("ftyp")) {
		return 0, false
	}
	pos := 0
	for pos+8 <= len(audio) {
		boxSize := int64(binary.BigEndian.Uint32(audio[pos : pos+4]))
		boxType := audio[pos+4 : pos+8]
		if boxSize < 8 || boxSize > int64(len(audio)-pos) {
			return 0, false
		}
		if bytes.Equal(boxType, []byte("moov")) {
			return readAudioMvhdSeconds(audio[pos+8 : pos+int(boxSize)])
		}
		pos += int(boxSize)
	}
	return 0, false
}

// readAudioMvhdSeconds reads the movie header box inside a moov box.
func readAudioMvhdSeconds(moov []byte) (float64, bool) {
	pos := 0
	for pos+8 <= len(moov) {
		boxSize := int64(binary.BigEndian.Uint32(moov[pos : pos+4]))
		boxType := moov[pos+4 : pos+8]
		if boxSize < 8 || boxSize > int64(len(moov)-pos) {
			return 0, false
		}
		if !bytes.Equal(boxType, []byte("mvhd")) {
			pos += int(boxSize)
			continue
		}
		body := moov[pos+8 : pos+int(boxSize)]
		// Version+flags precede the fixed fields; the v0 timescale and
		// duration sit at body offsets 12 and 16.
		if len(body) < 20 {
			return 0, false
		}
		if body[0] == 1 {
			if len(body) < 32 {
				return 0, false
			}
			timescale := binary.BigEndian.Uint32(body[20:24])
			duration := binary.BigEndian.Uint64(body[24:32])
			if timescale == 0 || duration == 0 {
				return 0, false
			}
			return float64(duration) / float64(timescale), true
		}
		timescale := binary.BigEndian.Uint32(body[12:16])
		duration := binary.BigEndian.Uint32(body[16:20])
		if timescale == 0 || duration == 0 {
			return 0, false
		}
		return float64(duration) / float64(timescale), true
	}
	return 0, false
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
