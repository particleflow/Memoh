package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agent/runtime/native"
	chatview "github.com/felinics/memoh/internal/agent/view"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/reasoning"
)

type presenceChatPermission struct{}

func (presenceChatPermission) HasBotPermission(_ context.Context, _, _, permission string) (bool, error) {
	return permission == bots.PermissionChat, nil
}

func TestConversationPresenceRuntimeSameModelParallelAndFormalFirst(t *testing.T) {
	for _, formalFirst := range []bool{false, true} {
		t.Run(fmt.Sprint("formal_first=", formalFirst), func(t *testing.T) {
			claims, _, pool := presencePostgresFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			releaseMain := make(chan struct{})
			releaseAux := make(chan struct{})
			var onceMain, onceAux sync.Once
			defer onceMain.Do(func() { close(releaseMain) })
			defer onceAux.Do(func() { close(releaseAux) })
			auxiliaryStarted := make(chan struct{})
			captured := make(chan map[string]any, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Error(e)
					return
				}
				captured <- body
				if body["stream"] == true {
					select {
					case <-releaseMain:
					case <-r.Context().Done():
						return
					}
					fmt.Fprint(w, "formal response")
					return
				}
				close(auxiliaryStarted)
				select {
				case <-releaseAux:
				case <-r.Context().Done():
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","model":"same-fixture-model","choices":[{"index":0,"message":{"role":"assistant","content":"{\"text\":\"收到，我来梳理。\",\"phase\":\"thinking\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}`)
			}))
			defer server.Close()
			service := &Service{queries: claims.queries, botPermissions: presenceChatPermission{}, logger: slog.New(slog.DiscardHandler)}
			service.SetConversationPresence(ConversationPresenceOptions{Policy: conversationpresence.Policy{Continuity: conversationpresence.ContinuityPolicy{}, Limits: conversationpresence.Limits{Deadline: 4 * time.Second, MaxInputRunes: 100, MaxOutputTokens: 100, MaxTextRunes: 100}}, MaxConcurrentPerProvider: 1})
			req := ChatRequest{BotID: claims.handle.BotID, ThreadID: claims.handle.SessionID, RunHandle: claims.handle, UserID: claims.actor, CurrentChannel: "local", RawQuery: "Can you explain this?"}
			selected := models.GetResponse{ID: "fixture-model-uuid", ModelID: "same-fixture-model", Model: models.Model{ProviderID: "fixture-provider", ModelID: "same-fixture-model", Config: models.ModelConfig{ThinkingMode: reasoning.ModeToggle, ReasoningEfforts: []string{"low", "high"}, ReasoningOffSupport: reasoning.OffSupportRejected}}}
			provider := sqlc.Provider{ClientType: string(models.ClientTypeOpenAICompletions)}
			mainCfg := models.SDKModelConfig{ModelID: selected.ModelID, ClientType: provider.ClientType, BaseURL: server.URL + "/v1", APIKey: "fixture-only", HTTPClient: server.Client(), ReasoningConfig: &models.ReasoningConfig{Active: true, Effort: "high"}}
			var snapshot *presenceResolvedModel
			hooked := service.presenceModelHook(req, &snapshot)(selected, provider, mainCfg)
			main := make(chan native.StreamEvent, 2)
			out := service.withConversationPresence(ctx, req, resolvedContext{presence: snapshot, runConfig: native.RunConfig{Bot: native.BotInfo{Name: "Fixture"}}}, main)
			mainRequestDone := make(chan error, 1)
			go func() {
				r, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"same-fixture-model","reasoning_effort":"high","stream":true}`))
				response, e := hooked.HTTPClient.Do(r)
				if e == nil {
					_, e = io.ReadAll(response.Body)
					_ = response.Body.Close()
				}
				mainRequestDone <- e
				if e == nil {
					main <- native.StreamEvent{Type: native.EventTextDelta, Delta: "Formal answer."}
					main <- native.StreamEvent{Type: native.EventAgentEnd}
				}
				close(main)
			}()
			// Reaching auxiliary HTTP while main is blocked proves simultaneous work,
			// and the WroteRequest hook ensures the main request took priority.
			select {
			case <-auxiliaryStarted:
			case <-ctx.Done():
				t.Fatal("auxiliary never ran alongside main")
			}
			converter := chatview.NewUIMessageStreamConverter()
			ackCount := 0
			var sequence []string
			if formalFirst {
				onceMain.Do(func() { close(releaseMain) })
			} else {
				onceAux.Do(func() { close(releaseAux) })
			}
			for event := range out {
				if isConversationPresenceEvent(event) {
					ackCount++
					sequence = append(sequence, "ack")
					ui := converter.HandleEvent(chatview.UIMessageStreamEvent{Type: string(event.Type), Progress: event.Progress})
					if len(ui) != 1 || ui[0].Type != chatview.UIMessageStatus || ui[0].Args["text"] == "" {
						t.Fatalf("typed status missing: %+v", ui)
					}
					onceMain.Do(func() { close(releaseMain) })
				}
				if event.Type == native.EventTextDelta {
					sequence = append(sequence, "formal")
					if event.Delta != "Formal answer." {
						t.Fatal(event)
					}
					onceAux.Do(func() { close(releaseAux) })
				}
			}
			if e := <-mainRequestDone; e != nil {
				t.Fatal(e)
			}
			wantAck := 1
			if formalFirst {
				wantAck = 0
			}
			if ackCount != wantAck {
				t.Fatalf("ack count=%d want=%d events=%v", ackCount, wantAck, sequence)
			}
			first, second := <-captured, <-captured
			if first["model"] != second["model"] || first["reasoning_effort"] != "high" || second["reasoning_effort"] != "low" {
				t.Fatalf("selection/effort: %v %v", first, second)
			}
			if mainCfg.ReasoningConfig.Effort != "high" {
				t.Fatal("main preference changed")
			}
			var history int
			if e := pool.QueryRow(ctx, `SELECT count(*) FROM bot_history_messages WHERE session_id=$1`, claims.handle.SessionID).Scan(&history); e != nil || history != 0 {
				t.Fatalf("history polluted: %d %v", history, e)
			}
		})
	}
}
