package botdelegation

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/felinics/memoh/internal/agent/turn"
)

func TestFinalSummaryAllowlistsFinalAssistantText(t *testing.T) {
	messages := []turn.ModelMessage{
		{Role: "tool", Content: turn.NewTextContent("SECRET stdout")},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"reasoning","text":"SECRET reasoning"},{"type":"thinking","text":"SECRET thinking"},{"type":"tool_result","text":"SECRET stderr"},{"type":"unknown","text":"SECRET future part"},{"type":"text","text":"<script>untrusted final text</script>"}]`)},
	}
	got := FinalSummary(messages, true)
	if got == nil || got.Text != "<script>untrusted final text</script>" || strings.Contains(got.Text, "SECRET") {
		t.Fatalf("summary=%+v", got)
	}
	if FinalSummary(messages, false) != nil {
		t.Fatal("unconfirmed run published a final result")
	}
}

func TestFinalSummaryDoesNotFallBackToEarlierOrToolOutput(t *testing.T) {
	for _, last := range []turn.ModelMessage{
		{Role: "tool", Content: turn.NewTextContent("SECRET")},
		{Role: "assistant", Content: turn.NewTextContent("narration"), ToolCalls: []turn.ToolCall{{ID: "call"}}},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"reasoning","text":"SECRET"}]`)},
		{Role: "assistant", Content: turn.NewTextContent("SECRET"), Source: turn.MessageSourceInternalFeedback},
		{Role: "assistant", Content: json.RawMessage(`{"text":"SECRET malformed"}`)},
	} {
		if got := FinalSummary([]turn.ModelMessage{{Role: "assistant", Content: turn.NewTextContent("earlier narration")}, last}, true); got != nil {
			t.Fatalf("non-final projected=%+v", got)
		}
	}
}

func TestFinalSummaryPreservesUTF8AtLimit(t *testing.T) {
	got := FinalSummary([]turn.ModelMessage{{Role: "assistant", Content: turn.NewTextContent(strings.Repeat("结果", maxSummaryBytes))}}, true)
	if got == nil || !got.Truncated || len(got.Text) > maxSummaryBytes || !utf8.ValidString(got.Text) {
		t.Fatalf("invalid bounded summary: %+v", got)
	}
}
