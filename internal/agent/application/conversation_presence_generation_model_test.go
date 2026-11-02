package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/reasoning"
)

// presenceAckRequestByteCap is the hard composite bound on the acknowledgement
// wire request: system prompt plus slot JSON, in bytes. The slots are already
// clamped piecewise (user ≤300 runes, points ≤200 each/≤600 total, recent acks
// ≤2×160); this cap pins the assembled whole so a future slot cannot silently
// grow the request back toward main-chain sizes. The 2026-11-02 08:05 ledger
// showed the ack is latency-bound by the model's first-token floor, not by its
// own input, which stays ~1.5k tokens — an order of magnitude below the
// 42.7k-token main request recorded the same second.
const presenceAckRequestByteCap = 6144

// TestPresenceAckRequestStaysBounded proves the acknowledgement generation
// input is and stays minimal: the directed system prompt plus the slot JSON
// assembled from maximal inputs stays under presenceAckRequestByteCap, and the
// trim keeps the fields acknowledgement quality depends on — bot identity
// (bot_name) and language following (locale) plus the user message.
func TestPresenceAckRequestStaysBounded(t *testing.T) {
	long := strings.Repeat("字", 400)
	acks := []string{strings.Repeat("在", 400), strings.Repeat("好", 400)}
	slots := buildPresenceSlots("红玉", "zh", long, []string{long, long, long, long}, conversationpresence.Thinking, true, 3, "可能要一会儿", acks)
	if got := len([]rune(slots.UserMessage)); got != presenceSlotUserMaxRunes {
		t.Fatalf("user_message runes=%d want the %d cap", got, presenceSlotUserMaxRunes)
	}
	totalPoints := 0
	for _, point := range slots.ContextPoints {
		totalPoints += len([]rune(point))
	}
	if totalPoints > presenceSlotPointsTotal {
		t.Fatalf("context_points runes=%d exceeds the %d total cap", totalPoints, presenceSlotPointsTotal)
	}
	for _, ack := range slots.RecentACKTexts {
		if got := len([]rune(ack)); got > presenceSlotACKMaxRunes {
			t.Fatalf("recent ack runes=%d exceeds the %d cap", got, presenceSlotACKMaxRunes)
		}
	}
	payload, err := json.Marshal(slots)
	if err != nil {
		t.Fatal(err)
	}
	system := presenceSystemPrompt("clarify")
	if total := len(system) + len(payload); total > presenceAckRequestByteCap {
		t.Fatalf("assembled ack request=%d bytes exceeds the %d cap (system=%d payload=%d)", total, presenceAckRequestByteCap, len(system), len(payload))
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["bot_name"] != "红玉" {
		t.Fatalf("bot_name lost from the trimmed payload: %s", payload)
	}
	if decoded["locale"] != "zh" {
		t.Fatalf("locale lost from the trimmed payload: %s", payload)
	}
	if text, _ := decoded["user_message"].(string); strings.TrimSpace(text) == "" {
		t.Fatalf("user_message lost from the trimmed payload: %s", payload)
	}
}

// fixtureSlowFastModelServer is one OpenAI-completions provider serving two
// models: slow-model never answers within the simulated window (the measured
// GLM-5.3 low first-token floor, 4.8-6.8s and 9.17s under main-chain
// contention at 2026-11-02 08:05), fast-model answers immediately.
func fixtureSlowFastModelServer(t *testing.T, slowDelay time.Duration) (*httptest.Server, *sync.Map) {
	t.Helper()
	var seen sync.Map
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 1<<16)
		n, _ := r.Body.Read(body)
		var request struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body[:n], &request)
		seen.Store("model:"+request.Model, true)
		if request.Model == "fast-model" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","model":"fast-model","choices":[{"index":0,"message":{"role":"assistant","content":"{\"text\":\"你说的整理我看到了，我先确认下范围再动手。\",\"phase\":\"thinking\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":9,"total_tokens":40}}`)
			return
		}
		timer := time.NewTimer(slowDelay)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","model":"slow-model","choices":[{"index":0,"message":{"role":"assistant","content":"{\"text\":\"慢\",\"phase\":\"thinking\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":1,"total_tokens":32}}`)
		}
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func fixturePresenceResolvedModel(server *httptest.Server) *presenceResolvedModel {
	selected := models.GetResponse{ID: "slow-model-uuid", ModelID: "slow-model", Model: models.Model{ProviderID: "e1-generation-provider", ModelID: "slow-model", Config: models.ModelConfig{ThinkingMode: reasoning.ModeNone}}}
	return &presenceResolvedModel{
		model: selected,
		sdk: models.SDKModelConfig{
			ModelID: "slow-model", ClientType: string(models.ClientTypeOpenAICompletions),
			BaseURL: server.URL + "/v1", APIKey: "fixture-only", HTTPClient: server.Client(),
		},
		mainWritten: func() chan struct{} { ch := make(chan struct{}); close(ch); return ch }(),
	}
}

