// Package nativeherdr calls an existing Herdr server through its official CLI.
// It owns transport receipts only; business completion remains in the shared Memoh task ledger.
package nativeherdr

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/felinics/memoh/internal/agenvo"
)

var (
	sessionName     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// buildCommand implements the bounded subset used by the existing ledger. The
// returned values are argv, never shell fragments. Session selection is applied
// by the executor through the official HERDR_* environment, not model input.
// agent.start is intentionally unsupported: the old adapter's asynchronous
// "starting" receipt is not the official CLI's synchronous startup contract.
func buildCommand(method string, params map[string]any, target agenvo.Target) ([]string, error) {
	if !sessionName.MatchString(target.Session) || params["session"] != target.Session {
		return nil, agenvo.ErrInvalidTarget
	}
	allowed := map[string]bool{"session": true}
	str := func(key, fallback string, limit int, multiline bool) (string, bool) {
		allowed[key] = true
		v := fallback
		if raw, exists := params[key]; exists {
			var ok bool
			v, ok = raw.(string)
			if !ok {
				return "", false
			}
		}
		return v, v != "" && len(v) <= limit && utf8.ValidString(v) && !strings.ContainsRune(v, 0) && (multiline || !strings.ContainsAny(v, "\r\n"))
	}
	id := func(key string) (string, bool) {
		v, ok := str(key, "", 128, false)
		return v, ok && !strings.HasPrefix(v, "-")
	}
	var argv []string
	switch method {
	case "workspace.list", "pane.list", "agent.list":
		argv = strings.Split(method, ".")
	case "workspace.get", "workspace.close", "tab.get", "tab.close", "pane.get", "pane.close", "pane.process-info", "agent.get", "agent.explain":
		parts := strings.Split(method, ".")
		key := parts[0] + "Id"
		if parts[0] == "agent" {
			key = "name"
		}
		ref, ok := id(key)
		if !ok {
			return nil, agenvo.ErrUnsupported
		}
		argv = parts
		if method == "pane.process-info" {
			argv = append(argv, "--pane")
		}
		argv = append(argv, ref)
		if method == "agent.explain" {
			argv = append(argv, "--json")
		}
	case "workspace.create", "tab.create":
		cwd, ok := str("cwd", target.WorkDir, 4096, false)
		if !ok {
			return nil, agenvo.ErrUnsupported
		}
		label, ok := str("label", "Memoh", 128, false)
		if !ok {
			return nil, agenvo.ErrUnsupported
		}
		argv = strings.Split(method, ".")
		if method == "tab.create" {
			ref, ok := id("workspaceId")
			if !ok {
				return nil, agenvo.ErrUnsupported
			}
			argv = append(argv, "--workspace", ref)
		}
		argv = append(argv, "--cwd", cwd, "--label", label, "--no-focus")
		if method == "tab.create" {
			allowed["env"] = true
			env := map[string]string{}
			switch raw := params["env"].(type) {
			case nil:
			case map[string]string:
				env = raw
			case map[string]any:
				for k, v := range raw {
					s, ok := v.(string)
					if !ok {
						return nil, agenvo.ErrUnsupported
					}
					env[k] = s
				}
			default:
				return nil, agenvo.ErrUnsupported
			}
			if len(env) > 32 {
				return nil, agenvo.ErrUnsupported
			}
			keys := make([]string, 0, len(env))
			for k, v := range env {
				if !environmentName.MatchString(k) || len(v) > 4096 || !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
					return nil, agenvo.ErrUnsupported
				}
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				argv = append(argv, "--env", k+"="+env[k])
			}
		}
	case "pane.read", "agent.read":
		key := "paneId"
		if method == "agent.read" {
			key = "name"
		}
		ref, ok := id(key)
		if !ok {
			return nil, agenvo.ErrUnsupported
		}
		source, ok := str("source", "recent-unwrapped", 32, false)
		if !ok || (source != "visible" && source != "recent" && source != "recent-unwrapped" && source != "detection") {
			return nil, agenvo.ErrUnsupported
		}
		allowed["lines"] = true
		lines := 80
		if v, exists := params["lines"]; exists {
			switch n := v.(type) {
			case int:
				lines = n
			case float64:
				if n < 1 || n > 500 || n != float64(int(n)) {
					return nil, agenvo.ErrUnsupported
				}
				lines = int(n)
			case json.Number:
				x, err := strconv.Atoi(string(n))
				if err != nil {
					return nil, agenvo.ErrUnsupported
				}
				lines = x
			default:
				return nil, agenvo.ErrUnsupported
			}
		}
		if lines < 1 || lines > 500 {
			return nil, agenvo.ErrUnsupported
		}
		argv = append(strings.Split(method, "."), ref, "--lines", strconv.Itoa(lines), "--source", source)
	case "pane.run", "pane.send-text", "agent.prompt":
		key := "paneId"
		if method == "agent.prompt" {
			key = "name"
		}
		ref, ok := id(key)
		if !ok {
			return nil, agenvo.ErrUnsupported
		}
		key = "text"
		if method == "pane.run" {
			key = "command"
		}
		value, ok := str(key, "", 48000, true)
		if !ok {
			return nil, agenvo.ErrUnsupported
		}
		argv = append(strings.Split(method, "."), ref, value)
	case "pane.send-keys", "agent.send-keys":
		key := "paneId"
		if method == "agent.send-keys" {
			key = "name"
		}
		ref, ok := id(key)
		if !ok {
			return nil, agenvo.ErrUnsupported
		}
		allowed["keys"] = true
		var keys []string
		switch raw := params["keys"].(type) {
		case []string:
			keys = raw
		case []any:
			for _, item := range raw {
				k, ok := item.(string)
				if !ok {
					return nil, agenvo.ErrUnsupported
				}
				keys = append(keys, k)
			}
		default:
			return nil, agenvo.ErrUnsupported
		}
		if len(keys) < 1 || len(keys) > 64 {
			return nil, agenvo.ErrUnsupported
		}
		for _, key := range keys {
			if key == "" || len(key) > 64 || !utf8.ValidString(key) || strings.ContainsAny(key, "\x00\r\n") || strings.HasPrefix(key, "-") {
				return nil, agenvo.ErrUnsupported
			}
		}
		argv = append(append(strings.Split(method, "."), ref), keys...)
	default:
		return nil, agenvo.ErrUnsupported
	}
	for key := range params {
		if !allowed[key] {
			return nil, agenvo.ErrUnsupported
		}
	}
	// Herdr's global session parser scans every argv before dispatch, including
	// positional text. Refuse its selector syntax so text cannot switch sessions.
	for _, arg := range argv {
		if arg == "--session" || strings.HasPrefix(arg, "--session=") {
			return nil, agenvo.ErrUnsupported
		}
	}
	return argv, nil
}

