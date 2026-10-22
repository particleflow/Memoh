package agenvo

import (
	"encoding/json"
	"strings"
	"testing"
)

func piEvents(session, reason, text string, aborted bool) string {
	var out strings.Builder
	for _, event := range []any{
		map[string]any{"type": "session", "version": 3, "id": session},
		map[string]any{"type": "agent_start"},
		map[string]any{"type": "turn_start"},
		map[string]any{"type": "message_start", "message": map[string]any{"role": "assistant"}},
		map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "provider": "synthetic", "model": "synthetic-model", "stopReason": reason, "content": []any{map[string]any{"type": "text", "text": text}}}},
		map[string]any{"type": "turn_end"},
		map[string]any{"type": "agent_end", "willRetry": false},
		map[string]any{"type": "agent_settled", "aborted": aborted},
	} {
		b, _ := json.Marshal(event)
		out.Write(b)
		out.WriteByte('\n')
	}
	return out.String()
}

func TestPiJSONRequiresFinalSemanticEvidence(t *testing.T) {
	good := piEvents("session-a", "stop", "42 中文 ✓\u2028still one record", false)
	for _, tc := range []struct {
		name, data, outcome string
		exit                int
	}{
		{"success", good, "succeeded", 0},
		{"exit0_error", piEvents("session-a", "error", "failed", false), "failed", 0},
		{"exit0_aborted", piEvents("session-a", "stop", "partial", true), "aborted", 0},
		{"exit0_length", piEvents("session-a", "length", "partial", false), "incomplete", 0},
		{"nonzero", good, "failed", 7},
		{"partial_final_line", strings.TrimSuffix(good, "\n"), "invalid_protocol", 0},
		{"missing_agent_end", strings.Replace(good, "{\"type\":\"agent_end\",\"willRetry\":false}\n", "", 1), "invalid_protocol", 0},
		{"missing_turn_end", strings.Replace(good, "{\"type\":\"turn_end\"}\n", "", 1), "invalid_protocol", 0},
		{"unknown_event", good + "{\"type\":\"pretend_completed\"}\n", "invalid_protocol", 0},
		{"duplicate_settled", good + "{\"type\":\"agent_settled\",\"aborted\":false}\n", "invalid_protocol", 0},
		{"agent_end_is_not_final", good[:strings.LastIndex(good, `{"aborted"`)], "invalid_protocol", 0},
		{"wrong_session", strings.Replace(good, "session-a", "session-b", 1), "invalid_protocol", 0},
		{"provider_fallback", strings.ReplaceAll(good, `"provider":"synthetic"`, `"provider":"other"`), "invalid_protocol", 0},
		{"not_jsonl", "PS C:\\synthetic> task done\n", "invalid_protocol", 0},
		{"empty", "", "invalid_protocol", 0},
		{"incomplete_tool", strings.Replace(good, `{"aborted":false,"type":"agent_settled"}`, `{"type":"tool_execution_start","toolCallId":"unfinished"}`+"\n"+`{"aborted":false,"type":"agent_settled"}`, 1), "invalid_protocol", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := VerifyPiJSON(strings.NewReader(tc.data), "session-a", "synthetic", "synthetic-model", tc.exit)
			if got.Outcome != tc.outcome {
				t.Fatalf("got=%+v want=%s", got, tc.outcome)
			}
		})
	}
}

func TestPiJSONRetriesAndBoundedPreview(t *testing.T) {
	first := piEvents("session-a", "error", "transient", false)
	first = first[:strings.LastIndex(first, `{"aborted"`)]
	second := piEvents("session-a", "stop", strings.Repeat("中文", 9000), false)
	second = second[strings.IndexByte(second, '\n')+1:]
	got := VerifyPiJSON(strings.NewReader(first+second), "session-a", "synthetic", "synthetic-model", 0)
	if got.Outcome != "succeeded" || !got.Truncated || len(got.Output) > 16384 || strings.Contains(got.Output, "transient") {
		t.Fatalf("early retry failure or unbounded preview: %+v", got)
	}
}
