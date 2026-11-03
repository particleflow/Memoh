package handlers

import (
	"bytes"
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
	audiopkg "github.com/felinics/memoh/internal/audio"
	"github.com/felinics/memoh/internal/audio/adapter/cosyvoiceinput"
	"github.com/felinics/memoh/internal/audio/input"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/server"
)

// Field observation gates (2026-11-04 TTS drop forensics): every playback
// input session leaves one structured, text-free line behind, and a PUT that
// arrives after the input closed is visible instead of silently dropped.
type observationInputAudio struct {
	*playbackAudio
	url      string
	opens    atomic.Int32
	identity atomic.Value
}

func (a *observationInputAudio) PlaybackIdentity(context.Context, string) (string, error) {
	return a.identity.Load().(string), nil
}

func (a *observationInputAudio) PlaybackInputInfo(context.Context, string) (string, string, error) {
	return a.identity.Load().(string), "audio/mpeg", nil
}

func (a *observationInputAudio) PlaybackReuseInfo(_ context.Context, botID, modelID, text string) (audiopkg.PlaybackReuseKey, string, string, error) {
	return audiopkg.PlaybackReusePrefixKey(botID, modelID, "observation-fixture-voice", text), a.identity.Load().(string), "audio/mpeg", nil
}

func (a *observationInputAudio) OpenPlaybackInput(ctx context.Context, _ string) (input.Session, string, error) {
	a.opens.Add(1)
	session, err := cosyvoiceinput.New("synthetic-only", a.url).OpenInput(ctx, sdk.SpeechParams{Model: &sdk.SpeechModel{ID: "observation-fixture-model"}, Config: map[string]any{"voice": "observation-fixture-voice"}})
	return session, a.identity.Load().(string), err
}

func observationServer(t *testing.T) (*httptest.Server, *observationInputAudio, <-chan map[string]any, *bytes.Buffer, string) {
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
	audio := &observationInputAudio{playbackAudio: &playbackAudio{}, url: "ws" + strings.TrimPrefix(ws.URL, "http")}
	audio.identity.Store("observation-config")
	q := newInvocationTestQueries()
	q.bot = testBotRow(invocationTestBotID, nil)
	q.bot.OwnerUserID = testUUID(invocationTestOwnerID)
	account := accounts.NewService(nil, &playbackBoundaryAccounts{active: true})
	logs := &bytes.Buffer{}
	h := &BotAudioHandler{audioService: audio, settingsService: &playbackBoundarySettings{}, logger: slog.New(slog.NewJSONHandler(logs, nil))}
	// Reuse on (an empty store): the recorder mirrors frames and audio, which
	// is what the observation line reports.
	h.reuse = newReuseMemoryStore()
	h.SetPlaybackAccess(bots.NewService(nil, q), account)
	const secret = "synthetic-native-input-observation"
	token, _, err := auth.GenerateInternalToken(invocationTestOwnerID, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.NewServerWithSessionValidator(slog.New(slog.DiscardHandler), "", secret, func(ctx context.Context, user, _ string) error { return account.ValidateSession(ctx, user) }, h))
	t.Cleanup(httpServer.Close)
	return httpServer, audio, commands, logs, token
}

func observationRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func TestPlaybackInputSessionLeavesOneStructuredTextFreeLine(t *testing.T) {
	server, audio, commands, logs, token := observationServer(t)
	const id = "55555555-5555-4555-8555-555555555555"
	response := nativeRequest(t, server, token, http.MethodPost, id, "甲😀", 0, 0, false)
	defer func() { _ = response.Body.Close() }()
	nextInputCommand(t, commands, "run-task")
	nextInputCommand(t, commands, "continue-task")
	appendResponse := nativeRequest(t, server, token, http.MethodPut, id, "乙。", 1, 2, false)
	_ = appendResponse.Body.Close()
	nextInputCommand(t, commands, "continue-task")
	final := nativeRequest(t, server, token, http.MethodPut, id, "", 2, 4, true)
	_ = final.Body.Close()
	nextInputCommand(t, commands, "finish-task")
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	records := observationRecords(t, logs)
	var session map[string]any
	for _, record := range records {
		if record["msg"] == "playback_input" && record["event"] == "session" {
			if session != nil {
				t.Fatal("more than one session line")
			}
			session = record
		}
	}
	if session == nil {
		t.Fatal("missing playback_input session line")
	}
	if session["session_id"] != id || session["outcome"] != "synthesized" {
		t.Fatalf("session line %v", session)
	}
	if session["frames"] != float64(3) || session["codepoints"] != float64(4) {
		t.Fatalf("frame/codepoint counts %v", session)
	}
	if session["contiguous"] != true {
		t.Fatalf("contiguous %v", session)
	}
	if session["audio_bytes"] != float64(4) {
		t.Fatalf("audio bytes %v", session)
	}
	if _, ok := session["duration_ms"]; !ok {
		t.Fatal("missing duration")
	}
	if _, ok := session["text"]; ok {
		t.Fatal("the observation line must never carry text")
	}
	if audio.opens.Load() != 1 {
		t.Fatalf("opens %d", audio.opens.Load())
	}
}