func responseType(method string) string {
	switch method {
	case "workspace.list":
		return "workspace_list"
	case "workspace.create":
		return "workspace_created"
	case "workspace.get":
		return "workspace_info"
	case "tab.create":
		return "tab_created"
	case "tab.get":
		return "tab_info"
	case "pane.list":
		return "pane_list"
	case "pane.get":
		return "pane_info"
	case "pane.process-info":
		return "pane_process_info"
	case "agent.list":
		return "agent_list"
	case "agent.get":
		return "agent_info"
	case "agent.prompt":
		return "agent_prompted"
	case "agent.explain":
		return "agent_explain"
	case "workspace.close", "tab.close", "pane.close", "agent.send-keys":
		return "ok"
	case "pane.read", "agent.read":
		return "text"
	case "pane.run", "pane.send-text", "pane.send-keys":
		return "empty"
	default:
		return ""
	}
}

func responseID(method string) string {
	if responseType(method) == "empty" {
		return "cli:request"
	}
	return "cli:" + strings.ReplaceAll(strings.ReplaceAll(method, ".", ":"), "-", "_")
}

// parseReceipt never treats terminal text as JSON, even when it looks like a
// success envelope. A successful CLI exit acknowledges the native request only.
// The executor calls this only after receiving a complete, bounded EXIT stream.
func parseReceipt(method string, target agenvo.Target, exitCode int, stdout, stderr []byte) (agenvo.Receipt, error) {
	unknown := agenvo.Receipt{Execution: "unknown"}
	kind := responseType(method)
	if kind == "" {
		return agenvo.Receipt{}, agenvo.ErrUnsupported
	}
	if !sessionName.MatchString(target.Session) || len(stdout) > 128*1024 || len(stderr) > 128*1024 || !utf8.Valid(stdout) || !utf8.Valid(stderr) {
		return unknown, agenvo.ErrUnknown
	}
	if exitCode != 0 {
		var envelope struct {
			ID    string          `json:"id"`
			Error json.RawMessage `json:"error"`
		}
		var cause struct {
			Code string `json:"code"`
		}
		if len(bytes.TrimSpace(stdout)) != 0 || json.Unmarshal(stderr, &envelope) != nil || envelope.ID != responseID(method) || json.Unmarshal(envelope.Error, &cause) != nil || cause.Code == "" {
			return unknown, agenvo.ErrUnknown
		}
		// Native errors can occur after side effects (prompt timeout, PTY write
		// failure, etc). Only explicit pre-dispatch refusals are rejected.
		knownRejected := false
		switch cause.Code {
		case "workspace_not_found", "tab_not_found", "pane_not_found", "agent_not_found", "agent_name_not_found", "agent_not_ready", "agent_blocked", "empty_agent_prompt", "invalid_key", "agent_explain_unavailable", "server_not_running", "protocol_mismatch":
			knownRejected = true
		}
		r := unknown
		r.RequestID = envelope.ID
		// Retain only the fixed native code needed to recognize a missing
		// reference. Native diagnostics may contain host paths or prompt text.
		native, _ := json.Marshal(map[string]string{"code": cause.Code})
		r.Error = &struct {
			Code   string          `json:"code"`
			Native json.RawMessage `json:"native,omitempty"`
		}{Code: "native_error", Native: native}
		if knownRejected {
			r.Execution = "rejected"
			return r, agenvo.ErrRejected
		}
		return r, agenvo.ErrUnknown
	}
	// stderr on a supposedly successful command is not part of this pinned
	// contract. Preserve uncertainty instead of interpreting unexpected output.
	if len(bytes.TrimSpace(stderr)) != 0 {
		return unknown, agenvo.ErrUnknown
	}
	value := map[string]any{"session": target.Session}
	switch kind {
	case "text":
		value["output"] = string(stdout)
	case "empty":
		if len(bytes.TrimSpace(stdout)) != 0 {
			return unknown, agenvo.ErrUnknown
		}
		value["output"] = ""
	default:
		var envelope struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		var body struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(stdout, &envelope) != nil || envelope.ID != responseID(method) || len(envelope.Error) != 0 || json.Unmarshal(envelope.Result, &body) != nil || body.Type != kind {
			return unknown, agenvo.ErrUnknown
		}
		value["id"], value["result"] = envelope.ID, envelope.Result
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return unknown, agenvo.ErrUnknown
	}
	return agenvo.Receipt{Execution: "accepted", RequestID: responseID(method), Result: raw}, nil
}
