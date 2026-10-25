package agenvo

import (
	"context"
	"encoding/json"
	"strings"
)

// Adoption is deliberately limited to the one unresolved, pre-prompt stage.
// A label is a discovery key, not authority: the caller must retain the original
// operation identity and registration pin, then durably save the adopted result.
func (s *Service) AdoptBatchWorkspace(ctx context.Context, scope Scope, workDir string, p BatchProgress) (BatchProgress, error) {
	if !p.valid() || p.Phase != "batch_creating_workspace" || p.WorkspaceID != "" || p.PaneID != "" || p.TerminalID != "" || p.ControlPane != "" || p.ControlTerminal != "" {
		return p, ErrUnsupported
	}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return p, err
	}
	if target.NativeRuntimeID == "" {
		return p, ErrUnsupported
	}
	cwd, err := resolveWorkDir(target.WorkDir, workDir)
	if err != nil {
		return p, err
	}
	var listed struct {
		Workspaces []batchResumeWorkspace `json:"workspaces"`
	}
	if err := c.batchResumeRead(ctx, scope, target, "workspace.list", "workspace_list", map[string]any{"session": target.Session}, &listed); err != nil {
		return p, err
	}
	if listed.Workspaces == nil {
		return p, ErrUnknown
	}
	seen := make(map[string]bool, len(listed.Workspaces))
	var selected batchResumeWorkspace
	for _, ws := range listed.Workspaces {
		if !ws.valid() || seen[ws.ID] {
			return p, ErrUnknown
		}
		seen[ws.ID] = true
		if ws.Label == "memoh-batch-"+p.Attempt {
			if selected.ID != "" {
				return p, ErrUnknown
			}
			selected = ws
		}
	}
	if selected.ID == "" || selected.TabCount != 1 || selected.PaneCount != 1 {
		return p, ErrUnknown
	}
	ws, err := c.batchResumeWorkspace(ctx, scope, target, selected.ID)
	if err != nil {
		return p, err
	}
	if ws != selected {
		return p, ErrTargetChanged
	}
	var panes struct {
		Panes []batchResumePane `json:"panes"`
	}
	if err := c.batchResumeRead(ctx, scope, target, "pane.list", "pane_list", map[string]any{"session": target.Session}, &panes); err != nil {
		return p, err
	}
	if panes.Panes == nil {
		return p, ErrUnknown
	}
	seenPanes, seenTerminals := map[string]bool{}, map[string]bool{}
	var control batchResumePane
	for _, pane := range panes.Panes {
		if !pane.valid() || seenPanes[pane.ID] || seenTerminals[pane.Terminal] {
			return p, ErrUnknown
		}
		seenPanes[pane.ID], seenTerminals[pane.Terminal] = true, true
		if pane.Workspace == ws.ID {
			if control.ID != "" {
				return p, ErrUnknown
			}
			control = pane
		}
	}
	if control.ID == "" || control.Tab != ws.ActiveTab || !sameBatchResumeCWD(control.CWD, cwd) {
		return p, ErrUnknown
	}
	observed, err := c.batchResumePane(ctx, scope, target, control.ID)
	if err != nil {
		return p, err
	}
	if !sameBatchResumePane(observed, control) {
		return p, ErrTargetChanged
	}
	if err := c.batchResumeNoAgents(ctx, scope, target); err != nil {
		return p, err
	}
	last, err := c.batchResumeWorkspace(ctx, scope, target, ws.ID)
	if err != nil {
		return p, err
	}
	if last != ws {
		return p, ErrTargetChanged
	}
	p.WorkspaceID, p.ControlPane, p.ControlTerminal = ws.ID, control.ID, control.Terminal
	p.Phase = "batch_workspace_adopted"
	return p, nil
}

