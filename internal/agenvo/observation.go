package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrAuthenticationUnavailable = errors.New("computer connection authentication unavailable")
	ErrDeviceOffline             = errors.New("computer device confirmed offline")
	ErrNativeUnavailable         = errors.New("native service unavailable")
	errReferenceMissing          = errors.New("native reference missing")
)

// Availability is a fixed, non-secret observation, never an inference from an
// error's text. Only Agenvo's explicit device_offline outcome means offline.
func Availability(err error) string {
	switch {
	case err == nil:
		return "online"
	case errors.Is(err, ErrAuthenticationUnavailable):
		return "authentication_unavailable"
	case errors.Is(err, ErrDeviceOffline):
		return "offline"
	case errors.Is(err, ErrNativeUnavailable):
		return "runtime_unavailable"
	default:
		return "connection_unavailable"
	}
}

func nativeAvailability(r Receipt) error {
	if r.Error == nil || (r.Execution != "not_started" && r.Execution != "rejected") {
		return nil
	}
	switch r.Error.Code {
	case "device_offline":
		return ErrDeviceOffline
	case "runtime_unavailable":
		return ErrNativeUnavailable
	default:
		return nil
	}
}

// These are allowlisted native metadata only. No cwd, tokens, worktree paths,
// terminal titles, output or native diagnostics are projected.
type WorkspaceObservation struct {
	ID          string `json:"workspace_id"`
	Label       string `json:"label"`
	PaneCount   int    `json:"pane_count"`
	AgentStatus string `json:"agent_status"`
}

type WorkspaceObservations struct {
	Availability string                 `json:"availability"`
	Workspaces   []WorkspaceObservation `json:"workspaces"`
	Truncated    bool                   `json:"truncated"`
}

// References must come from the owned operation, never from tool arguments.
type OperationReferences struct {
	Workspace, Pane, Terminal, ControlPane, ControlTerminal string
}

type ReferenceObservation struct {
	Availability string `json:"availability"`
	Workspace    string `json:"workspace"`
	Pane         string `json:"pane"`
	ControlPane  string `json:"control_pane"`
	AgentStatus  string `json:"agent_status"`
	Match        string `json:"reference_match"`
	Reason       string `json:"reason"`
}

func boundedNativeText(s string, limit int) bool {
	return len(s) <= limit && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}

func nativeState(s string) string {
	switch s {
	case "idle", "working", "blocked", "done", "unknown":
		return s
	default:
		return "unknown"
	}
}

func missingReference(method string, r Receipt) bool {
	if r.Error == nil || r.Error.Code != "native_error" || (r.Execution != "rejected" && r.Execution != "not_started") {
		return false
	}
	var cause struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(r.Error.Native, &cause) != nil {
		return false
	}
	return method == "pane.get" && cause.Code == "pane_not_found" || method == "agent.get" && (cause.Code == "agent_not_found" || cause.Code == "agent_name_not_found")
}

// readObservation has an explicit read-only method allowlist. It never calls
// pane.run/read, agent.start/prompt, create/close or an automatic retry.
func (c *Client) readObservation(ctx context.Context, scope Scope, target Target, method string, params map[string]any, out any) (string, error) {
	switch method {
	case "workspace.list", "workspace.get", "pane.get", "agent.get":
	default:
		return "unsupported", ErrUnsupported
	}
	r, callErr := c.relay(ctx, scope, target, method, params)
	current, err := c.resolveTarget(ctx, scope)
	if err != nil {
		return "authorization_changed", err
	}
	if current != target {
		return "authorization_changed", ErrTargetChanged
	}
	if callErr != nil {
		if missingReference(method, r) {
			return "native_reference_missing", errReferenceMissing
		}
		return "native_query_unavailable", callErr
	}
	var envelope struct {
		Session     string          `json:"session"`
		NativeError json.RawMessage `json:"nativeError"`
		Result      json.RawMessage `json:"result"`
	}
	if json.Unmarshal(r.Result, &envelope) != nil {
		return "native_response_unverified", ErrUnknown
	}
	if len(envelope.NativeError) != 0 && string(envelope.NativeError) != "null" {
		var failure Receipt
		if json.Unmarshal(envelope.NativeError, &failure) == nil && missingReference(method, failure) {
			return "native_reference_missing", errReferenceMissing
		}
		return "native_response_unverified", ErrUnknown
	}
	if envelope.Session != target.Session || len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return "native_response_unverified", ErrUnknown
	}
	if json.Unmarshal(envelope.Result, out) != nil {
		return "native_response_unverified", ErrUnknown
	}
	return "", nil
}