func TestPlaybackInputCacheHitSessionLogsOutcomeAndCounts(t *testing.T) {
	server, audio, commands, logs, token := observationServer(t)
	const first = "77777777-7777-4777-8777-777777777777"
	response := nativeRequest(t, server, token, http.MethodPost, first, "甲😀", 0, 0, false)
	defer func() { _ = response.Body.Close() }()
	nextInputCommand(t, commands, "run-task")
	nextInputCommand(t, commands, "continue-task")
	rest := nativeRequest(t, server, token, http.MethodPut, first, "乙。", 1, 2, false)
	_ = rest.Body.Close()
	nextInputCommand(t, commands, "continue-task")
	final := nativeRequest(t, server, token, http.MethodPut, first, "", 2, 4, true)
	_ = final.Body.Close()
	nextInputCommand(t, commands, "finish-task")
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	const replay = "88888888-8888-4888-8888-888888888888"
	replayResponse := nativeRequest(t, server, token, http.MethodPost, replay, "甲😀", 0, 0, false)
	defer func() { _ = replayResponse.Body.Close() }()
	replayRest := nativeRequest(t, server, token, http.MethodPut, replay, "乙。", 1, 2, false)
	_ = replayRest.Body.Close()
	replayFinal := nativeRequest(t, server, token, http.MethodPut, replay, "", 2, 4, true)
	_ = replayFinal.Body.Close()
	if _, err := io.ReadAll(replayResponse.Body); err != nil {
		t.Fatal(err)
	}
	var hit map[string]any
	for _, record := range observationRecords(t, logs) {
		if record["msg"] == "playback_input" && record["event"] == "session" && record["outcome"] == "cache_hit" {
			hit = record
		}
	}
	if hit == nil {
		t.Fatal("missing cache_hit session line")
	}
	if hit["session_id"] != replay || hit["frames"] != float64(3) || hit["codepoints"] != float64(4) {
		t.Fatalf("cache_hit line %v", hit)
	}
	if audio.opens.Load() != 1 {
		t.Fatalf("a cache hit must not open another provider task: opens=%d", audio.opens.Load())
	}
}

func TestPlaybackInputAppendAfterCloseIsLoggedNotSilent(t *testing.T) {
	server, _, commands, logs, token := observationServer(t)
	const id = "66666666-6666-4666-8666-666666666666"
	response := nativeRequest(t, server, token, http.MethodPost, id, "甲", 0, 0, false)
	defer func() { _ = response.Body.Close() }()
	nextInputCommand(t, commands, "run-task")
	nextInputCommand(t, commands, "continue-task")
	final := nativeRequest(t, server, token, http.MethodPut, id, "", 1, 1, true)
	_ = final.Body.Close()
	nextInputCommand(t, commands, "finish-task")
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	late := nativeRequest(t, server, token, http.MethodPut, id, "迟到的增量", 2, 1, false)
	_ = late.Body.Close()
	if late.StatusCode != http.StatusNotFound {
		t.Fatalf("late append %d", late.StatusCode)
	}
	records := observationRecords(t, logs)
	var missed map[string]any
	for _, record := range records {
		if record["msg"] == "playback_input" && record["event"] == "append_missed" {
			missed = record
		}
	}
	if missed == nil {
		t.Fatal("a PUT after close must leave an append_missed line")
	}
	if missed["session_id"] != id || missed["level"] != "WARN" {
		t.Fatalf("append_missed line %v", missed)
	}
	if _, ok := missed["text"]; ok {
		t.Fatal("the miss line must never carry text")
	}
}
