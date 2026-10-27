package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"
)

var ErrAgentNotConfigured = errors.New("this computer has no configured Pi provider profile")

// AgentProgress decodes the private receipt of historical TUI input.
// These native references are not accepted from a model or public MCP client.
type AgentProgress struct {
	Phase              string            `json:"phase"`
	Name               string            `json:"name"`
	WorkspaceID        string            `json:"workspace_id"`
	PaneID             string            `json:"pane_id"`
	TerminalID         string            `json:"terminal_id"`
	BaselineCompletion *uint64           `json:"baseline_completion,omitempty"`
	PromptSubmitted    bool              `json:"prompt_submitted"`
	AgentSession       *AgentSessionInfo `json:"agent_session,omitempty"`
}

type AgentObservation struct {
	// AgentSession is private sampled metadata, never a public path/ID or
	// an atomic binding between the get/read calls and a particular prompt.
	AgentSession      *AgentSessionInfo `json:"-"`
	CompletionSeq     string            `json:"completion_seq,omitempty"`
	SessionRelation   string            `json:"session_relation,omitempty"`
	Truncated         bool              `json:"truncated"`
	Status            string            `json:"status"`
	Starting          bool              `json:"starting"`
	InteractiveReady  bool              `json:"interactive_ready"`
	CompletionChanged bool              `json:"completion_changed"`
	Output            string            `json:"output"`
	OutputFidelity    string            `json:"output_fidelity"`
	// No Herdr status, completion sequence or terminal text independently
	// proves that the requested work succeeded. No lifecycle hook is assumed.
	TaskSuccessVerified bool `json:"task_success_verified"`
}

type agentInfo struct {
	Name             string            `json:"name"`
	Agent            string            `json:"agent"`
	WorkspaceID      string            `json:"workspace_id"`
	PaneID           string            `json:"pane_id"`
	TerminalID       string            `json:"terminal_id"`
	Status           string            `json:"agent_status"`
	InteractiveReady bool              `json:"interactive_ready"`
	LaunchPending    bool              `json:"launch_pending"`
	Completion       *uint64           `json:"completion_seq"`
	AgentSession     *AgentSessionInfo `json:"agent_session,omitempty"`
}

func (c *Client) agentInfo(ctx context.Context, scope Scope, target Target, p AgentProgress) (agentInfo, bool, error) {
	r, err := c.relay(ctx, scope, target, "agent.get", map[string]any{"session": target.Session, "name": p.Name})
	if err != nil {
		return agentInfo{}, false, err
	}
	var envelope struct {
		NativeError json.RawMessage `json:"nativeError"`
		Startup     struct {
			State string `json:"state"`
		} `json:"startup"`
		Result struct {
			Type  string     `json:"type"`
			Agent *agentInfo `json:"agent"`
		} `json:"result"`
	}
	if json.Unmarshal(r.Result, &envelope) != nil {
		return agentInfo{}, false, ErrUnknown
	}
	if envelope.Startup.State == "starting" && envelope.Result.Agent == nil {
		return agentInfo{}, true, nil
	}
	if len(envelope.NativeError) > 0 && string(envelope.NativeError) != "null" {
		return agentInfo{}, false, ErrUnknown
	}
	if envelope.Result.Agent == nil {
		return agentInfo{}, false, ErrUnknown
	}
	a := *envelope.Result.Agent
	if a.Name != p.Name || a.Agent != "pi" || a.WorkspaceID != p.WorkspaceID || a.PaneID != p.PaneID || a.TerminalID == "" || a.TerminalID != p.TerminalID {
		return agentInfo{}, false, ErrTargetChanged
	}
	return a, envelope.Startup.State == "starting" || a.LaunchPending, nil
}

func (s *Service) ObserveAgent(ctx context.Context, scope Scope, p AgentProgress) (AgentObservation, error) {
	obs := AgentObservation{OutputFidelity: "terminal_snapshot_unverified"}
	if p.Name == "" || p.PaneID == "" || p.TerminalID == "" {
		return obs, ErrUnknown
	}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return obs, err
	}
	a, starting, err := c.agentInfo(ctx, scope, target, p)
	if err != nil {
		return obs, err
	}
	obs.Status, obs.Starting, obs.InteractiveReady = nativeState(a.Status), starting, a.InteractiveReady
	obs.SessionRelation = agentSessionRelation(p.AgentSession, a.AgentSession)
	if a.AgentSession.Valid() {
		obs.AgentSession = a.AgentSession
	} else if a.AgentSession != nil {
		obs.SessionRelation = "unverified"
	}
	if a.Completion != nil {
		// Public JSON numbers cannot safely preserve all uint64 sequence values.
		obs.CompletionSeq = strconv.FormatUint(*a.Completion, 10)
	}
	if starting && a.Name == "" {
		return obs, nil
	}
	obs.CompletionChanged = p.BaselineCompletion != nil && a.Completion != nil && *a.Completion > *p.BaselineCompletion
	r, err := c.relay(ctx, scope, target, "agent.read", map[string]any{"session": target.Session, "name": p.Name, "lines": 200, "source": "recent-unwrapped"})
	if err != nil {
		return obs, err
	}
	var read struct {
		Output  *string `json:"output"`
		Session string  `json:"session"`
	}
	if json.Unmarshal(r.Result, &read) != nil || read.Output == nil || read.Session != target.Session || !utf8.ValidString(*read.Output) {
		return obs, ErrUnknown
	}
	// The connector JSON-parses CLI stdout. Even an output-shaped JSON value
	// cannot be distinguished from text here; never present it as lossless data
	// or use it as a task-completion receipt.
	obs.Output = *read.Output
	if len(obs.Output) > 16384 {
		obs.Truncated = true
		obs.Output = obs.Output[:16384]
		for !utf8.ValidString(obs.Output) {
			obs.Output = obs.Output[:len(obs.Output)-1]
		}
	}
	return obs, nil
}