func (s *Service) Workspaces(ctx context.Context, scope Scope) (WorkspaceObservations, error) {
	out := WorkspaceObservations{Availability: "connection_unavailable", Workspaces: []WorkspaceObservation{}}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return out, err
	}
	var body struct {
		Workspaces []WorkspaceObservation `json:"workspaces"`
	}
	_, err = c.readObservation(ctx, scope, target, "workspace.list", map[string]any{"session": target.Session}, &body)
	if err != nil {
		out.Availability = Availability(err)
		return out, err
	}
	if body.Workspaces == nil {
		return out, ErrUnknown
	}
	seen := map[string]bool{}
	for _, w := range body.Workspaces {
		if w.ID == "" || !boundedNativeText(w.ID, 256) || !boundedNativeText(w.Label, 512) || w.PaneCount < 0 || seen[w.ID] {
			return out, ErrUnknown
		}
		seen[w.ID] = true
		w.AgentStatus = nativeState(w.AgentStatus)
		if len(out.Workspaces) < 100 {
			out.Workspaces = append(out.Workspaces, w)
		} else {
			out.Truncated = true
		}
	}
	out.Availability = "online"
	return out, nil
}

func (s *Service) ObserveReferences(ctx context.Context, scope Scope, refs OperationReferences) (ReferenceObservation, error) {
	out := ReferenceObservation{Availability: "connection_unavailable", Workspace: "unknown", Pane: "unknown", ControlPane: "unknown", AgentStatus: "unknown", Match: "unknown", Reason: "native_references_missing"}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return out, err
	}
	for _, id := range []string{refs.Workspace, refs.Pane, refs.Terminal, refs.ControlPane, refs.ControlTerminal} {
		if !boundedNativeText(id, 256) {
			out.Reason = "native_references_invalid"
			return out, ErrUnknown
		}
	}
	if refs.Workspace == "" || refs.Pane == "" || refs.Terminal == "" {
		return out, nil
	}
	var body struct {
		Workspaces []WorkspaceObservation `json:"workspaces"`
	}
	out.Reason, err = c.readObservation(ctx, scope, target, "workspace.list", map[string]any{"session": target.Session}, &body)
	if err != nil {
		out.Availability = Availability(err)
		return out, err
	}
	if body.Workspaces == nil {
		out.Reason = "native_response_unverified"
		return out, ErrUnknown
	}
	out.Availability, out.Workspace = "online", "missing"
	for _, w := range body.Workspaces {
		if w.ID == refs.Workspace {
			out.Workspace = "present"
		}
	}
	if out.Workspace == "missing" {
		out.Reason = "workspace_missing"
		return out, nil
	}
	checkPane := func(id, terminal string) (string, error) {
		var p struct {
			Pane *struct {
				ID        string `json:"pane_id"`
				Workspace string `json:"workspace_id"`
				Terminal  string `json:"terminal_id"`
			} `json:"pane"`
		}
		_, err := c.readObservation(ctx, scope, target, "pane.get", map[string]any{"session": target.Session, "paneId": id}, &p)
		if errors.Is(err, errReferenceMissing) {
			return "missing", nil
		}
		if err != nil || p.Pane == nil {
			return "unknown", err
		}
		if p.Pane.ID != id || p.Pane.Workspace != refs.Workspace || p.Pane.Terminal != terminal {
			return "mismatch", nil
		}
		return "present", nil
	}
	out.Pane, err = checkPane(refs.Pane, refs.Terminal)
	if err != nil || out.Pane != "present" {
		out.Reason = "execution_pane_unverified"
		if out.Pane == "missing" {
			out.Reason = "execution_pane_missing"
		}
		if out.Pane == "mismatch" {
			out.Match, out.Reason = "mismatch", "execution_pane_mismatch"
		}
		if err != nil {
			out.Availability = Availability(err)
		}
		return out, err
	}
	if refs.ControlPane != "" && refs.ControlTerminal != "" {
		out.ControlPane, err = checkPane(refs.ControlPane, refs.ControlTerminal)
		if err != nil || out.ControlPane != "present" {
			out.Reason = "control_pane_unverified"
			if out.ControlPane == "missing" {
				out.Reason = "control_pane_missing"
			}
			if out.ControlPane == "mismatch" {
				out.Match, out.Reason = "mismatch", "control_pane_mismatch"
			}
			if err != nil {
				out.Availability = Availability(err)
			}
			return out, err
		}
	}
	out.Match, out.Reason = "observed", "receipt_not_inferred_from_native_state"
	var agent struct {
		Agent *agentInfo `json:"agent"`
	}
	_, err = c.readObservation(ctx, scope, target, "agent.get", map[string]any{"session": target.Session, "name": refs.Pane}, &agent)
	if err != nil {
		out.Reason = "agent_metadata_unavailable"
		if errors.Is(err, errReferenceMissing) {
			out.Reason = "agent_missing"
			return out, nil
		}
		out.Availability = Availability(err)
		return out, err
	}
	if agent.Agent == nil || agent.Agent.WorkspaceID != refs.Workspace || agent.Agent.PaneID != refs.Pane || agent.Agent.TerminalID != refs.Terminal {
		out.Match, out.Reason = "mismatch", "agent_reference_mismatch"
		return out, ErrUnknown
	}
	out.AgentStatus = nativeState(agent.Agent.Status)
	if refs.ControlPane == "" || refs.ControlTerminal == "" {
		out.Reason = "control_reference_missing"
	}
	return out, nil
}