// TestPresenceGenerationModelOverrideLandsSlowModelInWindow reproduces the
// E1-residual shape and the fix. Red half: the main selection is a model whose
// first token arrives after the remaining window (08:05: 9.17s generation vs
// 7.17s window remainder) — the directed generation dies on its window
// deadline. Green half: [agent.conversation_presence].generation_model points
// the sidecar at a fast model inside the same provider; the same window now
// carries the acknowledgement to completion.
func TestPresenceGenerationModelOverrideLandsSlowModelInWindow(t *testing.T) {
	server, seen := fixtureSlowFastModelServer(t, 6500*time.Millisecond)
	main := fixturePresenceResolvedModel(server)
	service := &Service{}

	// Red half: today's selection (the main model) cannot land in the window.
	// No override is configured — this is the production shape at 08:05.
	redRuntime := &conversationPresenceRuntime{options: ConversationPresenceOptions{}}
	if selection := service.resolvePresenceGeneration(context.Background(), redRuntime, main); selection.override {
		t.Fatal("an empty generation_model must keep the main selection")
	}
	redDetail := presenceDecisionDetail{}
	redGenerator, redChoice, err := prepareConversationPresenceSDK(
		conversationpresence.Identity{ModelUUID: main.model.ID, ModelID: main.model.ModelID, ProviderID: main.model.ProviderID, ClientType: string(models.ClientTypeOpenAICompletions), RuntimeType: "model"},
		main.model, main.sdk, true)
	if err != nil || redGenerator == nil {
		t.Fatalf("main generator: err=%v choice=%+v", err, redChoice)
	}
	window := 4 * time.Second
	start := time.Now()
	redCtx, cancelRed := context.WithTimeout(context.Background(), window)
	defer cancelRed()
	directed := &presenceDirectedGenerator{
		base: redGenerator, direction: "clarify", windowEnd: start.Add(window),
		botName: "红玉", locale: "zh", userMessage: "上次那个整理任务有结论了吗", urgency: 2,
	}
	timed := presenceTimedGenerator{detail: &redDetail, base: directed}
	if _, err := timed.Generate(redCtx, conversationpresence.Input{Phase: conversationpresence.Thinking, Identity: conversationpresence.Identity{ModelUUID: main.model.ID, ModelID: main.model.ModelID, ProviderID: main.model.ProviderID, ClientType: string(models.ClientTypeOpenAICompletions), RuntimeType: "model"}, Choice: redChoice}, conversationpresence.Limits{Deadline: window, MaxInputRunes: 1200, MaxOutputTokens: 100, MaxTextRunes: 160}); err == nil {
		t.Fatalf("slow main model inside a %s window must die on the deadline", window)
	}
	if redDetail.Timings.GenerateMs < int64(window/time.Millisecond)-500 {
		t.Fatalf("generate_ms=%d did not record the full slow attempt", redDetail.Timings.GenerateMs)
	}
	if _, ok := seen.Load("model:slow-model"); !ok {
		t.Fatal("red half never reached the slow model endpoint")
	}

	// Green half: the override resolves within the same provider and lands.
	greenRuntime := &conversationPresenceRuntime{
		options: ConversationPresenceOptions{
			GenerationModel: "fast-model",
			GenerationModelResolver: func(context.Context, string) (models.GetResponse, error) {
				return models.GetResponse{ID: "fast-model-uuid", ModelID: "fast-model", Model: models.Model{ProviderID: "e1-generation-provider", ModelID: "fast-model", Config: models.ModelConfig{ThinkingMode: reasoning.ModeNone}}}, nil
			},
		},
	}
	green := service.resolvePresenceGeneration(context.Background(), greenRuntime, main)
	if !green.override || green.model.ModelID != "fast-model" {
		t.Fatalf("override not applied: override=%v model=%+v", green.override, green.model.Model)
	}
	if green.sdk.ModelID != "fast-model" || green.sdk.BaseURL != main.sdk.BaseURL || green.sdk.APIKey != main.sdk.APIKey {
		t.Fatalf("override must reuse the main provider transport, got sdk=%+v", green.sdk)
	}
	identity := conversationpresence.Identity{ModelUUID: green.model.ID, ModelID: green.model.ModelID, ProviderID: green.model.ProviderID, ClientType: string(models.ClientTypeOpenAICompletions), RuntimeType: "model"}
	generator, choice, err := prepareConversationPresenceSDK(identity, green.model, green.sdk, true)
	if err != nil || generator == nil {
		t.Fatalf("override generator: err=%v choice=%+v", err, choice)
	}
	greenDetail := presenceDecisionDetail{}
	start = time.Now()
	greenCtx, cancelGreen := context.WithTimeout(context.Background(), window)
	defer cancelGreen()
	directed = &presenceDirectedGenerator{
		base: generator, direction: "clarify", windowEnd: start.Add(window),
		botName: "红玉", locale: "zh", userMessage: "上次那个整理任务有结论了吗", urgency: 2,
	}
	timed = presenceTimedGenerator{detail: &greenDetail, base: directed}
	result, err := timed.Generate(greenCtx, conversationpresence.Input{Phase: conversationpresence.Thinking, Identity: identity, Choice: choice}, conversationpresence.Limits{Deadline: window, MaxInputRunes: 1200, MaxOutputTokens: 100, MaxTextRunes: 160})
	if err != nil {
		t.Fatalf("fast override model must land inside the %s window: %v", window, err)
	}
	if result.Text == "" || result.Phase != conversationpresence.Thinking {
		t.Fatalf("override result incomplete: %+v", result)
	}
	if greenDetail.Timings.GenerateMs >= int64(window/time.Millisecond) {
		t.Fatalf("override generation generate_ms=%d did not beat the window", greenDetail.Timings.GenerateMs)
	}
	if _, ok := seen.Load("model:fast-model"); !ok {
		t.Fatal("green half never reached the fast model endpoint")
	}
}

