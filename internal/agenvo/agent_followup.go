package agenvo

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// AgentSessionInfo is Herdr's optional logical conversation reference, reported
// by the official Pi extension. Value may be a private path: never project this
// structure into a public tool result or use it as an execution argument.
type AgentSessionInfo struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

func (s *AgentSessionInfo) Valid() bool {
	if s == nil || s.Source != "herdr:pi" || s.Agent != "pi" || strings.TrimSpace(s.Value) == "" || !utf8.ValidString(s.Value) {
		return false
	}
	limit := 512
	if s.Kind == "path" {
		limit = 4096
		if !strings.HasPrefix(s.Value, "/") && !windowsAbsolute.MatchString(s.Value) {
			return false
		}
	} else if s.Kind != "id" {
		return false
	}
	return len(s.Value) <= limit && strings.IndexFunc(s.Value, unicode.IsControl) < 0
}

// AgentFollowupProgress describes one input delivery, not task completion.
// Session is a private snapshot for this request. It never replaces the
// original operation's logical-session binding.
type AgentFollowupProgress struct {
	State              string            `json:"state"`
	Reason             string            `json:"reason"`
	SessionRelation    string            `json:"session_relation"`
	Session            *AgentSessionInfo `json:"session,omitempty"`
	BaselineCompletion *uint64           `json:"baseline_completion,omitempty"`
}

func agentSessionRelation(bound, current *AgentSessionInfo) string {
	if !bound.Valid() {
		return "unbound"
	}
	if !current.Valid() {
		return "unavailable"
	}
	if *bound != *current {
		return "changed"
	}
	return "observed_match"
}
