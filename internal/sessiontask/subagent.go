package sessiontask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/felinics/memoh/internal/db"
)

func (s *Service) projectSubagent(ctx context.Context, bot, session string, t Task) (Task, error) {
	r, err := s.store.ReadSubagentRun(ctx, bot, session)
	if errors.Is(err, db.ErrNotFound) {
		return t, nil
	}
	if err != nil {
		return Task{}, err
	}
	if r.BotID != bot || r.SessionID != session || r.RunID == "" {
		return Task{}, ErrUnavailable
	}
	switch r.State {
	case "accepted", "running", "waiting_decision", "finishing", "completed", "failed", "aborted", "lost":
		t.Status, t.RunID, t.UpdatedAt = r.State, r.RunID, r.UpdatedAt
	default:
		return t, nil
	}
	t.Summary = "Latest child run state; visible assistant text is not business verification."
	t.BusinessState = "unknown"
	t.ResultSummary = visibleSubagentText(r.Content)
	t.ContentAvailable = t.ResultSummary != nil
	return t, nil
}

// A bounded visible assistant message from this run can update while it runs.
// This is neither a stream nor a diagnostic log; exclude reasoning/tool parts,
// feedback and arbitrary metadata. Assistant text itself is not secret-redacted.
func visibleSubagentText(raw json.RawMessage) *ResultSummary {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return nil
	}
	var message struct {
		Role    string          `json:"role"`
		Source  string          `json:"source"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &message) != nil || message.Role != "assistant" || message.Source != "" {
		return nil
	}
	var text string
	if json.Unmarshal(message.Content, &text) != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &parts) != nil {
			return nil
		}
		var visible []string
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				visible = append(visible, p.Text)
			}
		}
		text = strings.Join(visible, "\n")
	}
	text = strings.TrimSpace(text)
	if text == "" || !utf8.ValidString(text) {
		return nil
	}
	const limit = 16 * 1024
	truncated := len(text) > limit
	if truncated {
		text = text[:limit]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return &ResultSummary{Text: text, Truncated: truncated, Source: "subagent_assistant"}
}
