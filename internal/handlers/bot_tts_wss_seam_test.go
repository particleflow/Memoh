package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/audio"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/models"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
)

type wssSeamQueries struct {
	dbstore.Queries
	provider sqlc.Provider
	model    sqlc.GetSpeechModelWithProviderRow
}

func (q *wssSeamQueries) GetProviderByID(context.Context, pgtype.UUID) (sqlc.Provider, error) {
	return q.provider, nil
}
func (q *wssSeamQueries) GetSpeechModelWithProvider(context.Context, pgtype.UUID) (sqlc.GetSpeechModelWithProviderRow, error) {
	return q.model, nil
}

type wssSeamCase struct {
	mu                  sync.Mutex
	gate                chan struct{}
	Released            bool   `json:"released"`
	Started             bool   `json:"started"`
	UpstreamFinished    bool   `json:"upstreamFinished"`
	HandlerCanceled     bool   `json:"handlerCanceled"`
	LateFramesAttempted int    `json:"lateFramesAttempted"`
	FramesSent          int    `json:"framesSent"`
	Failure             string `json:"failure,omitempty"`
}

func (s *wssSeamCase) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"released": s.Released, "started": s.Started, "upstreamFinished": s.UpstreamFinished, "handlerCanceled": s.HandlerCanceled, "lateFramesAttempted": s.LateFramesAttempted, "framesSent": s.FramesSent, "failure": s.Failure}
}
func (s *wssSeamCase) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Released {
		s.Released = true
		close(s.gate)
	}
}

