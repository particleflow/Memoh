package imagecapability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5"
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
	model                              sqlc.Model
	provider                           sqlc.Provider
	settingsErr, modelErr, providerErr error
}

func (q *catalog) GetSettingsByBotID(context.Context, pgtype.UUID) (sqlc.GetSettingsByBotIDRow, error) {
	return sqlc.GetSettingsByBotIDRow{ImageModelID: q.model.ID}, q.settingsErr
}

func (q *catalog) GetModelByID(context.Context, pgtype.UUID) (sqlc.Model, error) {
	return q.model, q.modelErr
}

func (q *catalog) GetProviderByID(context.Context, pgtype.UUID) (sqlc.Provider, error) {
	return q.provider, q.providerErr
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
			if body["model"] != "synthetic-image" {
				t.Error("Bot image model was not used")
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
			cfg[ConfigKey] = Config{Protocol: "openai-images"}
		}
		q.provider.Config, _ = json.Marshal(cfg)
	}
	setConfig(false)
	log := slog.New(slog.DiscardHandler)
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

func TestLegacyDiscoveryDefersLookupFailuresUntilExecution(t *testing.T) {
	for _, mode := range []string{"settings_failure", "missing_model", "model_query_failure", "missing_provider", "provider_query_failure"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			p, server := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/chat/completions" {
					t.Error("legacy request protocol changed")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"synthetic recovered response"},"finish_reason":"stop"}]}`))
			})
			cfg, _ := json.Marshal(map[string]any{"base_url": server.URL + "/v1"})
			q := &catalog{model: sqlc.Model{ID: uuid("00000000-0000-0000-0000-000000000002"), ProviderID: uuid("00000000-0000-0000-0000-000000000003"), ModelID: "synthetic-image", Enable: true, Config: []byte(`{"compatibilities":["image-output"]}`)}, provider: sqlc.Provider{ClientType: "openai-completions", Enable: true, Config: cfg}}
			switch mode {
			case "settings_failure":
				q.settingsErr = errors.New("synthetic settings query failure")
			case "missing_model":
				q.modelErr = pgx.ErrNoRows
			case "model_query_failure":
				q.modelErr = errors.New("synthetic model query failure")
			case "missing_provider":
				q.providerErr = pgx.ErrNoRows
			case "provider_query_failure":
				q.providerErr = errors.New("synthetic provider query failure")
			}
			log := slog.New(slog.DiscardHandler)
			settingsSvc, modelsSvc := settings.NewService(log, q, nil, nil), models.NewService(log, q)
			legacy := tools.NewImageGenProvider(log, settingsSvc, modelsSvc, q, p.media)
			wrapped := New(legacy, settingsSvc, modelsSvc, q, p.media, nil)
			session := tools.SessionContext{BotID: "00000000-0000-0000-0000-000000000001", CurrentPlatform: "local", ReplyTarget: "synthetic-conversation", LiveStream: true, Emitter: func(tools.ToolStreamEvent) {}}
			original, err := legacy.Tools(t.Context(), session)
			if err != nil {
				t.Fatal(err)
			}
			current, err := wrapped.Tools(t.Context(), session)
			if err != nil {
				t.Fatalf("lookup error moved from execution to discovery: %v", err)
			}
			a, _ := json.Marshal(original)
			b, _ := json.Marshal(current)
			if !bytes.Equal(a, b) {
				t.Fatal("legacy discovery schema or visibility changed")
			}
			if mode == "settings_failure" {
				if len(current) != 0 {
					t.Fatal("legacy suppresses settings discovery failure")
				}
				return
			}
			if len(current) != 1 || current[0].Name != "generate_image" {
				t.Fatal("configured image ID lost its legacy tool")
			}
			available := tools.NewAvailableTools(current)
			if want := legacy.Usage(t.Context(), session, available); want == "" || wrapped.Usage(t.Context(), session, available) != want {
				t.Fatal("lookup failure changed legacy usage")
			}
			exec := &toolexec.ToolExecContext{Context: t.Context(), ToolCallID: "synthetic-lookup-call"}
			args := sdk.ParseToolArguments(`{"prompt":"synthetic"}`)
			if _, err := original[0].Execute(exec, args); err == nil {
				t.Fatal("legacy should reject unresolved configuration during execution")
			}
			if _, err := current[0].Execute(exec, args); err == nil || calls.Load() != 0 {
				t.Fatal("unresolved configuration did not fail before HTTP")
			}
			q.modelErr, q.providerErr = nil, nil
			out, err := current[0].Execute(exec, args)
			if err != nil || !strings.Contains(out.String(), "synthetic recovered response") || calls.Load() != 1 {
				t.Fatalf("same advertised tool did not recover legacy execution: %v", err)
			}
		})
	}
}

func TestLegacyDiscoveryDoesNotBypassExplicitProtocolAtExecution(t *testing.T) {
	for _, initiallyUnavailable := range []bool{false, true} {
		for _, raw := range []string{`null`, `{"protocol":"unsupported","model_mode":"omit"}`, `{"protocol":"openai-images","model_mode":"omit"}`} {
			t.Run(fmt.Sprintf("unavailable_%v/%s", initiallyUnavailable, raw), func(t *testing.T) {
				var calls atomic.Int32
				p, server := fixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
				cfg, _ := json.Marshal(map[string]any{"base_url": server.URL + "/v1"})
				q := &catalog{model: sqlc.Model{ID: uuid("00000000-0000-0000-0000-000000000002"), ProviderID: uuid("00000000-0000-0000-0000-000000000003"), ModelID: "synthetic-image", Enable: true, Config: []byte(`{"compatibilities":["image-output"]}`)}, provider: sqlc.Provider{ClientType: "openai-completions", Enable: true, Config: cfg}}
				if initiallyUnavailable {
					q.providerErr = errors.New("synthetic provider query failure")
				}
				log := slog.New(slog.DiscardHandler)
				settingsSvc, modelsSvc := settings.NewService(log, q, nil, nil), models.NewService(log, q)
				legacy := tools.NewImageGenProvider(log, settingsSvc, modelsSvc, q, p.media)
				wrapped := New(legacy, settingsSvc, modelsSvc, q, p.media, nil)
				session := tools.SessionContext{BotID: "00000000-0000-0000-0000-000000000001"}
				list, err := wrapped.Tools(t.Context(), session)
				if err != nil || len(list) != 1 {
					t.Fatalf("legacy tool not advertised: %v", err)
				}
				q.providerErr = nil
				q.provider.Config, _ = json.Marshal(map[string]any{"base_url": server.URL + "/v1", ConfigKey: json.RawMessage(raw)})
				if _, err := list[0].Execute(&toolexec.ToolExecContext{Context: t.Context()}, sdk.ParseToolArguments(`{"prompt":"synthetic"}`)); err == nil || calls.Load() != 0 {
					t.Fatal("stale legacy tool dispatched after explicit protocol selection")
				}
				if list, err := wrapped.Tools(t.Context(), session); err == nil || len(list) != 0 {
					t.Fatal("known invalid explicit configuration fell back to legacy")
				}
			})
		}
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

func TestEditMaskRejectedBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	p, _ := fixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "edit_image", `{"prompt":"synthetic edit","images":["missing"],"mask":"missing"}`)
	if !result.IsError || calls.Load() != 0 {
		t.Fatal("removed mask dispatched")
	}
}

func TestTimeoutIsUnknownWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	p, _ := fixture(t, func(_ http.ResponseWriter, r *http.Request) {
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
