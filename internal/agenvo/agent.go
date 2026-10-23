package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrAgentNotConfigured = errors.New("this computer has no configured Pi provider profile")

// AgentProgress is a private receipt, persisted before each mutating call.
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

// SubmitAgent uses only this fixed Herdr procedure. It never discovers a raw
// method during a business request, runs arbitrary relay code, or resumes an
// uncertain mutating step. A caller must durably reserve its task before entry.
func (s *Service) SubmitAgent(ctx context.Context, scope Scope, task, workDir string, p AgentProgress, save func(AgentProgress) error) (AgentProgress, error) {
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return p, err
	}
	if target.Agent == (AgentProfile{}) {
		return p, ErrAgentNotConfigured
	}
	if target.Agent.Validate() != nil || len(task) == 0 || len(task) > 32000 || !utf8.ValidString(task) || strings.ContainsRune(task, 0) || len(p.Name) != 31 || !strings.HasPrefix(p.Name, "m") || p.Phase != "" {
		return p, ErrUnsupported
	}
	cwd, err := resolveWorkDir(target.WorkDir, workDir)
	if err != nil {
		return p, err
	}
	call := func(phase, method string, args map[string]any) (Receipt, error) {
		p.Phase = phase
		if err := save(p); err != nil {
			return Receipt{}, err
		}
		return c.relay(ctx, scope, target, method, args)
	}
	created, err := call("creating_workspace", "workspace.create", map[string]any{"session": target.Session, "cwd": cwd, "label": p.Name})
	if err != nil {
		return p, err
	}
	var workspace nativeWorkspace
	if resultBody(created, &workspace) != nil || workspace.Workspace.ID == "" || workspace.Workspace.Label != p.Name {
		return p, ErrUnknown
	}
	p.WorkspaceID = workspace.Workspace.ID
	env := map[string]string{"PI_CODING_AGENT_DIR": target.Agent.ConfigDir}
	if target.Agent.Path != "" {
		env["PATH"] = target.Agent.Path
	}
	created, err = call("creating_tab", "tab.create", map[string]any{"session": target.Session, "workspaceId": p.WorkspaceID, "cwd": cwd, "label": p.Name, "env": env})
	if err != nil {
		return p, err
	}
	var tab struct {
		RootPane struct {
			ID        string `json:"pane_id"`
			Terminal  string `json:"terminal_id"`
			Workspace string `json:"workspace_id"`
		} `json:"root_pane"`
	}
	if resultBody(created, &tab) != nil || tab.RootPane.ID == "" || tab.RootPane.Terminal == "" || tab.RootPane.Workspace != p.WorkspaceID {
		return p, ErrUnknown
	}
	p.PaneID, p.TerminalID = tab.RootPane.ID, tab.RootPane.Terminal
	started, err := call("starting_agent", "agent.start", map[string]any{"session": target.Session, "name": p.Name, "paneId": p.PaneID, "kind": "pi", "args": []string{"--provider", target.Agent.Provider, "--model", target.Agent.Model}, "timeoutMs": 30000})
	if err != nil {
		return p, err
	}
	var start struct {
		Session string `json:"session"`
		Name    string `json:"name"`
		Query   string `json:"query"`
	}
	if started.Execution != "starting" || json.Unmarshal(started.Result, &start) != nil || start.Name != p.Name || start.Session != target.Session || start.Query != "agent.get" {
		return p, ErrUnknown
	}
	for {
		a, starting, err := c.agentInfo(ctx, scope, target, p)
		if err != nil {
			return p, err
		}
		if !starting && a.InteractiveReady {
			p.BaselineCompletion = a.Completion
			// This is optional official metadata, not a native generation or
			// proof of uninterrupted Pi conversation continuity.
			if a.AgentSession.Valid() {
				p.AgentSession = a.AgentSession
			}
			break
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return p, ErrUnknown
		case <-timer.C:
		}
	}
	prompted, err := call("submitting_prompt", "agent.prompt", map[string]any{"session": target.Session, "name": p.Name, "text": task})
	if err == nil || errors.Is(err, ErrRejected) {
		err = validatePromptReceipt(prompted, target.Session, p)
	}
	if err != nil {
		if errors.Is(err, ErrRejected) {
			p.Phase = "prompt_rejected"
		}
		return p, err
	}
	// The prompt receipt can be a snapshot of the PREVIOUS round. No status
	// in it is interpreted as this task's completion.
	p.PromptSubmitted = true
	p.Phase = "prompt_submitted"
	return p, save(p)
}

// Only Herdr's pre-submission validation errors prove that no prompt was
// delivered. timeout/agent_prompt_failed can follow partial terminal input.
func promptRejection(r Receipt) error {
	if r.Error == nil || (r.Execution != "rejected" && r.Execution != "not_started") {
		return ErrUnknown
	}
	var native struct {
		Code string `json:"code"`
	}
	if r.Error.Code != "native_error" || json.Unmarshal(r.Error.Native, &native) != nil {
		return ErrUnknown
	}
	switch native.Code {
	case "agent_not_found", "agent_not_ready", "agent_blocked", "empty_agent_prompt":
		return ErrRejected
	default:
		return ErrUnknown
	}
}

func validatePromptReceipt(r Receipt, session string, p AgentProgress) error {
	if r.Error != nil {
		return promptRejection(r)
	}
	var envelope struct {
		ID          string          `json:"id"`
		Session     string          `json:"session"`
		NativeError json.RawMessage `json:"nativeError"`
		Result      *struct {
			Type  string     `json:"type"`
			Agent *agentInfo `json:"agent"`
		} `json:"result"`
	}
	if r.Execution != "accepted" || json.Unmarshal(r.Result, &envelope) != nil {
		return ErrUnknown
	}
	if len(envelope.NativeError) != 0 && string(envelope.NativeError) != "null" {
		var rejected Receipt
		if json.Unmarshal(envelope.NativeError, &rejected) != nil {
			return ErrUnknown
		}
		return promptRejection(rejected)
	}
	if envelope.ID != "cli:agent:prompt" || envelope.Session != session || envelope.Result == nil || envelope.Result.Type != "agent_prompted" || envelope.Result.Agent == nil {
		return ErrUnknown
	}
	a := envelope.Result.Agent
	if a.Name != p.Name || a.Agent != "pi" || a.WorkspaceID != p.WorkspaceID || a.PaneID != p.PaneID || a.TerminalID != p.TerminalID {
		return ErrUnknown
	}
	return nil
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
	obs.Status, obs.Starting, obs.InteractiveReady = a.Status, starting, a.InteractiveReady
	obs.Status = nativeState(a.Status)
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
