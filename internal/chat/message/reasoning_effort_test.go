package message

import "testing"

func TestReasoningEffortFromMetadata(t *testing.T) {
	// Typed value, as the application layer writes it.
	typed := map[string]any{
		ReasoningEffortMetadataKey: struct {
			Applied string `json:"applied"`
			Cap     string `json:"cap"`
			Source  string `json:"source"`
		}{Applied: "high", Cap: "xhigh", Source: "chat_default"},
	}
	applied, ceiling, source, ok := ReasoningEffortFromMetadata(typed)
	if !ok || applied != "high" || ceiling != "xhigh" || source != "chat_default" {
		t.Fatalf("typed decode = (%q, %q, %q, %v)", applied, ceiling, source, ok)
	}
	// JSON round-tripped value, as it comes back from JSONB.
	roundTripped := map[string]any{
		ReasoningEffortMetadataKey: map[string]any{
			"applied": "low",
			"cap":     "xhigh",
			"source":  "short_input",
		},
	}
	applied, ceiling, source, ok = ReasoningEffortFromMetadata(roundTripped)
	if !ok || applied != "low" || ceiling != "xhigh" || source != "short_input" {
		t.Fatalf("jsonb decode = (%q, %q, %q, %v)", applied, ceiling, source, ok)
	}
	// Absent or unusable values report not-ok, never a partial tier.
	for name, metadata := range map[string]map[string]any{
		"nil":     nil,
		"empty":   {},
		"wrong-t": {ReasoningEffortMetadataKey: "low"},
		"no-applied": {ReasoningEffortMetadataKey: map[string]any{
			"cap": "xhigh", "source": "chat_default",
		}},
	} {
		if _, _, _, ok := ReasoningEffortFromMetadata(metadata); ok {
			t.Fatalf("%s metadata decoded as present", name)
		}
	}
}
