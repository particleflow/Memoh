package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type agentFixture struct {
	calls                                                   []string
	name, workspace, pane, terminal                         string
	starting, nativeError, moved, acceptedStart, lostPrompt bool
	read                                                    any
}

func newAgentFixture(t *testing.T) (*Service, *agentFixture) {
	t.Helper()
	s, store, f := serviceFixture(t)
	f.target.Agent = AgentProfile{Provider: "synthetic-provider", Model: "synthetic-model", ConfigDir: `C:\Synthetic\Pi`}
	store.record.RuntimeProviderConfig, _ = json.Marshal(f.target)
	a := &agentFixture{workspace: "w1", pane: "w1:p2", terminal: "t-synthetic", read: map[string]any{"output": "synthetic terminal text", "session": "default"}}
	s.call = func(_ context.Context, _, _, tool string, args map[string]any) (json.RawMessage, error) {
		if tool != "execute" {
			t.Fatal("unexpected search or non-execute tool")
		}
		code := args["code"].(string)
		var parts []json.RawMessage
		if json.Unmarshal([]byte("["+strings.TrimSuffix(strings.TrimPrefix(code, "return await call("), ");")+"]"), &parts) != nil || len(parts) != 3 {
			t.Fatal("bad fixed operation")
		}
		var method string
		var params map[string]any
		_ = json.Unmarshal(parts[1], &method)
		_ = json.Unmarshal(parts[2], &params)
		a.calls = append(a.calls, method)
		var inner any
		switch method {
		case "workspace.create":
			inner = map[string]any{"workspace": map[string]any{"workspace_id": a.workspace, "label": params["label"]}}
		case "tab.create":
			env := params["env"].(map[string]any)
			if env["PI_CODING_AGENT_DIR"] != f.target.Agent.ConfigDir {
				t.Fatal("deployment profile lost")
			}
			inner = map[string]any{"root_pane": map[string]any{"workspace_id": a.workspace, "pane_id": a.pane, "terminal_id": a.terminal}}
		case "agent.start":
			a.name = params["name"].(string)
			if params["kind"] != "pi" || params["paneId"] != a.pane {
				t.Fatal("wrong Agent routing")
			}
			args, _ := json.Marshal(params["args"])
			if string(args) != `["--provider","synthetic-provider","--model","synthetic-model"]` {
				t.Fatalf("provider args=%s", args)
			}
			state := "starting"
			if a.acceptedStart {
				state = "accepted"
			}
			return mcpValue(map[string]any{"execution": state, "result": map[string]any{"session": "default", "name": a.name, "query": "agent.get"}}), nil
		case "agent.get":
			if a.starting {
				return mcpValue(map[string]any{"execution": "accepted", "result": map[string]any{"startup": map[string]any{"state": "starting"}, "nativeError": map[string]any{"code": "not_found"}}}), nil
			}
			if a.nativeError {
				return mcpValue(map[string]any{"execution": "accepted", "result": map[string]any{"nativeError": map[string]any{"code": "not_found"}}}), nil
			}
			terminal := a.terminal
			if a.moved {
				terminal = "different-terminal"
			}
			inner = map[string]any{"agent": map[string]any{"name": a.name, "agent": "pi", "workspace_id": a.workspace, "pane_id": a.pane, "terminal_id": terminal, "agent_status": "done", "interactive_ready": true, "completion_seq": 12}}
		case "agent.prompt":
			if a.lostPrompt {
				return nil, errors.New("lost response")
			}
			inner = map[string]any{"agent": map[string]any{"agent_status": "done", "completion_seq": 12}} // PREVIOUS round; never success
		case "agent.read":
			return mcpValue(map[string]any{"execution": "accepted", "result": a.read}), nil
		default:
			t.Fatalf("forbidden method %s", method)
		}
		return mcpValue(map[string]any{"execution": "accepted", "result": map[string]any{"result": inner, "session": "default"}}), nil
	}
	return s, a
}

