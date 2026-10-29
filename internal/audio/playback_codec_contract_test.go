package audio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
	"github.com/gorilla/websocket"
)

// Only the unresolved configuration/MIME seam. The default-MP3 full adapter
// proof and browser MSE/Blob measurements are reused, not repeated here.
func TestAlibabaPlaybackCodecConfigurationContract(t *testing.T) {
	if os.Getenv("MEMOH_CODEC_CONTRACT") != "1" {
		t.Skip("opt-in loopback WSS; never a paid provider")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	fixtures := map[string][]byte{}
	for kind, path := range map[string]string{"mp3": "docs/testing/chat-read-aloud/streaming/codec-contract/fixtures/sine.mp3", "wav": "docs/testing/chat-read-aloud/streaming/codec-contract/fixtures/sine.wav"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		fixtures[kind] = data
	}
	cases := []struct{ name, providerFormat, modelJSON, wantFormat, wantMIME string }{
		{"model_mp3_overrides_provider_wav", "wav", `{"voice":"synthetic-clone","format":"mp3"}`, "mp3", "audio/mpeg"},
		{"model_wav_overrides_provider_mp3", "mp3", `{"voice":"synthetic-clone","format":"wav"}`, "wav", "audio/wav"},
		{"response_format_does_not_select_qwen_codec", "wav", `{"voice":"synthetic-clone","response_format":"mp3"}`, "wav", "audio/wav"},
	}
	records := []map[string]any{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			release := make(chan struct{})
			command := make(chan map[string]any, 1)
			var calls atomic.Int32
			data := fixtures[tc.wantFormat]
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
				_ = conn.SetWriteDeadline(time.Now().Add(4 * time.Second))
				var run, cont, finish map[string]any
				if err = conn.ReadJSON(&run); err != nil {
					t.Error(err)
					return
				}
				header := run["header"].(map[string]any)
				task := header["task_id"]
				event := func(name string) error {
					return conn.WriteJSON(map[string]any{"header": map[string]any{"event": name, "task_id": task}})
				}
				if err = event("task-started"); err != nil {
					t.Error(err)
					return
				}
				if err = conn.ReadJSON(&cont); err != nil {
					t.Error(err)
					return
				}
				if err = conn.ReadJSON(&finish); err != nil {
					t.Error(err)
					return
				}
				command <- run
				if err = conn.WriteMessage(websocket.BinaryMessage, data[:8403]); err != nil {
					t.Error(err)
					return
				}
				select {
				case <-release:
				case <-ctx.Done():
					return
				}
				if err = conn.WriteMessage(websocket.BinaryMessage, data[8403:]); err != nil {
					t.Error(err)
					return
				}
				if err = event("task-finished"); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			pool := x509.NewCertPool()
			pool.AddCert(upstream.Certificate())
			previous := websocket.DefaultDialer
			dialer := *previous
			dialer.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
			websocket.DefaultDialer = &dialer
			defer func() { websocket.DefaultDialer = previous }()
			providerID, _ := db.ParseUUID("10000000-0000-4000-8000-000000000001")
			modelID, _ := db.ParseUUID("10000000-0000-4000-8000-000000000002")
			cfg, _ := json.Marshal(map[string]any{"api_key": "synthetic-only", "base_url": "wss" + strings.TrimPrefix(upstream.URL, "https"), "voice": "provider-voice-must-not-win", "format": tc.providerFormat})
			q := &playbackStreamQueries{provider: sqlc.Provider{ID: providerID, ClientType: string(models.ClientTypeAlibabaSpeech), Enable: true, Config: cfg}, model: sqlc.GetSpeechModelWithProviderRow{ID: modelID, ProviderID: providerID, ModelID: "qwen-audio-3.1-tts-flash", Enable: true, Config: []byte(tc.modelJSON)}}
			before, _ := json.Marshal([]any{q.provider, q.model})
			stream, err := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), q, NewRegistry()).StreamPlayback(ctx, "10000000-0000-4000-8000-000000000002", "synthetic visible text")
			if err != nil {
				t.Fatal(err)
			}
			if stream.ContentType != tc.wantMIME {
				t.Fatalf("MIME %q, want %q", stream.ContentType, tc.wantMIME)
			}
			var run map[string]any
			select {
			case run = <-command:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			payload := run["payload"].(map[string]any)
			params := payload["parameters"].(map[string]any)
			if payload["model"] != "qwen-audio-3.1-tts-flash" || params["voice"] != "synthetic-clone" || params["format"] != tc.wantFormat || params["sample_rate"] != float64(22050) {
				t.Fatal("model, voice, codec or sample rate contract changed")
			}
			var received []byte
			select {
			case first := <-stream.Stream:
				if !bytes.Equal(first, data[:8403]) {
					t.Fatal("first binary chunk differs")
				}
				received = append(received, first...)
			case <-ctx.Done():
				t.Fatal("first chunk waited for EOF")
			}
			close(release)
			for chunk := range stream.Stream {
				received = append(received, chunk...)
			}
			if err = stream.Err(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(received, data) {
				t.Fatal("complete encoded audio changed")
			}
			after, _ := json.Marshal([]any{q.provider, q.model})
			if !bytes.Equal(before, after) {
				t.Fatal("configuration mutated")
			}
			if calls.Load() != 1 {
				t.Fatal("configuration caused another synthesis")
			}
			hash := sha256.Sum256(received)
			records = append(records, map[string]any{"case": tc.name, "providerFormat": tc.providerFormat, "serializedFormat": params["format"], "responseMIME": stream.ContentType, "voiceUnchanged": true, "sampleRate": params["sample_rate"], "fixtureSHA256": hex.EncodeToString(hash[:]), "firstBinaryBeforeEOF": true, "completeBytesEqual": true, "localWSSTasks": calls.Load(), "realCloudCalls": 0, "configUnchanged": true})
		})
	}
	if t.Failed() {
		return
	}
	out := os.Getenv("MEMOH_CODEC_CONTRACT_OUT")
	if out != "" {
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.MarshalIndent(records, "", "  ")
		if err := os.WriteFile(filepath.Join(out, "configuration-wss-contract.json"), append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
