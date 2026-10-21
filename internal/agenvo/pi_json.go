package agenvo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const maxPiJSONBytes = 64 << 20

// PiJSONResult describes the official Pi invocation, not independently verified
// completion of the user's real-world objective. Process exit is separate.
type PiJSONResult struct {
	Outcome    string `json:"outcome"`
	StopReason string `json:"stop_reason,omitempty"`
	Output     string `json:"output,omitempty"`
	Truncated  bool   `json:"truncated"`
	Records    int    `json:"records"`
}

// VerifyPiJSON consumes the complete, hash-verified stdout file. A terminal
// snapshot, a prefix, exit 0, agent_end, or a stale session JSONL is insufficient.
// Stdout uses LF framing; Unicode line/paragraph separators are ordinary data.
func VerifyPiJSON(input io.Reader, sessionID, provider, model string, exitCode int) PiJSONResult {
	out := PiJSONResult{Outcome: "invalid_protocol"}
	reader := bufio.NewReaderSize(io.LimitReader(input, maxPiJSONBytes+1), 64<<10)
	var total int
	var started, running, turnOpen, settled, aborted, final bool
	var messageOpen bool
	tools := map[string]bool{}
	for {
		line, err := reader.ReadBytes('\n')
		total += len(line)
		if total > maxPiJSONBytes || (err != nil && !errors.Is(err, io.EOF)) {
			return out
		}
		if len(line) == 0 && errors.Is(err, io.EOF) {
			break
		}
		if err != nil || !utf8.Valid(line) { // includes a truncated final JSONL record
			return out
		}
		line = bytes.TrimSuffix(bytes.TrimSuffix(line, []byte{'\n'}), []byte{'\r'})
		var event struct {
			Type       string            `json:"type"`
			Version    int               `json:"version"`
			ID         string            `json:"id"`
			Aborted    *bool             `json:"aborted"`
			ToolCallID string            `json:"toolCallId"`
			Message    json.RawMessage   `json:"message"`
			Steering   []json.RawMessage `json:"steering"`
			FollowUp   []json.RawMessage `json:"followUp"`
		}
		if json.Unmarshal(line, &event) != nil || event.Type == "" {
			return out
		}
		out.Records++
		if out.Records == 1 {
			if event.Type != "session" || event.Version != 3 || event.ID != sessionID {
				return out
			}
			continue
		}
		switch event.Type {
		case "session":
			return out
		case "agent_start":
			if running || messageOpen || turnOpen || len(tools) != 0 {
				return out
			}
			running = true
			started, settled, final = true, false, false
		case "turn_start":
			if !running || turnOpen {
				return out
			}
			turnOpen = true
			settled, final = false, false
		case "turn_end":
			if !turnOpen || messageOpen || len(tools) != 0 {
				return out
			}
			turnOpen = false
		case "agent_end":
			if !running || turnOpen || messageOpen || len(tools) != 0 {
				return out
			}
			running = false
		case "message_start":
			if !running || !turnOpen || messageOpen {
				return out
			}
			messageOpen, settled = true, false
		case "message_update":
			if !messageOpen {
				return out
			}
		case "message_end":
			if !started || !messageOpen {
				return out
			}
			messageOpen, settled = false, false
			var message struct {
				Role       string `json:"role"`
				Provider   string `json:"provider"`
				Model      string `json:"model"`
				StopReason string `json:"stopReason"`
				Content    []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			// User messages may have string content. Only assistant messages
			// supply a final result; never inspect terminal-like text recursively.
			var role struct {
				Role string `json:"role"`
			}
			if json.Unmarshal(event.Message, &role) != nil {
				return out
			}
			if role.Role != "assistant" {
				continue
			}
			if json.Unmarshal(event.Message, &message) != nil || message.Provider != provider || message.Model != model {
				return out // no implicit provider/model fallback
			}
			var text strings.Builder
			for _, c := range message.Content {
				if c.Type == "text" {
					text.WriteString(c.Text)
				}
			}
			out.Output, out.Truncated = boundedPiText(text.String())
			out.StopReason, final = message.StopReason, true
		case "tool_execution_start":
			if !running || !turnOpen || event.ToolCallID == "" || tools[event.ToolCallID] {
				return out
			}
			tools[event.ToolCallID], settled = true, false
		case "tool_execution_end":
			if !tools[event.ToolCallID] {
				return out
			}
			delete(tools, event.ToolCallID)
		case "agent_settled":
			if !started || running || turnOpen || settled || event.Aborted == nil || messageOpen || len(tools) != 0 {
				return out
			}
			settled, aborted = true, *event.Aborted
		case "tool_execution_update":
			if !tools[event.ToolCallID] {
				return out
			}
		case "auto_retry_start", "compaction_start", "summarization_retry_scheduled", "summarization_retry_attempt_start":
			settled = false
		case "queue_update":
			if len(event.Steering) != 0 || len(event.FollowUp) != 0 {
				settled = false
			}
		case "auto_retry_end", "compaction_end", "summarization_retry_finished", "entry_appended", "session_info_changed", "thinking_level_changed":
			// Official auxiliary events do not independently prove completion.
		default:
			return out // an unrecognized protocol must be reviewed, not guessed
		}
	}
	if !settled || !final || messageOpen || len(tools) != 0 {
		return out
	}
	switch {
	case aborted || out.StopReason == "aborted":
		out.Outcome = "aborted"
	case exitCode != 0 || out.StopReason == "error":
		out.Outcome = "failed"
	case out.StopReason == "length" || out.StopReason == "toolUse":
		out.Outcome = "incomplete"
	case out.StopReason == "stop":
		out.Outcome = "succeeded"
	}
	return out
}

func boundedPiText(s string) (string, bool) {
	if len(s) <= 16384 {
		return s, false
	}
	s = s[:16384]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s, true
}
