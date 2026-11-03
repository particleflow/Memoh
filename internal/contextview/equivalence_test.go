package contextview

import (
	"context"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	contextfrag "github.com/felinics/memoh/internal/agent/context/fragment"
)

type legacyInput struct {
	system    string
	messages  []sdk.Message
	query     string
	toolUsage string
}

func TestContextViewMatchesLegacyProviderBytes(t *testing.T) {
	t.Parallel()
	toolUsage := "## Tool usage\nUse tools carefully."
	cases := map[string]legacyInput{
		"basic":                    {system: "You are helpful.", messages: []sdk.Message{sdk.UserMessage("hello"), sdk.AssistantMessage("hi")}, query: "What is 2+2?"},
		"system whitespace":        {system: "  preserve outer system bytes\n", messages: []sdk.Message{sdk.UserMessage("hello")}},
		"tool usage and workspace": {system: "Base.\n\n" + toolUsage + "\n\n## Workspace instruction files\nAGENTS.md", toolUsage: toolUsage, query: "continue"},
		"query whitespace":         {system: "plain", query: "  inspect this \n"},
		"memory bytes":             {system: "system", messages: []sdk.Message{sdk.UserMessage("<memory>raw & unescaped</memory>"), sdk.UserMessage("current")}},
		"hook bytes":               {system: "system\n\n[Hook Context: BeforePromptBuild]\n  exact hook output \n"},
	}
	for name, input := range cases {
		name, input := name, input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertContextViewEquivalent(t, input)
		})
	}
}

func assertContextViewEquivalent(t *testing.T, input legacyInput) {
	t.Helper()
	want := legacyProviderMessages(input.messages, input.query)
	builder := NewBuilder(
		NewMapCollectorRegistry(&SystemPromptCollector{}, &HistoryMessagesCollector{}, &CurrentUserCollector{}),
		&FragmentSelector{}, StablePrefixPlacer{}, NewMapRendererRegistry(&SDKMessagesRenderer{}),
	)
	view, err := builder.Build(context.Background(), BuildInput{
		Intent: contextfrag.IntentRunConfigPreProvider,
		Sources: []SourceSpec{
			{Name: systemPromptCollectorName, Config: SystemPromptConfig{System: input.system, ToolUsage: input.toolUsage}},
			{Name: historyMessagesCollectorName, Config: HistoryMessagesConfig{Messages: input.messages}},
			{Name: currentUserCollectorName, Config: CurrentUserConfig{Query: input.query}},
		},
		Targets: []contextfrag.RenderTarget{contextfrag.RenderSDKMessages},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := view.Rendered[contextfrag.RenderSDKMessages].Data.(*SDKRenderedPayload)
	if payload.System != input.system {
		t.Fatalf("system = %q, want %q", payload.System, input.system)
	}
	assertMessagesEqual(t, payload.Messages, want)
}

func legacyProviderMessages(messages []sdk.Message, query string) []sdk.Message {
	var out []sdk.Message
	if messages != nil {
		out = make([]sdk.Message, len(messages))
		for i, message := range messages {
			out[i] = cloneSDKMessage(message)
		}
	}
	if query != "" {
		return append(out, sdk.UserMessage(query))
	}
	return out
}