// ContinueBatch consumes a durably adopted stage of the SAME invocation. save
// must atomically recheck admission and the original operation revision before
// each pre-dispatch phase. It never creates a workspace or retries a write.
// A callback or transport failure retains the pre-dispatch phase, which cannot
// reenter this method. Observed native references do not authorize replay.
func (s *Service) ContinueBatch(ctx context.Context, scope Scope, task, workDir string, p BatchProgress, save func(BatchProgress) error) (BatchProgress, error) {
	if !p.valid() || save == nil || (p.Phase != "batch_workspace_adopted" && p.Phase != "batch_tab_adopted") || !batchReaderID(p.WorkspaceID) || !batchReaderID(p.ControlPane) || !batchReaderID(p.ControlTerminal) {
		return p, ErrUnsupported
	}
	if p.Phase == "batch_workspace_adopted" {
		if p.PaneID != "" || p.TerminalID != "" {
			return p, ErrUnsupported
		}
	} else if !batchReaderID(p.PaneID) || !batchReaderID(p.TerminalID) || p.PaneID == p.ControlPane || p.TerminalID == p.ControlTerminal {
		return p, ErrUnsupported
	}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return p, err
	}
	if target.NativeRuntimeID == "" || target.Agent.ExecutionMode != "pi-json-v1" || target.Agent.Validate() != nil || ValidateBatchRequest(p, target, workDir, task) != nil {
		return p, ErrUnsupported
	}
	cwd, err := resolveWorkDir(target.WorkDir, workDir)
	if err != nil {
		return p, err
	}
	control, err := c.verifyBatchResume(ctx, scope, target, cwd, p)
	if err != nil {
		return p, err
	}
	if p.Phase == "batch_workspace_adopted" {
		p.Phase = "batch_creating_tab"
		if err := save(p); err != nil {
			return p, err
		}
		env := map[string]string{"PI_CODING_AGENT_DIR": target.Agent.ConfigDir}
		if target.Agent.Path != "" {
			env["PATH"] = target.Agent.Path
		}
		created, err := c.native(ctx, scope, target, "tab.create", map[string]any{"session": target.Session, "workspaceId": p.WorkspaceID, "cwd": cwd, "label": "memoh-pi-" + p.Attempt, "env": env})
		if err != nil {
			return p, err
		}
		var tab struct {
			Tab struct {
				ID        string `json:"tab_id"`
				Workspace string `json:"workspace_id"`
				Label     string `json:"label"`
			} `json:"tab"`
			Root batchResumePane `json:"root_pane"`
		}
		if decodeBatchResumeResult(created, target.Session, "tab_created", &tab) != nil || !tab.Root.valid() || !batchReaderID(tab.Tab.ID) || tab.Tab.ID == control.Tab || tab.Tab.Workspace != p.WorkspaceID || tab.Tab.Label != "memoh-pi-"+p.Attempt || tab.Root.Workspace != p.WorkspaceID || tab.Root.Tab != tab.Tab.ID || !sameBatchResumeCWD(tab.Root.CWD, cwd) || tab.Root.ID == p.ControlPane || tab.Root.Terminal == p.ControlTerminal {
			return p, ErrUnknown
		}
		p.PaneID, p.TerminalID = tab.Root.ID, tab.Root.Terminal
		adopted := p
		adopted.Phase = "batch_tab_adopted"
		if err := save(adopted); err != nil {
			return p, err
		}
		p = adopted
		// The new tab receipt and its durable checkpoint do not freeze the
		// native terminal. Apply the same identity check used after restart
		// before reserving the first Pi input in this uninterrupted call.
		if _, err := c.verifyBatchResume(ctx, scope, target, cwd, p); err != nil {
			return p, err
		}
	}
	script, err := batchScript(p, target, cwd, task)
	if err != nil {
		return p, err
	}
	command := launchBashScript(script, target.BashPath)
	if len(command) > 48000 {
		return p, ErrUnsupported
	}
	p.Phase = "batch_submitting"
	if err := save(p); err != nil {
		return p, err
	}
	r, err := c.native(ctx, scope, target, "pane.run", map[string]any{"session": target.Session, "paneId": p.PaneID, "command": command})
	p.RequestID = r.RequestID
	if err != nil {
		return p, err
	}
	submitted := p
	submitted.Phase = "batch_submitted"
	if err := save(submitted); err != nil {
		return p, err
	}
	return submitted, nil
}

type batchResumeWorkspace struct {
	ID        string `json:"workspace_id"`
	Label     string `json:"label"`
	ActiveTab string `json:"active_tab_id"`
	TabCount  int    `json:"tab_count"`
	PaneCount int    `json:"pane_count"`
}

func (w batchResumeWorkspace) valid() bool {
	return batchReaderID(w.ID) && batchReaderID(w.ActiveTab) && boundedNativeText(w.Label, 512) && w.TabCount > 0 && w.PaneCount >= w.TabCount
}

type batchResumePane struct {
	ID        string `json:"pane_id"`
	Workspace string `json:"workspace_id"`
	Tab       string `json:"tab_id"`
	Terminal  string `json:"terminal_id"`
	CWD       string `json:"cwd"`
}

func (p batchResumePane) valid() bool {
	return batchReaderID(p.ID) && batchReaderID(p.Workspace) && batchReaderID(p.Tab) && batchReaderID(p.Terminal) && p.CWD != "" && boundedNativeText(p.CWD, 4096)
}

// Match the existing Windows work-directory rules, regardless of the server
// OS. Normalization cannot select a child, sibling, other drive, or UNC alias.
// This is a lexical identity check, not a claim about junctions or OS ACLs.
func sameBatchResumeCWD(a, b string) bool {
	av, ap, aok := absoluteWindowsParts(a)
	bv, bp, bok := absoluteWindowsParts(b)
	if !aok || !bok || !strings.EqualFold(av, bv) || len(ap) != len(bp) {
		return false
	}
	for i := range ap {
		if !strings.EqualFold(ap[i], bp[i]) {
			return false
		}
	}
	return true
}

