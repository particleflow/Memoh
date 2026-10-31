// Package cosyvoiceinput adds the documented DashScope duplex input contract
// shared by CosyVoice and Qwen-Audio-TTS to the registered Twilight provider.
// It does not dispatch by model name or implement Qwen-TTS-Realtime events.
// Fixed-text methods remain Twilight-owned.
package cosyvoiceinput

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	speech "github.com/felinics/twilight/provider/alibabacloud/speech"
	sdk "github.com/felinics/twilight/sdk"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/felinics/memoh/internal/audio/input"
)

var (
	ErrSequence = errors.New("speech input sequence or offset mismatch")
	ErrFinished = errors.New("speech input is closed")
	ErrProtocol = errors.New("invalid CosyVoice input session response")
)

// Provider preserves all existing SDK methods and opts in to input.Provider.
type Provider struct {
	*speech.Provider
	endpoint, key string
}

func New(key, endpoint string) *Provider {
	opts := []speech.Option{speech.WithAPIKey(key)}
	if endpoint != "" {
		opts = append(opts, speech.WithBaseURL(endpoint))
	} else {
		endpoint = "wss://dashscope.aliyuncs.com/api-ws/v1/inference/"
	}
	return &Provider{Provider: speech.New(opts...), endpoint: endpoint, key: key}
}

// InputContentType validates the exact protocol/configuration without dialing.
func (p *Provider) InputContentType(params sdk.SpeechParams) (string, error) {
	// Remote TLS remains verified by the normal dialer. Plain WS is confined to
	// the loopback protocol fixture, never a production TLS override.
	endpoint, err := url.Parse(p.endpoint)
	if err != nil {
		return "", errors.New("invalid secure CosyVoice endpoint")
	}
	loopback := endpoint.Hostname() == "localhost" || net.ParseIP(endpoint.Hostname()).IsLoopback()
	secure := endpoint.Scheme == "wss" || endpoint.Scheme == "ws" && loopback
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || !secure {
		return "", errors.New("invalid secure CosyVoice endpoint")
	}

	if params.Model == nil || strings.TrimSpace(params.Model.ID) == "" {
		return "", errors.New("speech input model is required")
	}
	voice, _ := params.Config["voice"].(string)
	if strings.TrimSpace(voice) == "" {
		return "", errors.New("speech input voice is required")
	}
	if ssml, _ := params.Config["enable_ssml"].(bool); ssml {
		return "", errors.New("CosyVoice SSML does not support incremental input")
	}
	format, _ := params.Config["format"].(string)
	if format == "" {
		format = "mp3"
	}
	mime := map[string]string{"mp3": "audio/mpeg", "wav": "audio/wav", "pcm": "audio/pcm", "opus": "audio/opus"}[format]
	if mime == "" {
		return "", errors.New("invalid CosyVoice audio format")
	}
	return mime, nil
}

func (p *Provider) OpenInput(ctx context.Context, params sdk.SpeechParams) (input.Session, error) {
	mime, err := p.InputContentType(params)
	if err != nil {
		return nil, err
	}
	voice, _ := params.Config["voice"].(string)
	format, _ := params.Config["format"].(string)
	if format == "" {
		format = "mp3"
	}

	headers := http.Header{"Authorization": []string{"Bearer " + p.key}, "X-DashScope-DataInspection": []string{"enable"}}
	conn, response, err := websocket.DefaultDialer.DialContext(ctx, p.endpoint, headers)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("connect CosyVoice input: %w", err)
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &session{ctx: sessionCtx, cancel: cancel, conn: conn, task: uuid.NewString(), started: make(chan error, 1), done: make(chan struct{}), data: make(chan []byte, 8), errors: make(chan error, 1)}
	s.audio = sdk.NewSpeechStreamResult(s.data, mime, s.errors)
	parameters := map[string]any{"text_type": "PlainText", "voice": voice, "format": format, "sample_rate": 22050}
	// Match the existing configured Twilight fields. No model/voice defaults,
	// provider switch, secret config passthrough or invented context overlap.
	// Preserve pinned Twilight numeric parsing and omission of zero controls;
	// protocol defaults remain unchanged. Model and voice are never substituted.
	for _, key := range []string{"sample_rate", "volume", "rate", "pitch"} {
		if value, ok := params.Config[key]; ok {
			number, err := strconv.ParseFloat(fmt.Sprint(value), 64)
			if err != nil {
				continue
			}
			if key == "sample_rate" {
				parameters[key] = int(number)
			} else if number != 0 {
				parameters[key] = number
			}
		}
	}

	command := s.command("run-task", map[string]any{})
	command["payload"] = map[string]any{"task_group": "audio", "task": "tts", "function": "SpeechSynthesizer", "model": params.Model.ID, "parameters": parameters, "input": map[string]any{}}
	stop := context.AfterFunc(sessionCtx, func() { _ = conn.Close() })
	if err = conn.WriteJSON(command); err != nil {
		stop()
		s.Cancel()
		return nil, err
	}
	go s.read(stop)
	select {
	case err = <-s.started:
		if err != nil {
			return nil, err
		}
		return s, nil
	case <-sessionCtx.Done():
		return nil, sessionCtx.Err()
	}
}

