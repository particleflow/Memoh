package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/reasoning"
)

func TestPresenceSDKUsesSameModelOffThenLowWithoutPreferenceWrites(t *testing.T) {
	for _, tc := range []struct {
		name   string
		levels []string
		off    string
		want   string
	}{
		{"off", []string{"disable", "low", "high"}, reasoning.OffSupportAccepted, "none"},
		{"explicit off without legacy sentinel", []string{"low", "high"}, reasoning.OffSupportAccepted, "none"},
		{"low", []string{"low", "high", "max"}, reasoning.OffSupportRejected, "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				bodies <- body
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","model":"catalog-model","choices":[{"index":0,"message":{"role":"assistant","content":"{\"text\":\"收到，我来梳理。\",\"phase\":\"accepted\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}`)
			}))
			defer server.Close()
			model := models.GetResponse{
				ID: "selected-model", ModelID: "catalog-model",
				Model: models.Model{ProviderID: "selected-provider", ModelID: "catalog-model", Config: models.ModelConfig{ThinkingMode: reasoning.ModeToggle, ReasoningEfforts: tc.levels, ReasoningOffSupport: tc.off}},
			}
			main := models.SDKModelConfig{ModelID: model.ModelID, ClientType: string(models.ClientTypeOpenAICompletions), APIKey: "fixture-only", BaseURL: server.URL + "/v1", HTTPClient: server.Client(), ReasoningConfig: &models.ReasoningConfig{Active: true, Effort: "high"}}
			identity := conversationpresence.Identity{ModelUUID: model.ID, ModelID: model.ModelID, ProviderID: model.ProviderID, ClientType: main.ClientType}
			generator, choice, err := prepareConversationPresenceSDK(identity, model, main, true)
			if err != nil || generator == nil {
				t.Fatalf("prepare: %+v %v", choice, err)
			}
			result, err := generator.Generate(context.Background(), conversationpresence.Input{Identity: identity, Choice: choice, Phase: conversationpresence.Accepted, Text: "user request"}, conversationpresence.Limits{MaxInputRunes: 100, MaxOutputTokens: 80})
			if err != nil || result.Text != "收到，我来梳理。" {
				t.Fatalf("generate: %+v %v", result, err)
			}
			body := <-bodies
			if body["model"] != main.ModelID || body["reasoning_effort"] != tc.want || (body["tool_choice"] != nil && body["tool_choice"] != "none") {
				t.Fatalf("wrong model/effort or tool execution allowed: %+v", body)
			}
			if tools, ok := body["tools"].([]any); ok && len(tools) != 0 {
				t.Fatal("unexpected tools")
			}
			if !main.ReasoningConfig.Active || main.ReasoningConfig.Disabled || main.ReasoningConfig.Effort != "high" {
				t.Fatal("main configuration changed")
			}
			if !result.Usage.Known || result.Usage.InputTokens != 7 {
				t.Fatal("usage lost")
			}
			if choice.Config != nil {
				choice.Config.Effort = "max"
				_, err := generator.Generate(context.Background(), conversationpresence.Input{Identity: identity, Choice: choice, Phase: conversationpresence.Accepted}, conversationpresence.Limits{MaxInputRunes: 100, MaxOutputTokens: 80})
				if err == nil {
					t.Fatal("caller mutation changed frozen choice")
				}
			}
		})
	}
}

func TestPresenceSDKRejectsWrongSelectionAndInvalidOutput(t *testing.T) {
	identity := conversationpresence.Identity{ModelUUID: "model", ModelID: "catalog-model", ProviderID: "provider", ClientType: "openai-completions"}
	choice := conversationpresence.ThinkingChoice{Usable: true, Reason: "already_non_reasoning"}
	for _, tc := range []struct {
		name     string
		response string
		phase    conversationpresence.Phase
	}{
		{"wrong phase", `{"text":"Working now","phase":"thinking"}`, conversationpresence.Accepted},
		{"extra fields", `{"text":"Received","phase":"accepted","reasoning":"private"}`, conversationpresence.Accepted},
		{"trailing object", `{"text":"Received","phase":"accepted"}{}`, conversationpresence.Accepted},
		{"plain answer", `The answer is 42.`, conversationpresence.Accepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generator := &conversationPresenceSDKGenerator{identity: identity, choice: choice, generate: func(context.Context, sdk.Request) (sdk.ModelResult, error) {
				return sdk.ModelResult{Text: tc.response, Reasoning: "must never be displayed", FinishReason: sdk.FinishReasonStop}, nil
			}}
			result, err := generator.Generate(context.Background(), conversationpresence.Input{Identity: identity, Choice: choice, Phase: tc.phase}, conversationpresence.Limits{MaxInputRunes: 100, MaxOutputTokens: 80})
			if err == nil || result.Text != "" || result.Usage.Known {
				t.Fatalf("invalid output escaped: %+v %v", result, err)
			}
		})
	}
	generator := &conversationPresenceSDKGenerator{identity: identity, choice: choice, generate: func(context.Context, sdk.Request) (sdk.ModelResult, error) {
		t.Fatal("wrong model called")
		return sdk.ModelResult{}, nil
	}}
	wrong := identity
	wrong.ProviderID = "different-provider"
	if _, err := generator.Generate(context.Background(), conversationpresence.Input{Identity: wrong, Choice: choice, Phase: conversationpresence.Accepted}, conversationpresence.Limits{MaxInputRunes: 100, MaxOutputTokens: 80}); err == nil {
		t.Fatal("different provider accepted")
	}
}