func sameBatchResumePane(a, b batchResumePane) bool {
	return a.ID == b.ID && a.Workspace == b.Workspace && a.Tab == b.Tab && a.Terminal == b.Terminal && sameBatchResumeCWD(a.CWD, b.CWD)
}

func decodeBatchResumeResult(r Receipt, session, kind string, out any) error {
	var envelope struct {
		Session string          `json:"session"`
		Result  json.RawMessage `json:"result"`
	}
	var body struct {
		Type string `json:"type"`
	}
	if r.Execution != "accepted" || r.Error != nil || json.Unmarshal(r.Result, &envelope) != nil || envelope.Session != session || json.Unmarshal(envelope.Result, &body) != nil || body.Type != kind || json.Unmarshal(envelope.Result, out) != nil {
		return ErrUnknown
	}
	return nil
}

func (c *Client) batchResumeRead(ctx context.Context, scope Scope, target Target, method, kind string, params map[string]any, out any) error {
	r, err := c.native(ctx, scope, target, method, params)
	if err != nil {
		return err
	}
	return decodeBatchResumeResult(r, target.Session, kind, out)
}

// A dedicated session must have no registered agents before this narrow
// pre-prompt recovery. Do not infer that an unrelated or malformed entry is
// safe, and do not start/stop an agent to make the condition true.
func (c *Client) batchResumeNoAgents(ctx context.Context, scope Scope, target Target) error {
	var body struct {
		Agents []json.RawMessage `json:"agents"`
	}
	if err := c.batchResumeRead(ctx, scope, target, "agent.list", "agent_list", map[string]any{"session": target.Session}, &body); err != nil {
		return err
	}
	if body.Agents == nil || len(body.Agents) != 0 {
		return ErrUnknown
	}
	return nil
}

func (c *Client) batchResumeWorkspace(ctx context.Context, scope Scope, target Target, id string) (batchResumeWorkspace, error) {
	var body struct {
		Workspace batchResumeWorkspace `json:"workspace"`
	}
	if err := c.batchResumeRead(ctx, scope, target, "workspace.get", "workspace_info", map[string]any{"session": target.Session, "workspaceId": id}, &body); err != nil {
		return batchResumeWorkspace{}, err
	}
	if !body.Workspace.valid() || body.Workspace.ID != id {
		return batchResumeWorkspace{}, ErrUnknown
	}
	return body.Workspace, nil
}

func (c *Client) batchResumePane(ctx context.Context, scope Scope, target Target, id string) (batchResumePane, error) {
	var body struct {
		Pane batchResumePane `json:"pane"`
	}
	if err := c.batchResumeRead(ctx, scope, target, "pane.get", "pane_info", map[string]any{"session": target.Session, "paneId": id}, &body); err != nil {
		return batchResumePane{}, err
	}
	if !body.Pane.valid() || body.Pane.ID != id {
		return batchResumePane{}, ErrUnknown
	}
	return body.Pane, nil
}

func (c *Client) verifyBatchResume(ctx context.Context, scope Scope, target Target, cwd string, p BatchProgress) (batchResumePane, error) {
	ws, err := c.batchResumeWorkspace(ctx, scope, target, p.WorkspaceID)
	if err != nil {
		return batchResumePane{}, err
	}
	count := 1
	if p.Phase == "batch_tab_adopted" {
		count = 2
	}
	if ws.Label != "memoh-batch-"+p.Attempt || ws.TabCount != count || ws.PaneCount != count {
		return batchResumePane{}, ErrTargetChanged
	}
	control, err := c.batchResumePane(ctx, scope, target, p.ControlPane)
	if err != nil {
		return batchResumePane{}, err
	}
	if control.Terminal != p.ControlTerminal || control.Workspace != ws.ID || (count == 1 && control.Tab != ws.ActiveTab) || !sameBatchResumeCWD(control.CWD, cwd) {
		return batchResumePane{}, ErrTargetChanged
	}
	if count == 2 {
		pane, err := c.batchResumePane(ctx, scope, target, p.PaneID)
		if err != nil {
			return batchResumePane{}, err
		}
		if pane.Terminal != p.TerminalID || pane.Workspace != ws.ID || pane.Tab == control.Tab || pane.Terminal == control.Terminal || (ws.ActiveTab != control.Tab && ws.ActiveTab != pane.Tab) || !sameBatchResumeCWD(pane.CWD, cwd) {
			return batchResumePane{}, ErrTargetChanged
		}
	}
	if err := c.batchResumeNoAgents(ctx, scope, target); err != nil {
		return batchResumePane{}, err
	}
	last, err := c.batchResumeWorkspace(ctx, scope, target, ws.ID)
	if err != nil {
		return batchResumePane{}, err
	}
	if last != ws {
		return batchResumePane{}, ErrTargetChanged
	}
	return control, nil
}