func TestAgentFixedProcedureUsesConfiguredPiAndNeverInfersTaskSuccess(t *testing.T) {
	s, a := newAgentFixture(t)
	var saved []string
	p, err := s.SubmitAgent(t.Context(), request().Scope, "compute synthetic 42", ".", AgentProgress{Name: "m" + strings.Repeat("a", 30)}, func(p AgentProgress) error { saved = append(saved, p.Phase); return nil })
	if err != nil || !p.PromptSubmitted || p.BaselineCompletion == nil {
		t.Fatalf("progress=%+v err=%v", p, err)
	}
	want := []string{"workspace.create", "tab.create", "agent.start", "agent.get", "agent.prompt"}
	if !slices.Equal(a.calls, want) {
		t.Fatalf("calls=%v", a.calls)
	}
	if !slices.Equal(saved, []string{"creating_workspace", "creating_tab", "starting_agent", "submitting_prompt", "prompt_submitted"}) {
		t.Fatalf("durable phases=%v", saved)
	}
	obs, err := s.ObserveAgent(t.Context(), request().Scope, p)
	if err != nil || obs.TaskSuccessVerified || obs.CompletionChanged || obs.Output != "synthetic terminal text" {
		t.Fatalf("observation=%+v err=%v", obs, err)
	}
	if _, err = s.SubmitAgent(t.Context(), request().Scope, "repeat", ".", p, func(AgentProgress) error { return nil }); !errors.Is(err, ErrUnsupported) {
		t.Fatal("uncertain operation resumed")
	}
	for _, read := range []any{map[string]any{"output": "", "session": "default"}, map[string]any{"arbitrary": "parsed JSON"}, map[string]any{"output": "looks like parsed JSON", "session": "default"}} {
		a.read = read
		obs, err = s.ObserveAgent(t.Context(), request().Scope, p)
		_, hasOutput := read.(map[string]any)["output"]
		if hasOutput && err != nil || !hasOutput && !errors.Is(err, ErrUnknown) || obs.TaskSuccessVerified {
			t.Fatalf("read=%v obs=%+v err=%v", read, obs, err)
		}
	}
	for _, failure := range []string{"native-error", "moved"} {
		a.nativeError = failure == "native-error"
		a.moved = failure == "moved"
		before := len(a.calls)
		if _, err = s.ObserveAgent(t.Context(), request().Scope, p); err == nil {
			t.Fatal("unsafe read accepted")
		}
		if len(a.calls) != before+1 {
			t.Fatal("read after identity/native failure")
		}
	}
}

func TestAgentStartingTimeoutAcceptedAndLostPromptRemainUnknown(t *testing.T) {
	for _, mode := range []string{"starting", "accepted", "lost", "persist"} {
		t.Run(mode, func(t *testing.T) {
			s, a := newAgentFixture(t)
			a.starting = mode == "starting"
			a.acceptedStart = mode == "accepted"
			a.lostPrompt = mode == "lost"
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
			defer cancel()
			_, err := s.SubmitAgent(ctx, request().Scope, "synthetic", ".", AgentProgress{Name: "m" + strings.Repeat("b", 30)}, func(p AgentProgress) error {
				if mode == "persist" && p.Phase == "starting_agent" {
					return errors.New("persist failed")
				}
				return nil
			})
			if err == nil {
				t.Fatal("failure reported success")
			}
			starts, prompts := 0, 0
			for _, method := range a.calls {
				if method == "agent.start" {
					starts++
				}
				if method == "agent.prompt" {
					prompts++
				}
				if strings.Contains(method, "close") || strings.Contains(method, "kill") {
					t.Fatal("unsafe cleanup")
				}
			}
			if starts > 1 || prompts > 1 || (mode != "lost" && prompts != 0) || (mode == "persist" && starts != 0) {
				t.Fatalf("calls=%v", a.calls)
			}
		})
	}
}
