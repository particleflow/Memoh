package imagecapability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/runtime/native"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func pngBytes(t *testing.T, red uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: red, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func fixture(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	store := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
	store.SetRequirePersistence(true)
	p := &Provider{media: store, http: server.Client()}
	p.authorize = func(_ context.Context, session tools.SessionContext) error {
		if session.BotID != "synthetic-bot" || session.UserID != "synthetic-user" || session.SessionID != "synthetic-session" {
			return errors.New("synthetic access denied")
		}
		return nil
	}
	p.resolve = func(context.Context, string) (selection, bool, error) {
		return selection{config: Config{Protocol: "openai-images", Edits: true}, baseURL: server.URL + "/v1", model: "synthetic-selected-image-model", credentials: func(context.Context) (string, error) { return "", nil }}, true, nil
	}
	return p, server
}

func respondImage(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(data)}}})
}

func call(t *testing.T, p *Provider, session tools.SessionContext, name, args string) sdk.ToolResultPart {
	t.Helper()
	session.UserID, session.SessionID = "synthetic-user", "synthetic-session"
	list, err := p.Tools(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	result, err := toolexec.ExecuteTools(t.Context(), []sdk.ToolCall{{ToolCallID: "synthetic-call", ToolName: name, Input: sdk.ParseToolArguments(args)}}, toolexec.ToolExecOptions{Tools: list})
	if err != nil || len(result.Results) != 1 {
		t.Fatalf("executor: %v %+v", err, result)
	}
	return result.Results[0]
}

func TestExplicitConfig(t *testing.T) {
	for _, tc := range []struct {
		name, raw        string
		enabled, invalid bool
	}{
		{"absent", `{}`, false, false},
		{"omit", `{"protocol":"openai-images","model_mode":"omit"}`, true, true},
		{"configured", `{"protocol":"openai-images","model_mode":"configured"}`, true, false},
		{"null", `null`, true, true},
		{"unknown_protocol", `{"protocol":"magpie","model_mode":"omit"}`, true, true},
		{"native_model", `{"protocol":"openai-images"}`, true, false},
		{"unknown_field", `{"protocol":"openai-images","model_mode":"omit","url":"https://example.invalid"}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"base_url": "https://provider.example.invalid/v1"}
			if tc.name != "absent" {
				cfg[ConfigKey] = json.RawMessage(tc.raw)
			}
			data, _ := json.Marshal(cfg)
			_, enabled, err := parseConfig(sqlc.Provider{ClientType: "openai-completions", Config: data})
			if enabled != tc.enabled || (err != nil) != tc.invalid {
				t.Fatalf("enabled=%v err=%v", enabled, err)
			}
		})
	}
}

func TestGenerateBytesAndAttachment(t *testing.T) {
	for _, mode := range []string{"synthetic-selected-image-model", "synthetic-other-image-model"} {
		t.Run(mode, func(t *testing.T) {
			original := pngBytes(t, 32)
			var requests atomic.Int32
			p, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
					t.Error("unexpected protocol")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				_, hasModel := body["model"]
				if !hasModel || body["prompt"] != "synthetic" || body["n"] != float64(1) || body["response_format"] != "b64_json" {
					t.Errorf("wire=%v", body)
				}
				if body["model"] != mode {
					t.Error("configured model lost")
				}
				respondImage(w, original)
			})
			resolve := p.resolve
			p.resolve = func(ctx context.Context, bot string) (selection, bool, error) {
				s, b, e := resolve(ctx, bot)
				s.model = mode
				return s, b, e
			}
			var emitted []tools.ToolStreamEvent
			session := tools.SessionContext{BotID: "synthetic-bot", CurrentPlatform: "local", ReplyTarget: "synthetic-conversation", LiveStream: true, Emitter: func(e tools.ToolStreamEvent) { emitted = append(emitted, e) }}
			result := call(t, p, session, "generate_image", `{"prompt":"synthetic"}`)
			if result.IsError || !strings.Contains(result.Result.String(), digest(original)) || requests.Load() != 1 {
				t.Fatalf("result=%s count=%d", result.Result.String(), requests.Load())
			}
			rc, asset, err := p.media.Open(t.Context(), session.BotID, digest(original))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cleanupErr := rc.Close(); cleanupErr != nil {
					t.Error(cleanupErr)
				}
			}()
			got, _ := io.ReadAll(rc)
			if !bytes.Equal(got, original) || asset.Mime != "image/png" {
				t.Fatal("original bytes or MIME changed")
			}
			if len(emitted) != 1 || emitted[0].Attachments[0].ContentHash != digest(original) {
				t.Fatalf("attachment events=%+v", emitted)
			}
			t.Logf("mode=%s POST=1 original_sha256=%s persisted_bytes=%d", mode, digest(got), len(got))
		})
	}
}

func TestNativeSupervisorAndSubagentImageChain(t *testing.T) {
	for _, subagent := range []bool{false, true} {
		t.Run(fmt.Sprintf("subagent_%v", subagent), func(t *testing.T) {
			generated, edited := pngBytes(t, 80), pngBytes(t, 160)
			var chatCalls, generations, edits atomic.Int32
			p, server := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/chat/completions":
					raw, _ := io.ReadAll(r.Body)
					var req map[string]any
					_ = json.Unmarshal(raw, &req)
					if req["model"] != "synthetic-chat" {
						t.Error("chat model switched")
					}
					definitions, _ := req["tools"].([]any)
					names := map[string]bool{}
					for _, definition := range definitions {
						function := definition.(map[string]any)["function"].(map[string]any)
						names[function["name"].(string)] = true
					}
					if len(names) != 2 || !names["generate_image"] || !names["edit_image"] {
						t.Errorf("image tools not advertised to synthetic model: %v", names)
					}
					n := chatCalls.Add(1)
					message := map[string]any{"role": "assistant", "content": "synthetic chain complete"}
					reason := "stop"
					if n <= 2 {
						name, args := "generate_image", `{"prompt":"synthetic generation"}`
						if n == 2 {
							name = "edit_image"
							args = `{"prompt":"synthetic edit","images":["` + digest(generated) + `"]}`
							if !bytes.Contains(raw, []byte(digest(generated))) {
								t.Error("tool reference not returned to model")
							}
						}
						message["content"] = nil
						message["tool_calls"] = []any{map[string]any{"id": fmt.Sprintf("call-%d", n), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}
						reason = "tool_calls"
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "synthetic-completion", "object": "chat.completion", "model": "synthetic-chat", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": reason}}})
				case "/v1/images/generations":
					generations.Add(1)
					respondImage(w, generated)
				case "/v1/images/edits":
					edits.Add(1)
					var body struct {
						Model  string `json:"model"`
						Prompt string `json:"prompt"`
						N      int    `json:"n"`
						Format string `json:"response_format"`
						Images []struct {
							URL string `json:"image_url"`
						} `json:"images"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					if r.Header.Get("Content-Type") != "application/json" || body.Model != "synthetic-selected-image-model" || body.Prompt != "synthetic edit" || body.N != 1 || body.Format != "b64_json" || len(body.Images) != 1 || body.Images[0].URL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(generated) {
						t.Error("SDK reference contract changed")
					}
					respondImage(w, edited)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			a := native.New(native.Deps{})
			a.SetToolProviders([]tools.ToolProvider{p})
			model := models.NewSDKChatModel(models.SDKModelConfig{ModelID: "synthetic-chat", ClientType: "openai-completions", BaseURL: server.URL + "/v1"})
			// Production admission derives SupportsToolCall from model capability;
			// the fixture must explicitly declare its synthetic model supports it.
			sessionType := "chat"
			if subagent {
				sessionType = "subagent"
			}
			result, err := a.Generate(t.Context(), native.RunConfig{Model: model, SupportsToolCall: true, SessionType: sessionType, Messages: []sdk.Message{sdk.UserMessage("Synthetic image capability acceptance")}, Identity: native.SessionContext{BotID: "synthetic-bot", UserID: "synthetic-user", SessionID: "synthetic-session", CurrentPlatform: "local", ReplyTarget: "synthetic-conversation", IsSubagent: subagent}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Text != "synthetic chain complete" || chatCalls.Load() != 3 || generations.Load() != 1 || edits.Load() != 1 {
				t.Fatalf("result=%+v calls=%d/%d/%d", result, chatCalls.Load(), generations.Load(), edits.Load())
			}
			rc, _, err := p.media.Open(t.Context(), "synthetic-bot", digest(edited))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cleanupErr := rc.Close(); cleanupErr != nil {
					t.Error(cleanupErr)
				}
			}()
			data, _ := io.ReadAll(rc)
			if !bytes.Equal(data, edited) {
				t.Fatal("edited original changed")
			}
			t.Logf("real native loop + synthetic HTTP: chat=3 generation=1 edit=1; source=%s output=%s", digest(generated), digest(data))
		})
	}
}

func TestUnknownNeverAutomaticallyRetries(t *testing.T) {
	for _, mode := range []string{"201", "429", "500", "redirect", "disconnect", "invalid_base64", "url_only"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			p, _ := fixture(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				switch mode {
				case "201":
					w.WriteHeader(http.StatusCreated)
					respondImage(w, pngBytes(t, 4))
				case "429":
					w.WriteHeader(http.StatusTooManyRequests)
				case "500":
					w.WriteHeader(http.StatusInternalServerError)
				case "redirect":
					w.Header().Set("Location", "/v1/images/generations")
					w.WriteHeader(http.StatusTemporaryRedirect)
				case "disconnect":
					conn, _, _ := w.(http.Hijacker).Hijack()
					_ = conn.Close()
				case "invalid_base64":
					_, _ = w.Write([]byte(`{"data":[{"b64_json":"PRIVATE_DIAGNOSTIC"}]}`))
				case "url_only":
					_, _ = w.Write([]byte(`{"data":[{"url":"https://example.invalid/private"}]}`))
				}
			})
			result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "generate_image", `{"prompt":"synthetic"}`)
			text := result.Result.String()
			if result.IsError || calls.Load() != 1 || !strings.Contains(text, `"state":"unknown"`) || !strings.Contains(text, `"retryable":false`) || strings.Contains(text, "PRIVATE") || strings.Contains(text, "example.invalid") {
				t.Fatalf("calls=%d result=%s", calls.Load(), text)
			}
		})
	}
}

