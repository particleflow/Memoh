package botdelegation

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/felinics/memoh/internal/agent/turn"
)

const maxSummaryBytes = 16 * 1024

// Summary is final user-visible assistant text, not arbitrary output with
// secrets removed. Clients must render Text as untrusted plain text.
type Summary struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Source    string `json:"source"`
}

// FinalSummary requires a confirmed terminal run and its actual final message.
// Earlier narration, reasoning, tool results, arbitrary part types, and a
// successful submission acknowledgement do not become a specialist result.
func FinalSummary(messages []turn.ModelMessage, completed bool) *Summary {
	if !completed || len(messages) == 0 {
		return nil
	}
	m := messages[len(messages)-1]
	if m.Role != "assistant" || m.Source == turn.MessageSourceInternalFeedback || len(m.ToolCalls) != 0 || m.ToolCallID != "" {
		return nil
	}
	var text string
	if json.Unmarshal(m.Content, &text) != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(m.Content, &parts) != nil {
			return nil
		}
		var visible []string
		for _, part := range parts {
			if part.Type == "text" && part.Text != "" {
				visible = append(visible, part.Text)
			}
		}
		text = strings.Join(visible, "\n")
	}
	text = strings.TrimSpace(text)
	if text == "" || !utf8.ValidString(text) {
		return nil
	}
	truncated := len(text) > maxSummaryBytes
	if truncated {
		text = text[:maxSummaryBytes]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return &Summary{Text: text, Truncated: truncated, Source: "assistant_final"}
}
