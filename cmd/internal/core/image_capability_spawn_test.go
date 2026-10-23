package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/agent/background"
	"github.com/felinics/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/runtime/session/ledger"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/imagecapability"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
	sessiontest "github.com/felinics/memoh/internal/testutil/sessionruntime"
)

func imageSpawnReply(w http.ResponseWriter, streaming bool, tool string, args map[string]any, text string) {
	delta := map[string]any{"role": "assistant", "content": text}
	reason := "stop"
	if tool != "" {
		raw, _ := json.Marshal(args)
		delta["content"] = nil
		delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "synthetic-" + tool, "type": "function", "function": map[string]any{"name": tool, "arguments": string(raw)}}}
		reason = "tool_calls"
	}
	if !streaming {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "synthetic-completion", "object": "chat.completion", "model": "synthetic-model", "choices": []any{map[string]any{"index": 0, "message": delta, "finish_reason": reason}}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, choice := range []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": reason}} {
		raw, _ := json.Marshal(map[string]any{"id": "synthetic-chunk", "object": "chat.completion.chunk", "model": "synthetic-model", "choices": []any{choice}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func imageSpawnLastTool(req map[string]any) string {
	text := ""
	for _, item := range req["messages"].([]any) {
		m := item.(map[string]any)
		if m["role"] == "tool" {
			text, _ = m["content"].(string)
		}
	}
	return text
}

// Exercise the actual spawn tool and application admission, not a hand-written
// child RunConfig. Storage ports are synthetic; this is not a PostgreSQL gate.
func TestImageCapabilityActualSpawnAndParentSend(t *testing.T) {
	for _, mode := range []string{"success", "revoked", "foreign_reference", "missing_reference", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			var original bytes.Buffer
			if err := png.Encode(&original, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(original.Bytes())
			hash := hex.EncodeToString(digest[:])
			storage := &imageSpawnStorage{Originals: localfs.NewOriginals(t.TempDir())}
			store := media.NewService(nil, storage)
			store.SetRequirePersistence(true)
			foreign, err := store.Ingest(ctx, media.IngestInput{BotID: imageSpawnOtherBot, Mime: "image/png", Reader: bytes.NewReader(original.Bytes())})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "revoked" {
				if _, err := store.Ingest(ctx, media.IngestInput{BotID: imageSpawnBot, Mime: "image/png", Reader: bytes.NewReader(original.Bytes())}); err != nil {
					t.Fatal(err)
				}
			}
			storage.opens.Store(0)
			q := &imageSpawnQueries{sessions: map[string]sqlc.BotSession{}, configs: map[string]sqlc.SubagentConfig{}, authorizedChild: map[string]string{}}
			q.allowed.Store(true)
			var parentCalls, childCalls, imagePosts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/images/generations" {
					imagePosts.Add(1)
					if mode == "unknown" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(original.Bytes())}}})
					return
				}
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("unexpected synthetic endpoint %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				var req map[string]any
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				if req["model"] != "synthetic-model" {
					t.Error("chat model changed")
				}
				streaming, _ := req["stream"].(bool)
				names := map[string]bool{}
				for _, entry := range req["tools"].([]any) {
					f := entry.(map[string]any)["function"].(map[string]any)
					names[f["name"].(string)] = true
				}
				if streaming {
					if names["spawn_agent"] || names["send"] || !names["generate_image"] || !names["edit_image"] {
						t.Error("spawned tool policy or image visibility changed")
					}
					n := childCalls.Add(1)
					if n == 1 {
						if mode == "revoked" {
							q.allowed.Store(false)
						}
						name, args := "generate_image", map[string]any{"prompt": "synthetic child image"}
						if mode == "foreign_reference" || mode == "missing_reference" || mode == "revoked" {
							ref := foreign.ContentHash
							if mode == "missing_reference" {
								ref = strings.Repeat("a", 64)
							}
							name = "edit_image"
							args = map[string]any{"prompt": "synthetic edit", "images": []string{ref}}
						}
						imageSpawnReply(w, true, name, args, "")
						return
					}
					if n != 2 {
						t.Error("child unexpectedly retried its turn")
					}
					output := imageSpawnLastTool(req)
					if mode == "success" {
						if !strings.Contains(output, hash) || !strings.Contains(output, "completed") {
							t.Errorf("child has no persisted reference: %s", output)
						}
						imageSpawnReply(w, true, "", nil, "synthetic child reference "+hash)
						return
					}
					if strings.Contains(output, `"state":"completed"`) {
						t.Error("failure became image success")
					}
					if mode == "unknown" && !strings.Contains(output, `"state":"unknown"`) {
						t.Errorf("uncertainty lost: %s", output)
					}
					if mode == "unknown" && (!strings.Contains(output, `"retryable":false`) || strings.Contains(output, `"content_hash"`)) {
						t.Error("unknown result became retryable or gained a ready reference")
					}
					if mode == "revoked" && !strings.Contains(output, "not authorized for this conversation") {
						t.Errorf("current permission revocation not enforced: %s", output)
					}
					if (mode == "foreign_reference" || mode == "missing_reference") && !strings.Contains(output, "input image reference is unavailable for this Bot") {
						t.Errorf("invalid reference not rejected in current Bot scope: %s", output)
					}
					imageSpawnReply(w, true, "", nil, "synthetic child has no ready image: "+mode)
					return
				}
				if !names["spawn_agent"] || !names["send"] {
					t.Error("supervisor admission/send tools missing")
				}
				n := parentCalls.Add(1)
				if n == 1 {
					imageSpawnReply(w, false, "spawn_agent", map[string]any{"id": "image-worker", "task": "synthetic image task", "run_in_background": false}, "")
					return
				}
				if n == 2 {
					output := imageSpawnLastTool(req)
					if mode == "success" {
						if !strings.Contains(output, "synthetic child reference "+hash) {
							t.Errorf("child report missing from supervisor: %s", output)
						}
						imageSpawnReply(w, false, "send", map[string]any{"attachments": []any{map[string]any{"content_hash": hash, "type": "image"}}}, "")
						return
					}
					if !strings.Contains(output, "synthetic child has no ready image: "+mode) {
						t.Errorf("child failure/unknown report lost: %s", output)
					}
				}
				imageSpawnReply(w, false, "", nil, "synthetic supervisor finished")
			}))
			t.Cleanup(server.Close)
			q.model = sqlc.Model{ID: imageSpawnUUID(imageSpawnModel), ProviderID: imageSpawnUUID(imageSpawnProvider), ModelID: "synthetic-model", Type: "chat", Enable: true, Config: []byte(`{"compatibilities":["tool-call","image-output"]}`)}
			cfg, _ := json.Marshal(map[string]any{"base_url": server.URL + "/v1", imagecapability.ConfigKey: imagecapability.Config{Protocol: "openai-images", ModelMode: "omit", Edits: true}})
			q.provider = sqlc.Provider{ID: imageSpawnUUID(imageSpawnProvider), Name: "synthetic-provider", ClientType: "openai-completions", Enable: true, Config: cfg}
			q.sessions[imageSpawnParent] = sqlc.BotSession{ID: imageSpawnUUID(imageSpawnParent), BotID: imageSpawnUUID(imageSpawnBot), Type: "chat", SessionMode: "chat", RuntimeType: "model", Metadata: []byte(`{}`), RuntimeMetadata: []byte(`{}`)}
			log := slog.New(slog.DiscardHandler)
			settingsSvc, modelsSvc := settings.NewService(log, q, nil, nil), models.NewService(log, q)
			sessions := thread.NewService(log, q, nil)
			accountSvc := accounts.NewService(log, imageSpawnAccounts{})
			permissions := &applicationBotPermissionChecker{bots: bots.NewService(log, q), accounts: accountSvc}
			images := imagecapability.New(tools.NewImageGenProvider(log, settingsSvc, modelsSvc, q, store), settingsSvc, modelsSvc, q, store, imageCapabilityAuthorizer(permissions, sessions.Get))
			messages := &imageSpawnMessages{}
			a := native.New(native.Deps{})
			service := application.NewService(log, modelsSvc, nil, messages, settingsSvc, accountSvc, a, time.UTC, time.Minute)
			service.SetSessionService(sessions)
			runs := sessionledger.New()
			manager := sessiontest.New(sessionruntime.NewMemoryBackend(), sessionruntime.Options{Ledger: runs, Logger: log})
			t.Cleanup(func() { _ = manager.Close() })
			service.SetSessionRuntime(manager)
			bg := background.New(log)
			service.SetBackgroundManager(bg)
			spawn := tools.NewSpawnProvider(log, settingsSvc, modelsSvc, q, sessions, bg)
			adapter := native.NewSpawnAdapter(a)
			adapter.SetStepCommitFactory(service.SubagentStepCommit)
			adapter.SetRunObserverFactory(service.SubagentRunObserver)
			adapter.SetFailureTranslator(service.SubagentFailure)
			spawn.SetAgent(adapter)
			spawn.SetSubagentAdmitter(service)
			spawn.SetMessageService(messages)
			spawn.SetSystemPromptFunc(native.SpawnSystemPrompt)
			sender := &imageSpawnSender{media: store}
			a.SetToolProviders(delegationScopedTools([]tools.ToolProvider{spawn, images, tools.NewMessageProvider(log, sender, nil, sender, store)}))
			model := models.NewSDKChatModel(models.SDKModelConfig{ModelID: "synthetic-model", ClientType: "openai-completions", BaseURL: server.URL + "/v1"})
			result, err := a.Generate(ctx, native.RunConfig{Model: model, CurrentModelUUID: imageSpawnModel, CurrentModelProviderID: imageSpawnProvider, SupportsToolCall: true, SessionType: "chat", Messages: []sdk.Message{sdk.UserMessage("synthetic parent request")}, Identity: native.SessionContext{BotID: imageSpawnBot, UserID: imageSpawnUser, SessionID: imageSpawnParent, CurrentPlatform: "local", ReplyTarget: "synthetic-conversation"}})
			if err != nil || result.Text != "synthetic supervisor finished" {
				t.Fatalf("supervisor failed: %v %+v", err, result)
			}
			if childCalls.Load() != 2 {
				t.Fatalf("child HTTP calls=%d; actual spawn not established", childCalls.Load())
			}
			q.mu.Lock()
			children := len(q.configs)
			authorized := len(q.authorizedChild)
			for childID := range q.configs {
				child := q.sessions[childID]
				if child.Type != "subagent" || child.ParentSessionID.String() != imageSpawnParent || child.BotID.String() != imageSpawnBot || child.CreatedByUserID.String() != imageSpawnUser || q.authorizedChild[childID] == "" {
					t.Error("created/admitted child identity did not reach the real DI authorizer")
				}
			}
			q.mu.Unlock()
			if children != 1 || authorized != 1 {
				t.Fatalf("child creation/current DI authorization counts=%d/%d", children, authorized)
			}
			runs.Mu.Lock()
			admitted, claimed := runs.Admits, runs.Claims
			var childRun ledger.Run
			for _, r := range runs.Runs {
				childRun = *r
			}
			runs.Mu.Unlock()
			if admitted != 1 || claimed != 1 || childRun.SessionID == imageSpawnParent || childRun.State != ledger.StateCompleted {
				t.Fatalf("actual admission/terminal mismatch: admits=%d claims=%d run=%+v", admitted, claimed, childRun)
			}
			messages.mu.Lock()
			steps := messages.steps
			messages.mu.Unlock()
			if steps < 2 {
				t.Fatalf("child steps not committed through application factory: %d", steps)
			}
			wantPosts := int32(0)
			if mode == "success" || mode == "unknown" {
				wantPosts = 1
			}
			if imagePosts.Load() != wantPosts {
				t.Fatalf("image POST count=%d want=%d", imagePosts.Load(), wantPosts)
			}
			if mode == "revoked" && storage.opens.Load() != 0 {
				t.Fatal("revoked child opened an existing current-Bot image")
			}
			if mode == "success" {
				if sender.calls.Load() != 1 || sender.hash != hash || !bytes.Equal(sender.bytes, original.Bytes()) || parentCalls.Load() != 3 {
					t.Fatal("supervisor send changed or lost original reference/bytes")
				}
			} else if sender.calls.Load() != 0 || parentCalls.Load() != 2 {
				t.Fatal("unconfirmed child result sent an attachment or retried")
			}
			t.Logf("actual spawn admission=1 child_session=1 child_chat=%d image_POST=%d parent_chat=%d send=%d steps=%d mode=%s original_sha256=%s", childCalls.Load(), imagePosts.Load(), parentCalls.Load(), sender.calls.Load(), steps, mode, hash)
		})
	}
}