func TestInvalidInputsNeverDispatch(t *testing.T) {
	for _, args := range []string{`null`, `{"prompt":"synthetic","model":"override"}`, `{"prompt":""}`, `{"prompt":"synthetic","mode":"edit"}`} {
		t.Run(args, func(t *testing.T) {
			var calls atomic.Int32
			p, _ := fixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
			if !call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "generate_image", args).IsError || calls.Load() != 0 {
				t.Fatal("invalid arguments dispatched")
			}
		})
	}
	p, _ := fixture(t, func(http.ResponseWriter, *http.Request) { t.Error("foreign attachment dispatched") })
	asset, err := p.media.Ingest(t.Context(), media.IngestInput{BotID: "other-bot", Mime: "image/png", Reader: bytes.NewReader(pngBytes(t, 50))})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{asset.ContentHash, "/data/image.png", "https://example.invalid/image.png"} {
		args, _ := json.Marshal(map[string]any{"prompt": "synthetic", "images": []string{ref}})
		if !call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "edit_image", string(args)).IsError {
			t.Fatal("foreign/path/URL input accepted")
		}
	}
}

type failedStorage struct{ storage.Provider }

func (failedStorage) Put(context.Context, string, io.Reader) error {
	return errors.New("PRIVATE_STORAGE")
}

