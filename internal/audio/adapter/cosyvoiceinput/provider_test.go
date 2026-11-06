package cosyvoiceinput

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	speech "github.com/felinics/twilight/provider/alibabacloud/speech"
	sdk "github.com/felinics/twilight/sdk"
	"github.com/gorilla/websocket"

	"github.com/felinics/memoh/internal/audio/input"
)

type fixture struct {
	mu       sync.Mutex
	commands []map[string]any
	server   *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		for {
			var command map[string]any
			if c.ReadJSON(&command) != nil {
				return
			}
			f.mu.Lock()
			f.commands = append(f.commands, command)
			f.mu.Unlock()
			header := command["header"].(map[string]any)
			id := header["task_id"]
			action := header["action"]
			switch action {
			case "run-task":
				_ = c.WriteJSON(map[string]any{"header": map[string]any{"event": "task-started", "task_id": id}})
			case "continue-task":
				_ = c.WriteMessage(websocket.BinaryMessage, []byte{1, 2})
			case "finish-task":
				_ = c.WriteJSON(map[string]any{"header": map[string]any{"event": "task-finished", "task_id": id}})
				return
			}
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}
func (f *fixture) url() string { return "ws" + strings.TrimPrefix(f.server.URL, "http") }
func (f *fixture) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.commands {
		out = append(out, c["header"].(map[string]any)["action"].(string))
	}
	return out
}

func TestPinnedFixedTextCannotKeepTwoInputsInOneTask(t *testing.T) {
	f := newFixture(t)
	p := speech.New(speech.WithBaseURL(f.url()), speech.WithAPIKey("synthetic-only"))
	for _, text := range []string{"甲😀", "乙。"} {
		s, err := p.DoStream(context.Background(), sdk.SpeechParams{Model: &sdk.SpeechModel{ID: "configured-fixture-model"}, Text: text, Config: map[string]any{"voice": "configured-fixture-voice"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Bytes(); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for _, a := range f.actions() {
		if a == "run-task" {
			count++
		}
	}
	if count != 2 {
		b, _ := json.Marshal(f.actions())
		t.Fatalf("negative control: pinned fixed-text calls should expose two tasks, got %d, commands=%s", count, b)
	}
}

func TestNativeInputKeepsContextAcrossUnicodeFramesAndFinishesOnlyAfterTail(t *testing.T) {
	f := newFixture(t)
	p := New("synthetic-only", f.url())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	s, err := p.OpenInput(ctx, sdk.SpeechParams{Model: &sdk.SpeechModel{ID: "configured-fixture-model"}, Config: map[string]any{"voice": "configured-fixture-voice", "format": "mp3", "sample_rate": 24000}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cancel()
	if err = s.Append(ctx, input.Frame{Sequence: 0, Offset: 0, Text: "甲😀"}); err != nil {
		t.Fatal(err)
	}
	select {
	case audio := <-s.Audio().Stream:
		if string(audio) != string([]byte{1, 2}) {
			t.Fatalf("audio %v", audio)
		}
	case <-ctx.Done():
		t.Fatal("no audio before input finish")
	}
	if err = s.Append(ctx, input.Frame{Sequence: 0, Offset: 0, Text: "duplicate"}); !errors.Is(err, ErrSequence) {
		t.Fatalf("duplicate %v", err)
	}
	if err = s.Append(ctx, input.Frame{Sequence: 1, Offset: 1, Text: "wrong offset"}); !errors.Is(err, ErrSequence) {
		t.Fatalf("offset %v", err)
	}
	if err = s.Append(ctx, input.Frame{Sequence: 1, Offset: 2, Text: "乙。"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Append(ctx, input.Frame{Sequence: 2, Offset: 4, Final: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Audio().Bytes(); err != nil {
		t.Fatal(err)
	}
	want := []string{"run-task", "continue-task", "continue-task", "finish-task"}
	got := f.actions()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("commands %v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.commands[0]["header"].(map[string]any)["task_id"]
	for _, command := range f.commands {
		if command["header"].(map[string]any)["task_id"] != id {
			t.Fatal("context restarted")
		}
	}
	payload := f.commands[0]["payload"].(map[string]any)
	if payload["model"] != "configured-fixture-model" || payload["parameters"].(map[string]any)["voice"] != "configured-fixture-voice" {
		t.Fatal("configuration changed")
	}
	if err = s.Append(ctx, input.Frame{Sequence: 3, Offset: 4, Text: "late"}); err == nil {
		t.Fatal("accepted after finish")
	}
}

func TestCancelClosesBlockedReadAndNeverAcceptsLateInput(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	s, err := New("synthetic-only", f.url()).OpenInput(ctx, sdk.SpeechParams{Model: &sdk.SpeechModel{ID: "configured-fixture-model"}, Config: map[string]any{"voice": "configured-fixture-voice"}})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Audio().Bytes(); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked read did not cancel")
	}
	if err = s.Append(context.Background(), input.Frame{Text: "late"}); err == nil {
		t.Fatal("accepted canceled input")
	}
	if got := f.actions(); len(got) != 1 || got[0] != "run-task" {
		t.Fatalf("post-cancel commands %v", got)
	}
}

func TestInputRejectsEarlyEOFOrForeignTaskInsteadOfCachingCompleteAudio(t *testing.T) {
	for _, event := range []string{"task-finished", "foreign"} {
		t.Run(event, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = c.Close() }()
				var command map[string]any
				if c.ReadJSON(&command) != nil {
					return
				}
				id := command["header"].(map[string]any)["task_id"]
				_ = c.WriteJSON(map[string]any{"header": map[string]any{"event": "task-started", "task_id": id}})
				if c.ReadJSON(&command) != nil {
					return
				}
				_ = c.WriteMessage(websocket.BinaryMessage, []byte{1, 2})
				if event == "foreign" {
					id = "another-task"
				}
				_ = c.WriteJSON(map[string]any{"header": map[string]any{"event": "task-finished", "task_id": id}})
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
			defer cancel()
			s, err := New("synthetic-only", "ws"+strings.TrimPrefix(server.URL, "http")).OpenInput(ctx, sdk.SpeechParams{Model: &sdk.SpeechModel{ID: "configured-fixture-model"}, Config: map[string]any{"voice": "configured-fixture-voice"}})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Cancel()
			if err = s.Append(ctx, input.Frame{Text: "not finished"}); err != nil {
				t.Fatal(err)
			}
			bytes, err := s.Audio().Bytes()
			if string(bytes) != string([]byte{1, 2}) || !errors.Is(err, ErrProtocol) {
				t.Fatalf("bytes=%v error=%v", bytes, err)
			}
		})
	}
}
