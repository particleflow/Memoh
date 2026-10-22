package agenvo

import (
	"context"
	"encoding/json"
	"errors"
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

// PromptAgent sends only to the currently observed native association. It does
// not create, restart or resume an Agent/session. The caller durably reserves
// the request and saves dispatching before this one RPC; neither layer retries.
// bound_session requires matching logical-session observations, not an atomic
// incarnation guarantee. current_agent explicitly permits unknown/changed
// logical continuity. Both retain the get-to-prompt race of the native API.
func (s *Service) PromptAgent(ctx context.Context, scope Scope, p AgentProgress, mode, task string, save func(AgentFollowupProgress) error) (AgentFollowupProgress, error) {
	out := AgentFollowupProgress{State: "rejected", Reason: "invalid_native_binding", SessionRelation: "unbound"}
	if !p.PromptSubmitted || p.Name == "" || p.WorkspaceID == "" || p.PaneID == "" || p.TerminalID == "" || (mode != "current_agent" && mode != "bound_session") || strings.TrimSpace(task) == "" || len(task) > 32000 || !utf8.ValidString(task) || strings.ContainsRune(task, 0) || save == nil {
		return out, ErrUnsupported
	}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		out.Reason = "authorization_unavailable"
		return out, ErrRejected
	}
	if target.Agent == (AgentProfile{}) || target.Agent.ExecutionMode != "" {
		return out, ErrUnsupported
	}
	var body struct {
		Agent *agentInfo `json:"agent"`
	}
	_, err = c.readObservation(ctx, scope, target, "agent.get", map[string]any{"session": target.Session, "name": p.Name}, &body)
	if err != nil {
		out.Reason = "agent_observation_unavailable"
		if errors.Is(err, errReferenceMissing) {
			out.Reason = "agent_missing"
		}
		return out, ErrRejected // Only a read has occurred; no input was sent.
	}
	a := body.Agent
	if a == nil {
		out.Reason = "agent_metadata_unverified"
		return out, ErrRejected
	}
	if a.Name != p.Name || a.Agent != "pi" || a.WorkspaceID != p.WorkspaceID || a.PaneID != p.PaneID || a.TerminalID != p.TerminalID {
		out.Reason = "agent_reference_changed"
		return out, ErrRejected
	}
	out.SessionRelation = agentSessionRelation(p.AgentSession, a.AgentSession)
	if a.AgentSession != nil && !a.AgentSession.Valid() {
		out.Reason = "agent_session_unverified"
		return out, ErrRejected
	}
	out.Session, out.BaselineCompletion = a.AgentSession, a.Completion
	if mode == "bound_session" && out.SessionRelation != "observed_match" {
		out.Reason = "bound_session_unavailable"
		return out, ErrRejected
	}
	if a.LaunchPending || !a.InteractiveReady || a.Status == "blocked" {
		out.Reason = "agent_not_ready"
		return out, ErrRejected
	}
	out.State, out.Reason = "dispatching", "prompt_confirmation_pending"
	if err := save(out); err != nil {
		out.State, out.Reason = "rejected", "dispatch_not_started"
		return out, ErrRejected
	}
	r, err := c.relay(ctx, scope, target, "agent.prompt", map[string]any{"session": target.Session, "name": p.Name, "text": task})
	out.State, out.Reason = "unknown", "prompt_confirmation_missing"
	if err == nil || errors.Is(err, ErrRejected) {
		err = validatePromptReceipt(r, target.Session, p)
	}
	if err != nil {
		if errors.Is(err, ErrRejected) {
			out.State, out.Reason = "rejected", "native_prompt_rejected"
		}
		return out, err
	}
	var envelope struct {
		Result struct {
			Agent agentInfo `json:"agent"`
		} `json:"result"`
	}
	if json.Unmarshal(r.Result, &envelope) != nil {
		return out, ErrUnknown
	}
	// Compare the prompt receipt to this request's observation, including
	// disappearance/appearance of session metadata. A mismatch is too late to
	// prove non-delivery, so it blocks further dispatch instead of being retried.
	received := envelope.Result.Agent.AgentSession
	if (received == nil) != (out.Session == nil) || received != nil && (!received.Valid() || *received != *out.Session) {
		out.Reason = "session_changed_during_dispatch"
		return out, ErrUnknown
	}
	out.State, out.Reason = "accepted", "native_prompt_accepted"
	return out, nil
}
