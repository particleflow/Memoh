package message

import "encoding/json"

// ReasoningEffortMetadataKey stores the per-turn reasoning effort tiering
// decision on the assistant rows of the turn that ran under it. Like the
// timing envelope it lives in metadata, outside model content, so it is never
// replayed to providers.
const ReasoningEffortMetadataKey = "reasoning_effort"

// ReasoningEffortFromMetadata decodes the tiering decision from a message
// metadata map. JSON round-tripping keeps this tolerant of values loaded from
// JSONB as map[string]any as well as typed values used in tests.
func ReasoningEffortFromMetadata(metadata map[string]any) (applied, ceiling, source string, ok bool) {
	if len(metadata) == 0 {
		return "", "", "", false
	}
	value, exists := metadata[ReasoningEffortMetadataKey]
	if !exists || value == nil {
		return "", "", "", false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", "", "", false
	}
	var envelope struct {
		Applied string `json:"applied"`
		Cap     string `json:"cap"`
		Source  string `json:"source"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Applied == "" {
		return "", "", "", false
	}
	return envelope.Applied, envelope.Cap, envelope.Source, true
}