func TestStorageFailureDoesNotRegenerate(t *testing.T) {
	var calls atomic.Int32
	data := pngBytes(t, 70)
	p, _ := fixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); respondImage(w, data) })
	p.media = media.NewService(nil, failedStorage{localfs.New(t.TempDir())})
	p.media.SetRequirePersistence(true)
	result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "generate_image", `{"prompt":"synthetic"}`)
	if result.IsError || calls.Load() != 1 || !strings.Contains(result.Result.String(), `"storage_state":"unconfirmed"`) || strings.Contains(result.Result.String(), "PRIVATE") {
		t.Fatal(result.Result.String())
	}
}

// Model-controlled text can contain URLs, but only server-managed provider
// configuration chooses the network destination. Exercise the real HTTP path.
func TestModelContentDoesNotSelectDestination(t *testing.T) {
	var destinationCalls, otherCalls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		otherCalls.Add(1)
	}))
	t.Cleanup(other.Close)
	prompt := "Use " + other.URL + "/private?token=synthetic-not-a-credential"
	p, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" || r.URL.RawQuery != "" {
			t.Error("model content changed request routing")
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if r.ContentLength != int64(len(data)) {
			t.Error("request body length changed")
		}
		var body request
		if err := json.Unmarshal(data, &body); err != nil || body.Prompt != prompt {
			t.Error("model content was not preserved as request data")
		}
		respondImage(w, pngBytes(t, 23))
	})
	args, err := json.Marshal(request{Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "generate_image", string(args))
	if result.IsError || !strings.Contains(result.Result.String(), `"state":"completed"`) || destinationCalls.Load() != 1 || otherCalls.Load() != 0 {
		t.Fatal("configured destination isolation failed", result.Result.String())
	}
}