type session struct {
	ctx              context.Context
	cancel           context.CancelFunc
	conn             *websocket.Conn
	task             string
	mu               sync.Mutex
	sequence, offset uint64
	final            bool
	started          chan error
	done             chan struct{}
	data             chan []byte
	errors           chan error
	audio            *sdk.SpeechStreamResult
}

func (s *session) Audio() *sdk.SpeechStreamResult { return s.audio }
func (s *session) Cancel()                        { s.cancel(); _ = s.conn.Close() }
func (s *session) command(action string, payload map[string]any) map[string]any {
	return map[string]any{"header": map[string]any{"action": action, "task_id": s.task, "streaming": "duplex"}, "payload": map[string]any{"input": payload}}
}

func (s *session) Append(ctx context.Context, frame input.Frame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.final {
		return ErrFinished
	}
	if frame.Sequence != s.sequence || frame.Offset != s.offset {
		return ErrSequence
	}
	if !utf8.ValidString(frame.Text) || (frame.Text == "" && !frame.Final) {
		return errors.New("invalid empty or UTF-8 speech input")
	}
	var count uint64
	for range frame.Text {
		count++
		if count > 20000 {
			return errors.New("CosyVoice input limit exceeded")
		}
	}
	if s.offset+count > 200000 {
		return errors.New("CosyVoice input limit exceeded")
	}
	// A canceled/uncertain write closes this whole task; never replay an append.
	stop := context.AfterFunc(ctx, func() { s.Cancel() })
	defer stop()
	if frame.Text != "" {
		if err := s.conn.WriteJSON(s.command("continue-task", map[string]any{"text": frame.Text})); err != nil {
			s.Cancel()
			return err
		}
	}
	if frame.Final {
		s.final = true
		if err := s.conn.WriteJSON(s.command("finish-task", map[string]any{})); err != nil {
			s.Cancel()
			return err
		}
	}
	s.sequence++
	s.offset += count
	return nil
}

func (s *session) read(stop func() bool) {
	var result error
	started := false
	defer func() {
		if s.ctx.Err() != nil {
			result = s.ctx.Err()
		}
		if !started {
			if result == nil {
				result = ErrProtocol
			}
			s.started <- result
		}
		stop()
		s.cancel()
		_ = s.conn.Close()
		if result != nil {
			s.errors <- result
		}
		close(s.errors)
		close(s.data)
		close(s.done)
	}()
	for {
		kind, bytes, err := s.conn.ReadMessage()
		if err != nil {
			result = err
			return
		}
		if kind == websocket.BinaryMessage {
			if !started {
				result = ErrProtocol
				return
			}
			if len(bytes) > 0 {
				select {
				case s.data <- bytes:
				case <-s.ctx.Done():
					return
				}
			}
			continue
		}
		if kind != websocket.TextMessage {
			result = ErrProtocol
			return
		}
		var event struct {
			Header struct {
				Event  string `json:"event"`
				TaskID string `json:"task_id"`
			} `json:"header"`
		}
		if err = json.Unmarshal(bytes, &event); err != nil || event.Header.TaskID != s.task {
			result = ErrProtocol
			return
		}
		switch event.Header.Event {
		case "task-started":
			if started {
				result = ErrProtocol
				return
			}
			started = true
			s.started <- nil
		case "result-generated": // Provider-owned metadata is not text or extra audio.
		case "task-finished":
			s.mu.Lock()
			finished := s.final
			s.mu.Unlock()
			if !started || !finished {
				result = ErrProtocol
			}
			return
		case "task-failed":
			result = errors.New("CosyVoice input task failed")
			return
		default:
			result = ErrProtocol
			return
		}
	}
}
