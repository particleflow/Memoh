package cosyvoiceinput

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/gorilla/websocket"

	"github.com/felinics/memoh/internal/audio/input"
)

// Exact-model protocol coverage, not a provider synthesis or codec test.
// Sources: https://help.aliyun.com/en/model-studio/qwen-audio-tts-websocket-api
// and its client-events / server-events pages, inspected 2026-10-30.
func TestQwenAudio31DocumentedDuplexProtocol(t *testing.T) {
	for _, format := range []string{"mp3", "wav"} {
		t.Run(format, func(t *testing.T) {
			const first = "完整一句😀。"
			const tail = "末尾没有句号"
			serverResult := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serverResult <- serveQwenAudioFixture(w, r, format, first, tail)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			s, err := New("synthetic-only", "ws"+strings.TrimPrefix(server.URL, "http")).OpenInput(ctx, sdk.SpeechParams{
				Model: &sdk.SpeechModel{ID: "qwen-audio-3.1-tts-flash"},
				Config: map[string]any{
					"voice": "synthetic-configured-voice", "format": format,
					"sample_rate": 24000, "volume": 60, "rate": 1.1, "pitch": 0.9,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Cancel()
			wantMIME := map[string]string{"mp3": "audio/mpeg", "wav": "audio/wav"}[format]
			if s.Audio().ContentType != wantMIME {
				t.Fatalf("configured format changed: %q", s.Audio().ContentType)
			}
			if err = s.Append(ctx, input.Frame{Text: first}); err != nil {
				t.Fatal(err)
			}
			select {
			case bytes := <-s.Audio().Stream:
				if string(bytes) != "prefix-binary-only" {
					t.Fatalf("metadata leaked into audio: %q", bytes)
				}
			case <-ctx.Done():
				t.Fatal("complete sentence did not yield audio before finish")
			}
			if err = s.Append(ctx, input.Frame{Sequence: 1, Offset: 6, Text: tail}); err != nil {
				t.Fatal(err)
			}
			// The server buffers the incomplete sentence until this one finish.
			if err = s.Append(ctx, input.Frame{Sequence: 2, Offset: 12, Final: true}); err != nil {
				t.Fatal(err)
			}
			bytes, err := s.Audio().Bytes()
			if err != nil || string(bytes) != "tail-binary-only" {
				t.Fatalf("final tail bytes=%q err=%v", bytes, err)
			}
			select {
			case err = <-serverResult:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("fixture did not finish")
			}
			if err = s.Append(ctx, input.Frame{Sequence: 3, Offset: 12, Text: "late"}); err == nil {
				t.Fatal("completed Qwen task accepted late input")
			}
		})
	}
}

type qwenAudioCommand struct {
	Header struct {
		Action    string `json:"action"`
		TaskID    string `json:"task_id"`
		Streaming string `json:"streaming"`
	} `json:"header"`
	Payload struct {
		TaskGroup  string         `json:"task_group"`
		Task       string         `json:"task"`
		Function   string         `json:"function"`
		Model      string         `json:"model"`
		Parameters map[string]any `json:"parameters"`
		Input      map[string]any `json:"input"`
	} `json:"payload"`
}

func serveQwenAudioFixture(w http.ResponseWriter, r *http.Request, format, first, tail string) error {
	if r.Header.Get("Authorization") != "Bearer synthetic-only" {
		return errors.New("unexpected synthetic authorization")
	}
	c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	var command qwenAudioCommand
	if err = c.ReadJSON(&command); err != nil {
		return err
	}
	id := command.Header.TaskID
	p := command.Payload
	if command.Header.Action != "run-task" || command.Header.Streaming != "duplex" || id == "" ||
		p.Model != "qwen-audio-3.1-tts-flash" || p.TaskGroup != "audio" || p.Task != "tts" || p.Function != "SpeechSynthesizer" || p.Input == nil || len(p.Input) != 0 {
		return fmt.Errorf("unexpected Qwen run-task: %+v", command)
	}
	want := map[string]any{"text_type": "PlainText", "voice": "synthetic-configured-voice", "format": format, "sample_rate": float64(24000), "volume": float64(60), "rate": 1.1, "pitch": 0.9}
	for key, value := range want {
		if p.Parameters[key] != value {
			return fmt.Errorf("configured parameter %s changed: %v", key, p.Parameters[key])
		}
	}
	event := func(name string, payload any) error {
		return c.WriteJSON(map[string]any{"header": map[string]any{"task_id": id, "event": name}, "payload": payload})
	}
	if err = event("task-started", map[string]any{}); err != nil {
		return err
	}
	for index, text := range []string{first, tail, ""} {
		command = qwenAudioCommand{}
		if err = c.ReadJSON(&command); err != nil {
			return err
		}
		action := "continue-task"
		if index == 2 {
			action = "finish-task"
		}
		if command.Header.TaskID != id || command.Header.Streaming != "duplex" || command.Header.Action != action ||
			(index < 2 && command.Payload.Input["text"] != text) || (index == 2 && len(command.Payload.Input) != 0) {
			return fmt.Errorf("input order/context/coverage changed at %d: %+v", index, command)
		}
		if index == 1 {
			continue // Incomplete sentence: no audio until finish-task.
		}
		sentenceIndex := 0
		if index == 2 {
			text = tail
			sentenceIndex = 1
		}
		for _, kind := range []string{"sentence-begin", "sentence-synthesis", "sentence-end"} {
			payload := map[string]any{"output": map[string]any{"type": kind, "original_text": text, "sentence": map[string]any{"index": sentenceIndex, "words": []any{}}}}
			if kind == "sentence-end" {
				payload["usage"] = map[string]any{"input_tokens": 4, "output_tokens": 8, "total_tokens": 12}
			}
			if err = event("result-generated", payload); err != nil {
				return err
			}
			if kind == "sentence-synthesis" {
				audio := "prefix-binary-only"
				if index == 2 {
					audio = "tail-binary-only"
				}
				if err = c.WriteMessage(websocket.BinaryMessage, []byte(audio)); err != nil {
					return err
				}
			}
		}
	}
	return event("task-finished", map[string]any{"usage": map[string]any{"input_tokens": 8, "output_tokens": 16, "total_tokens": 24}})
}
