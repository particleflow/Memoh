package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/gorilla/websocket"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/audio/adapter/cosyvoiceinput"
	"github.com/felinics/memoh/internal/audio/input"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/server"
)

type lifecycleInputAudio struct {
	*playbackAudio
	url      string
	opens    atomic.Int32
	identity atomic.Value
}

func (a *lifecycleInputAudio) PlaybackIdentity(context.Context, string) (string, error) {
	return a.identity.Load().(string), nil
}

func (a *lifecycleInputAudio) PlaybackInputInfo(context.Context, string) (string, string, error) {
	return a.identity.Load().(string), "audio/mpeg", nil
}

func (a *lifecycleInputAudio) OpenPlaybackInput(ctx context.Context, _ string) (input.Session, string, error) {
	a.opens.Add(1)
	s, err := cosyvoiceinput.New("synthetic-only", a.url).OpenInput(ctx, sdk.SpeechParams{Model: &sdk.SpeechModel{ID: "configured-fixture-model"}, Config: map[string]any{"voice": "configured-fixture-voice"}})
	return s, a.identity.Load().(string), err
}

func inputLifecycleServer(t *testing.T) (*httptest.Server, *lifecycleInputAudio, <-chan map[string]any, string) {
	t.Helper()
	commands := make(chan map[string]any, 16)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		continues := 0
		for {
			var command map[string]any
			if c.ReadJSON(&command) != nil {
				return
			}
			commands <- command
			header := command["header"].(map[string]any)
			id := header["task_id"]
			switch header["action"] {
			case "run-task":
				_ = c.WriteJSON(map[string]any{"header": map[string]any{"event": "task-started", "task_id": id}})
			case "continue-task":
				continues++
				if continues == 2 {
					_ = c.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3, 4})
				}
			case "finish-task":
				_ = c.WriteJSON(map[string]any{"header": map[string]any{"event": "task-finished", "task_id": id}})
				return
			}
		}
	}))
	t.Cleanup(ws.Close)
	audio := &lifecycleInputAudio{playbackAudio: &playbackAudio{}, url: "ws" + strings.TrimPrefix(ws.URL, "http")}
	audio.identity.Store("fixture-config-one")
	q := newInvocationTestQueries()
	q.bot = testBotRow(invocationTestBotID, nil)
	q.bot.OwnerUserID = testUUID(invocationTestOwnerID)
	account := accounts.NewService(nil, &playbackBoundaryAccounts{active: true})
	h := &BotAudioHandler{audioService: audio, settingsService: &playbackBoundarySettings{}}
	h.SetPlaybackAccess(bots.NewService(nil, q), account)
	const secret = "synthetic-native-input-loopback-boundary"
	token, _, err := auth.GenerateToken(invocationTestOwnerID, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.NewServerWithSessionValidator(slog.New(slog.DiscardHandler), "", secret, account.ValidateSession, h))
	t.Cleanup(httpServer.Close)
	return httpServer, audio, commands, token
}

func nativeRequest(t *testing.T, server *httptest.Server, token, method, id, text string, seq, offset uint64, final bool) *http.Response {
	t.Helper()
	body, err := json.Marshal(playbackInputRequest{ID: id, Frame: input.Frame{Sequence: seq, Offset: offset, Text: text, Final: final}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, server.URL+"/bots/"+invocationTestBotID+"/tts/playback/input", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	//nolint:gosec // G704: URL is the owned httptest loopback server, never request/user input.
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func nextInputCommand(t *testing.T, commands <-chan map[string]any, action string) map[string]any {
	t.Helper()
	select {
	case command := <-commands:
		if command["header"].(map[string]any)["action"] != action {
			t.Fatalf("unexpected command %v", command["header"])
		}
		return command
	case <-time.After(time.Second):
		t.Fatal("missing command " + action)
		return nil
	}
}

func TestNativeHTTPInputKeepsOneTaskAndStreamsBeforeFinishWithoutReopeningMissingOrDuplicateInput(t *testing.T) {
	server, audio, commands, token := inputLifecycleServer(t)
	const id = "77777777-7777-4777-8777-777777777777"
	response := nativeRequest(t, server, token, http.MethodPost, id, "甲😀", 0, 0, false)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Speech-Playback-Identity") != "fixture-config-one" {
		t.Fatalf("response %v", response.StatusCode)
	}
	run := nextInputCommand(t, commands, "run-task")
	nextInputCommand(t, commands, "continue-task")
	// No audio has been sent yet: receiving headers proves the HTTP ingress is
	// ready for the next frame without waiting on the model's first sound.
	duplicate := nativeRequest(t, server, token, http.MethodPost, id, "duplicate", 0, 0, false)
	_ = duplicate.Body.Close()
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate %d", duplicate.StatusCode)
	}
	wrong := nativeRequest(t, server, token, http.MethodPut, "88888888-8888-4888-8888-888888888888", "late", 1, 2, false)
	_ = wrong.Body.Close()
	if wrong.StatusCode != http.StatusNotFound {
		t.Fatalf("missing %d", wrong.StatusCode)
	}
	appendResponse := nativeRequest(t, server, token, http.MethodPut, id, "乙。", 1, 2, false)
	_ = appendResponse.Body.Close()
	if appendResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("append %d", appendResponse.StatusCode)
	}
	next := nextInputCommand(t, commands, "continue-task")
	if next["header"].(map[string]any)["task_id"] != run["header"].(map[string]any)["task_id"] {
		t.Fatal("task restarted")
	}
	bytes := make([]byte, 4)
	if _, err := io.ReadFull(response.Body, bytes); err != nil {
		t.Fatal(err)
	}
	if string(bytes) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("audio %v", bytes)
	}
	final := nativeRequest(t, server, token, http.MethodPut, id, "", 2, 4, true)
	_ = final.Body.Close()
	if final.StatusCode != http.StatusNoContent {
		t.Fatalf("finish %d", final.StatusCode)
	}
	nextInputCommand(t, commands, "finish-task")
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	late := nativeRequest(t, server, token, http.MethodPut, id, "late", 3, 4, false)
	_ = late.Body.Close()
	if late.StatusCode != http.StatusNotFound {
		t.Fatalf("late %d", late.StatusCode)
	}
	if audio.opens.Load() != 1 {
		t.Fatalf("opens %d", audio.opens.Load())
	}
}

func TestNativeHTTPConfigCancellationIsTruncatedEOFAndNeverACompleteCacheBody(t *testing.T) {
	server, audio, commands, token := inputLifecycleServer(t)
	const id = "99999999-9999-4999-8999-999999999999"
	response := nativeRequest(t, server, token, http.MethodPost, id, "甲😀", 0, 0, false)
	defer func() { _ = response.Body.Close() }()
	nextInputCommand(t, commands, "run-task")
	nextInputCommand(t, commands, "continue-task")
	audio.identity.Store("fixture-config-changed")
	denied := nativeRequest(t, server, token, http.MethodPut, id, "乙。", 1, 2, false)
	_ = denied.Body.Close()
	if denied.StatusCode != http.StatusConflict {
		t.Fatalf("configuration %d", denied.StatusCode)
	}
	if _, err := io.ReadAll(response.Body); err == nil {
		t.Fatal("configuration cancellation became clean EOF and could be cached")
	}
	if audio.opens.Load() != 1 || len(commands) != 0 {
		t.Fatal("cancellation opened or appended another synthesis")
	}
}