// Opt-in integration seam, not an application server or cloud synthesis. The
// registry, pinned Alibaba SDK, playback handler, generated JS SDK, composable,
// controller and native MSE player are real. Only DB/auth boundary and upstream
// speech are controlled fixtures. Run this exact test; old suites need not run.
func TestPlaybackAlibabaWSSNativePlayerSeam(t *testing.T) {
	if os.Getenv("MEMOH_WSS_SEAM_BROWSER") != "1" {
		t.Skip("opt-in loopback WSS/HTTP/native Chromium seam")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	fixture, err := os.ReadFile(filepath.Join(root, "internal/handlers/testdata/read-aloud-wss-seam.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	frames, err := wssSeamMP3Frames(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 50 {
		t.Fatal("fixture requires multiple real MPEG frames")
	}
	cases := map[string]*wssSeamCase{"complete": {gate: make(chan struct{})}, "cancel": {gate: make(chan struct{})}, "collect": {gate: make(chan struct{})}}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-only" {
			http.Error(w, "fixture credentials", 401)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
		// Keep protocol fields literal so a model/voice/default-format regression is visible.
		var raw map[string]any
		if err = conn.ReadJSON(&raw); err != nil {
			t.Error(err)
			return
		}
		header, _ := raw["header"].(map[string]any)
		payload, _ := raw["payload"].(map[string]any)
		params, _ := payload["parameters"].(map[string]any)
		task := header["task_id"]
		if header["action"] != "run-task" || header["streaming"] != "duplex" || payload["model"] != "qwen-audio-3.1-tts-flash" || params["voice"] != "synthetic-clone-fixture" || params["format"] != "mp3" || params["sample_rate"] != float64(22050) {
			t.Errorf("unexpected actual adapter command: %v", raw)
			return
		}
		event := func(name string) error {
			return conn.WriteJSON(map[string]any{"header": map[string]any{"event": name, "task_id": task}, "payload": map[string]any{}})
		}
		if err = event("task-started"); err != nil {
			t.Error(err)
			return
		}
		if err = conn.ReadJSON(&raw); err != nil {
			t.Error(err)
			return
		}
		h := raw["header"].(map[string]any)
		p := raw["payload"].(map[string]any)
		input := p["input"].(map[string]any)
		name, _ := input["text"].(string)
		s := cases[name]
		if s == nil || h["action"] != "continue-task" || h["task_id"] != task {
			t.Errorf("unexpected continue: %v", raw)
			return
		}
		if err = conn.ReadJSON(&raw); err != nil {
			t.Error(err)
			return
		}
		h = raw["header"].(map[string]any)
		if h["action"] != "finish-task" || h["task_id"] != task {
			t.Errorf("unexpected finish: %v", raw)
			return
		}
		s.mu.Lock()
		s.Started = true
		s.mu.Unlock()
		for i, frame := range frames {
			if i == 40 {
				select {
				case <-s.gate:
				case <-ctx.Done():
					return
				}
			}
			if i >= 40 {
				s.mu.Lock()
				s.LateFramesAttempted++
				s.mu.Unlock()
			}
			if err = event("result-generated"); err == nil {
				err = conn.WriteMessage(websocket.BinaryMessage, frame)
			}
			if err != nil {
				if name != "cancel" {
					s.mu.Lock()
					s.Failure = err.Error()
					s.mu.Unlock()
				}
				return
			}
			s.mu.Lock()
			s.FramesSent++
			s.mu.Unlock()
		}
		if err = event("task-finished"); err == nil {
			s.mu.Lock()
			s.UpstreamFinished = true
			s.mu.Unlock()
		}
	}))
	defer upstream.Close()
	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())
	oldDialer := websocket.DefaultDialer
	dialer := *oldDialer
	dialer.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	websocket.DefaultDialer = &dialer
	defer func() { websocket.DefaultDialer = oldDialer }()
	providerID, _ := db.ParseUUID("10000000-0000-4000-8000-000000000001")
	modelID, _ := db.ParseUUID("10000000-0000-4000-8000-000000000002")
	providerConfig, _ := json.Marshal(map[string]any{"api_key": "synthetic-only", "base_url": "wss" + strings.TrimPrefix(upstream.URL, "https")})
	q := &wssSeamQueries{provider: sqlc.Provider{ID: providerID, ClientType: string(models.ClientTypeAlibabaSpeech), Enable: true, Config: providerConfig}, model: sqlc.GetSpeechModelWithProviderRow{ID: modelID, ProviderID: providerID, ModelID: "qwen-audio-3.1-tts-flash", Enable: true, Config: []byte(`{"voice":"synthetic-clone-fixture"}`)}}
	before, _ := json.Marshal([]any{q.provider, q.model})
	service := audio.NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), q, audio.NewRegistry())
	h := &BotAudioHandler{audioService: service, settingsService: &playbackSettings{model: "10000000-0000-4000-8000-000000000002"}, playbackAccess: func(_ context.Context, actor, bot string) error {
		if actor != "11111111-1111-1111-1111-111111111111" || bot != "bot" {
			return fmt.Errorf("fixture identity mismatch")
		}
		return nil
	}}

	// The original playback called this same Synthesize method. Exercise its
	// actual Alibaba collector with the same held upstream; no copied adapter.
	type collected struct {
		data []byte
		err  error
	}
	full := make(chan collected, 1)
	go func() {
		data, _, err := service.Synthesize(ctx, "10000000-0000-4000-8000-000000000002", "collect", nil)
		full <- collected{data, err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for cases["collect"].snapshot()["framesSent"].(int) < 40 {
		if time.Now().After(deadline) {
			t.Fatal("collector control did not receive first frames")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-full:
		t.Fatal("original collector unexpectedly returned before task-finished")
	default:
	}
	collectorHeldBeforeEOF := true
	cases["collect"].release()
	select {
	case result := <-full:
		if result.err != nil || !bytes.Equal(result.data, fixture) {
			t.Fatalf("collector control completion mismatch: %v", result.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	mux := http.NewServeMux()
	e := echo.New()
	mux.HandleFunc("/bots/bot/tts/playback", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var req struct{ Text string }
		_ = json.Unmarshal(body, &req)
		s := cases[req.Text]
		if s == nil {
			http.Error(w, "unexpected fixture text", 400)
			return
		}
		c := testAuthContext(e, r, w, "11111111-1111-1111-1111-111111111111")
		c.SetParamNames("bot_id")
		c.SetParamValues("bot")
		err = h.Playback(c)
		if r.Context().Err() != nil {
			s.mu.Lock()
			s.HandlerCanceled = true
			s.mu.Unlock()
		}
		if err != nil && r.Context().Err() == nil {
			t.Errorf("playback: %v", err)
		}
	})
	mux.HandleFunc("/seam/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cases[r.URL.Query().Get("case")].snapshot())
	})
	mux.HandleFunc("/seam/release", func(w http.ResponseWriter, r *http.Request) {
		cases[r.URL.Query().Get("case")].release()
		w.WriteHeader(204)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<!doctype html><div id="root"></div>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	out := os.Getenv("MEMOH_WSS_SEAM_OUT")
	if out == "" {
		out = t.TempDir()
	}
	if err = os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "docs/testing/chat-read-aloud/streaming/wss-seam/native-player.mjs"))
	cmd.Dir = root
	sum := sha256.Sum256(fixture)
	cmd.Env = append(os.Environ(), "SEAM_URL="+server.URL, "SEAM_OUT="+out, "SEAM_MP3_SHA256="+hex.EncodeToString(sum[:]))
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("native seam: %v", err)
	}
	after, _ := json.Marshal([]any{q.provider, q.model})
	if !bytes.Equal(before, after) {
		t.Fatal("fixture provider/model config mutated")
	}
	for name, s := range cases {
		status := s.snapshot()
		if status["failure"] != "" || !status["started"].(bool) || !status["released"].(bool) {
			t.Fatalf("%s: %v", name, status)
		}
	}
	report := map[string]any{"originalCollectorHeldBeforeEOF": collectorHeldBeforeEOF, "collect": cases["collect"].snapshot(), "complete": cases["complete"].snapshot(), "cancel": cases["cancel"].snapshot(), "mp3SHA256": hex.EncodeToString(sum[:]), "mpegFrames": len(frames), "modelConfigUnchanged": true, "realCloudCalls": 0, "physicalAudible500msPassed": false, "authBoundary": "synthetic testAuthContext; existing auth tests reused, not revalidated"}
	data, _ := json.MarshalIndent(report, "", "  ")
	if err = os.WriteFile(filepath.Join(out, "adapter-http.json"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

// Parse the generated MPEG-2 Layer III 22050Hz fixture; do not call arbitrary
// byte slices 'MP3 frames'. Preserve the leading ID3 tag in the first WS packet.
func wssSeamMP3Frames(data []byte) ([][]byte, error) {
	offset := 0
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		offset = 10 + (int(data[6]) << 21) + (int(data[7]) << 14) + (int(data[8]) << 7) + int(data[9])
	}
	var frames [][]byte
	start := 0
	for offset < len(data) {
		if offset+4 > len(data) || data[offset] != 255 || data[offset+1]&0xfe != 0xf2 {
			return nil, fmt.Errorf("invalid MPEG-2 Layer III frame at %d", offset)
		}
		h := data[offset+2]
		rates := []int{22050, 24000, 16000}
		rateIndex := int(h>>2) & 3
		bitrates := []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
		kbps := bitrates[h>>4]
		if rateIndex >= len(rates) || kbps == 0 {
			return nil, fmt.Errorf("unsupported MPEG header")
		}
		length := 72000*kbps/rates[rateIndex] + int(h>>1&1)
		if offset+length > len(data) {
			return nil, fmt.Errorf("truncated MPEG frame")
		}
		frames = append(frames, data[start:offset+length])
		offset += length
		start = offset
	}
	return frames, nil
}