// TestResolvePresenceGenerationFallbacks keeps the override honest: an empty
// ref, a resolver error and a cross-provider model all keep the exact main
// selection, and a ref equal to the main model is not an override.
func TestResolvePresenceGenerationFallbacks(t *testing.T) {
	server, _ := fixtureSlowFastModelServer(t, time.Millisecond)
	main := fixturePresenceResolvedModel(server)
	base := ConversationPresenceOptions{}
	fastModel := func() models.GetResponse {
		return models.GetResponse{ID: "fast-model-uuid", ModelID: "fast-model", Model: models.Model{ProviderID: "e1-generation-provider", ModelID: "fast-model", Config: models.ModelConfig{ThinkingMode: reasoning.ModeNone}}}
	}
	cases := []struct {
		name     string
		options  ConversationPresenceOptions
		resolver func(context.Context, string) (models.GetResponse, error)
		override bool
	}{
		{"empty_ref", base, nil, false},
		{"nil_resolver", ConversationPresenceOptions{GenerationModel: "fast-model"}, nil, false},
		{"same_as_main", ConversationPresenceOptions{GenerationModel: "slow-model"}, func(context.Context, string) (models.GetResponse, error) { return fastModel(), nil }, false},
		{"resolver_error", ConversationPresenceOptions{GenerationModel: "fast-model"}, func(context.Context, string) (models.GetResponse, error) {
			return models.GetResponse{}, errors.New("catalog down")
		}, false},
		{"provider_mismatch", ConversationPresenceOptions{GenerationModel: "fast-model"}, func(context.Context, string) (models.GetResponse, error) {
			model := fastModel()
			model.ProviderID = "other-provider"
			model.Model.ProviderID = "other-provider"
			return model, nil
		}, false},
		{"same_provider_override", ConversationPresenceOptions{GenerationModel: "fast-model"}, func(context.Context, string) (models.GetResponse, error) { return fastModel(), nil }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			options := tc.options
			options.GenerationModelResolver = tc.resolver
			runtime := &conversationPresenceRuntime{options: options}
			selection := (&Service{}).resolvePresenceGeneration(context.Background(), runtime, main)
			if selection.override != tc.override {
				t.Fatalf("override=%v want %v", selection.override, tc.override)
			}
			if !tc.override {
				if selection.model.ID != main.model.ID || selection.model.ModelID != main.model.ModelID || selection.sdk.ModelID != main.sdk.ModelID || selection.sdk.BaseURL != main.sdk.BaseURL {
					t.Fatalf("fallback must keep the exact main selection, got model=%+v sdk=%+v", selection.model, selection.sdk)
				}
				return
			}
			if selection.sdk.ModelID != "fast-model" || selection.sdk.BaseURL != main.sdk.BaseURL || selection.sdk.HTTPClient != main.sdk.HTTPClient {
				t.Fatalf("override must swap only the wire model id, got sdk=%+v", selection.sdk)
			}
		})
	}
}
