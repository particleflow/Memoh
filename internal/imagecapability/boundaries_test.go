package imagecapability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5/pgtype"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type catalog struct {
	dbstore.Queries
	model    sqlc.Model
	provider sqlc.Provider
}

func (q *catalog) GetSettingsByBotID(context.Context, pgtype.UUID) (sqlc.GetSettingsByBotIDRow, error) {
	return sqlc.GetSettingsByBotIDRow{ImageModelID: q.model.ID}, nil
}
func (q *catalog) GetModelByID(context.Context, pgtype.UUID) (sqlc.Model, error) { return q.model, nil }
func (q *catalog) GetProviderByID(context.Context, pgtype.UUID) (sqlc.Provider, error) {
	return q.provider, nil
}
func uuid(value string) pgtype.UUID { var id pgtype.UUID; _ = id.Scan(value); return id }

func TestProductionResolverPreservesLegacyDefault(t *testing.T) {
	var calls atomic.Int32
	output := pngBytes(t, 17)
	p, server := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-test-key" {
			t.Error("managed synthetic authorization was not used")
		}
		if calls.Load() == 2 {
			if r.URL.Path != "/v1/images/generations" {
				t.Error("explicit protocol not selected")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if _, ok := body["model"]; ok {
				t.Error("explicit default-model policy did not omit model")
			}
			respondImage(w, output)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Error("legacy protocol changed")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"legacy synthetic response"},"finish_reason":"stop"}]}`))
	})
	q := &catalog{model: sqlc.Model{ID: uuid("00000000-0000-0000-0000-000000000002"), ProviderID: uuid("00000000-0000-0000-0000-000000000003"), ModelID: "synthetic-image", Type: "chat", Enable: true, Config: []byte(`{"compatibilities":["image-output"]}`)}, provider: sqlc.Provider{ID: uuid("00000000-0000-0000-0000-000000000003"), ClientType: "openai-completions", Enable: true}}
	setConfig := func(explicit bool) {
		cfg := map[string]any{"base_url": server.URL + "/v1", "api_key": "synthetic-test-key"}
		if explicit {
			cfg[ConfigKey] = Config{Protocol: "openai-images", ModelMode: "omit"}
		}
		q.provider.Config, _ = json.Marshal(cfg)
	}
	setConfig(false)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	settingsSvc := settings.NewService(log, q, nil, nil)
	modelsSvc := models.NewService(log, q)
	legacy := tools.NewImageGenProvider(log, settingsSvc, modelsSvc, q, p.media)
	wrapped := New(legacy, settingsSvc, modelsSvc, q, p.media, p.authorize)
	session := tools.SessionContext{BotID: "00000000-0000-0000-0000-000000000001"}
	original, err := legacy.Tools(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	current, err := wrapped.Tools(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(original)
	b, _ := json.Marshal(current)
	if len(current) != 1 || current[0].Name != "generate_image" || !bytes.Equal(a, b) {
		t.Fatal("default schema/description or uniqueness changed")
	}
	result := call(t, wrapped, session, "generate_image", `{"prompt":"synthetic"}`)
	if result.IsError || !strings.Contains(result.Result.String(), "legacy synthetic response") || calls.Load() != 1 {
		t.Fatal("legacy behavior changed", result.Result.String())
	}
	setConfig(true)
	current, err = wrapped.Tools(t.Context(), session)
	if err != nil || len(current) != 1 || current[0].Name != "generate_image" {
		t.Fatal("explicit config did not replace exactly one image provider", err)
	}
	wrapped.authorize = func(_ context.Context, s tools.SessionContext) error {
		if s.BotID != session.BotID || s.UserID != "synthetic-user" || s.SessionID != "synthetic-session" {
			return errors.New("denied")
		}
		return nil
	}
	result = call(t, wrapped, session, "generate_image", `{"prompt":"synthetic"}`)
	if result.IsError || calls.Load() != 2 || !strings.Contains(result.Result.String(), digest(output)) {
		t.Fatal("managed config did not execute Images protocol", result.Result.String())
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(result.Result.String()), &receipt); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"model", "model_id", "api_key", "base_url", "b64_json"} {
		if _, present := receipt[forbidden]; present {
			t.Fatal("receipt leaked configuration or claimed model provenance")
		}
	}
	q.provider.Enable = false
	if _, err := wrapped.Tools(t.Context(), session); err == nil {
		t.Fatal("disabled provider accepted")
	}
}

func TestAttachmentShortcutKeepsExistingSessionRules(t *testing.T) {
	for _, mode := range []string{"no_reply_target", "subagent"} {
		t.Run(mode, func(t *testing.T) {
			data := pngBytes(t, 18)
			p, _ := fixture(t, func(w http.ResponseWriter, _ *http.Request) { respondImage(w, data) })
			var events int
			s := tools.SessionContext{BotID: "synthetic-bot", CurrentPlatform: "local", LiveStream: true, Emitter: func(tools.ToolStreamEvent) { events++ }}
			if mode == "subagent" {
				s.IsSubagent = true
				s.SessionType = "subagent"
				s.ReplyTarget = "synthetic-conversation"
			}
			result := call(t, p, s, "generate_image", `{"prompt":"synthetic"}`)
			if result.IsError || events != 0 || strings.Contains(result.Result.String(), "delivered") || !strings.Contains(result.Result.String(), digest(data)) {
				t.Fatal("session shortcut was widened", result.Result.String())
			}
		})
	}
}

func TestEditDisabledBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	p, _ := fixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	session := tools.SessionContext{BotID: "synthetic-bot", SessionID: "synthetic-session", UserID: "synthetic-user"}
	list, err := p.Tools(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	resolve := p.resolve
	p.resolve = func(ctx context.Context, bot string) (selection, bool, error) {
		s, b, e := resolve(ctx, bot)
		s.config.Edits = false
		return s, b, e
	}
	available, err := p.Tools(t.Context(), session)
	if err != nil || len(available) != 1 || available[0].Name != "generate_image" {
		t.Fatal("disabled edit advertised")
	}
	for _, tool := range list {
		if tool.Name == "edit_image" {
			_, err = tool.Execute(&toolexec.ToolExecContext{Context: t.Context()}, sdk.ParseToolArguments(`{"prompt":"synthetic","images":["`+strings.Repeat("a", 64)+`"]}`))
			if err == nil {
				t.Fatal("stale edit tool executed after disable")
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("disabled edit dispatched or fell back")
	}
}

type observedStorage struct {
	*localfs.Originals
	opens, lists int
}

func (s *observedStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	s.opens++
	return s.Originals.Open(ctx, key)
}
func (s *observedStorage) ListPrefix(ctx context.Context, key string) ([]string, error) {
	s.lists++
	return s.Originals.ListPrefix(ctx, key)
}

func TestDeniedConversationDoesNotOpenExistingHash(t *testing.T) {
	p, _ := fixture(t, func(http.ResponseWriter, *http.Request) { t.Error("denied request dispatched") })
	storage := &observedStorage{Originals: localfs.NewOriginals(t.TempDir())}
	p.media = media.NewService(nil, storage)
	p.media.SetRequirePersistence(true)
	asset, err := p.media.Ingest(t.Context(), media.IngestInput{BotID: "synthetic-bot", Mime: "image/png", Reader: bytes.NewReader(pngBytes(t, 19))})
	if err != nil {
		t.Fatal(err)
	}
	storage.opens, storage.lists = 0, 0
	p.authorize = func(context.Context, tools.SessionContext) error { return errors.New("PRIVATE_AUTHORIZATION") }
	result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "edit_image", `{"prompt":"synthetic","images":["`+asset.ContentHash+`"]}`)
	if !result.IsError || storage.opens != 0 || storage.lists != 0 || strings.Contains(result.Result.String(), "PRIVATE") {
		t.Fatal("existing hash bypassed authority", result.Result.String())
	}
}

func TestModelArgumentsCannotChooseProvider(t *testing.T) {
	for _, field := range []string{"base_url", "endpoint", "api_key", "protocol", "model", "model_mode", "bot_id", "session_id"} {
		t.Run(field, func(t *testing.T) {
			p, _ := fixture(t, func(http.ResponseWriter, *http.Request) { t.Error("model-selected endpoint or authority dispatched") })
			args, _ := json.Marshal(map[string]any{"prompt": "synthetic", field: "synthetic-disallowed"})
			if !call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "generate_image", string(args)).IsError {
				t.Fatal("unexpected argument accepted")
			}
		})
	}
}

func TestEditMultipartConfiguredModelAndMask(t *testing.T) {
	data, mask := pngBytes(t, 21), pngBytes(t, 22)
	p, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" || r.Method != "POST" {
			t.Error("wrong edit protocol")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("model") != "synthetic-selected-image-model" || len(r.MultipartForm.File["image[]"]) != 2 {
			t.Error("configured multipart fields lost")
		}
		for field, expected := range map[string][]byte{"image[]": data, "mask": mask} {
			for _, header := range r.MultipartForm.File[field] {
				f, err := header.Open()
				if err != nil {
					t.Error(err)
					continue
				}
				got, _ := io.ReadAll(f)
				_ = f.Close()
				if !bytes.Equal(got, expected) {
					t.Error("edit input bytes changed")
				}
			}
		}
		if len(r.MultipartForm.File["mask"]) != 1 {
			t.Error("mask absent")
		}
		respondImage(w, data)
	})
	resolve := p.resolve
	p.resolve = func(ctx context.Context, bot string) (selection, bool, error) {
		s, b, e := resolve(ctx, bot)
		s.config.ModelMode = "configured"
		return s, b, e
	}
	for _, b := range [][]byte{data, mask} {
		if _, err := p.media.Ingest(t.Context(), media.IngestInput{BotID: "synthetic-bot", Mime: "image/png", Reader: bytes.NewReader(b)}); err != nil {
			t.Fatal(err)
		}
	}
	args, _ := json.Marshal(map[string]any{"prompt": "synthetic edit", "images": []string{digest(data), digest(data)}, "mask": digest(mask)})
	result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "edit_image", string(args))
	if result.IsError || !strings.Contains(result.Result.String(), digest(data)) {
		t.Fatal(result.Result.String())
	}
}

func TestTimeoutIsUnknownWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	p, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	p.http.Timeout = 20 * time.Millisecond
	result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "generate_image", `{"prompt":"synthetic"}`)
	if result.IsError || calls.Load() != 1 || !strings.Contains(result.Result.String(), `"state":"unknown"`) || !strings.Contains(result.Result.String(), `"tool_call_id":"synthetic-call"`) {
		t.Fatal(result.Result.String())
	}
}
