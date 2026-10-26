package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
)

// All receipts below are synthetic official CLI envelopes. This fixture has
// no Runtime connection, Herdr process, OAuth credentials, or model provider.
type batchResumeRelayFixture struct {
	t        *testing.T
	service  *Service
	progress BatchProgress
	target   Target
	tab      bool
	calls    []string
	counts   map[string]int
	writes   []string
	saves    []string
	failSave string
	failCall string
	tweak    func(string, int, map[string]any)
}

func newBatchResumeRelayFixture(t *testing.T) *batchResumeRelayFixture {
	t.Helper()
	s, store, mcp := serviceFixture(t)
	profile := syntheticNativeProfile()
	profile.WorkDir = `C:\Synthetic`
	store.sourceAllowed = false
	store.record.RuntimeID, store.record.RuntimeProvider = syntheticNativeID, "memoh"
	store.record.RuntimeProviderConfig, _ = json.Marshal(map[string]any{"herdr": profile})
	target, err := profile.target(syntheticNativeID)
	if err != nil {
		t.Fatal(err)
	}
	p := batchFixtureProgress()
	p.WorkspaceID, p.PaneID, p.TerminalID = "", "", ""
	p.Phase = "batch_creating_workspace"
	f := &batchResumeRelayFixture{t: t, service: s, target: target, progress: p, counts: map[string]int{}}
	s.SetNativeCaller(f.call)
	t.Cleanup(func() {
		if len(mcp.calls) != 0 {
			t.Fatal("recovery used MCP/OAuth")
		}
	})
	return f
}

func (f *batchResumeRelayFixture) pane(pi bool) map[string]any {
	id, terminal, tab := "control", "control-terminal", "control-tab"
	if pi {
		id, terminal, tab = "job", "job-terminal", "job-tab"
	}
	return map[string]any{"pane_id": id, "workspace_id": "w-synthetic", "terminal_id": terminal, "tab_id": tab, "cwd": f.target.WorkDir}
}

func (f *batchResumeRelayFixture) call(_ context.Context, _ Scope, target Target, method string, params map[string]any) (Receipt, error) {
	f.calls = append(f.calls, method)
	f.counts[method]++
	if target != f.target || params["session"] != f.target.Session {
		f.t.Fatal("target or session changed")
	}
	count := 1
	if f.tab {
		count = 2
	}
	ws := map[string]any{"workspace_id": "w-synthetic", "label": "memoh-batch-" + f.progress.Attempt, "active_tab_id": "control-tab", "tab_count": count, "pane_count": count}
	var body map[string]any
	switch method {
	case "workspace.list":
		body = map[string]any{"type": "workspace_list", "workspaces": []any{ws}}
	case "workspace.get":
		if params["workspaceId"] != "w-synthetic" {
			f.t.Fatal("guessed workspace reference")
		}
		body = map[string]any{"type": "workspace_info", "workspace": ws}
	case "pane.list":
		body = map[string]any{"type": "pane_list", "panes": []any{f.pane(false)}}
	case "agent.list":
		body = map[string]any{"type": "agent_list", "agents": []any{}}
	case "pane.get":
		id := params["paneId"]
		if id != "control" && id != "job" {
			f.t.Fatal("guessed pane reference")
		}
		body = map[string]any{"type": "pane_info", "pane": f.pane(id == "job")}
	case "tab.create":
		f.writes = append(f.writes, method)
		if f.saves[len(f.saves)-1] != "batch_creating_tab" || params["workspaceId"] != "w-synthetic" || params["cwd"] != f.target.WorkDir || params["label"] != "memoh-pi-"+f.progress.Attempt || !reflect.DeepEqual(params["env"], map[string]string{"PI_CODING_AGENT_DIR": f.target.Agent.ConfigDir}) {
			f.t.Fatal("tab write bypassed checkpoint or changed invocation settings")
		}
		f.tab = true
		body = map[string]any{"type": "tab_created", "tab": map[string]any{"tab_id": "job-tab", "workspace_id": "w-synthetic", "label": params["label"]}, "root_pane": f.pane(true)}
	case "pane.run":
		f.writes = append(f.writes, method)
		if f.saves[len(f.saves)-1] != "batch_submitting" || params["paneId"] != "job" {
			f.t.Fatal("prompt write bypassed checkpoint or changed pane")
		}
		expected := f.progress
		expected.WorkspaceID, expected.PaneID, expected.TerminalID = "w-synthetic", "job", "job-terminal"
		script, err := batchScript(expected, target, target.WorkDir, "same synthetic task")
		if err != nil || params["command"] != launchBatchScript(script, target) {
			f.t.Fatal("resume changed the original batch script/identity")
		}
		body = map[string]any{"type": "ok"}
	default:
		f.t.Fatalf("unexpected operation: %s", method)
	}
	if f.failCall == method {
		return Receipt{}, ErrUnknown
	}
	envelope := map[string]any{"session": target.Session, "result": body}
	if f.tweak != nil {
		f.tweak(method, f.counts[method], envelope)
	}
	raw, _ := json.Marshal(envelope)
	return Receipt{Execution: "accepted", RequestID: "synthetic-cli-receipt", Result: raw}, nil
}

func (f *batchResumeRelayFixture) save(p BatchProgress) error {
	f.saves = append(f.saves, p.Phase)
	if f.failSave == p.Phase {
		return errors.New("synthetic uncertain persistence")
	}
	return nil
}

func (f *batchResumeRelayFixture) adopt() BatchProgress {
	f.t.Helper()
	p, err := f.service.AdoptBatchWorkspace(f.t.Context(), request().Scope, "", f.progress)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func TestAdoptBatchWorkspaceOnlyDiscoversExactOriginalIdentity(t *testing.T) {
	f := newBatchResumeRelayFixture(t)
	p := f.adopt()
	want := f.progress
	want.Phase, want.WorkspaceID, want.ControlPane, want.ControlTerminal = "batch_workspace_adopted", "w-synthetic", "control", "control-terminal"
	if !reflect.DeepEqual(p, want) || len(f.writes) != 0 || !slices.Equal(f.calls, []string{"workspace.list", "workspace.get", "pane.list", "pane.get", "agent.list", "workspace.get"}) {
		t.Fatalf("identity or read-only sequence changed: %+v calls=%v", p, f.calls)
	}
}

func TestAdoptBatchWorkspaceRejectsAmbiguousChangedOrInvalidEvidence(t *testing.T) {
	for _, mode := range []string{"duplicate-label", "duplicate-workspace", "missing-list", "missing-match", "wrong-session", "wrong-kind", "invalid-id", "duplicate-pane", "missing-pane", "wrong-cwd", "wrong-tab", "changed-terminal", "changed-topology", "existing-agent", "missing-agents"} {
		t.Run(mode, func(t *testing.T) {
			f := newBatchResumeRelayFixture(t)
			f.tweak = func(method string, count int, envelope map[string]any) {
				body := envelope["result"].(map[string]any)
				if method == "workspace.list" {
					list := body["workspaces"].([]any)
					ws := list[0].(map[string]any)
					switch mode {
					case "duplicate-label":
						other := map[string]any{"workspace_id": "other", "label": ws["label"], "active_tab_id": "other-tab", "tab_count": 1, "pane_count": 1}
						body["workspaces"] = append(list, other)
					case "duplicate-workspace":
						body["workspaces"] = append(list, ws)
					case "missing-list":
						delete(body, "workspaces")
					case "missing-match":
						ws["label"] = "foreign-label"
					case "wrong-session":
						envelope["session"] = "foreign"
					case "wrong-kind":
						body["type"] = "workspace_info"
					case "invalid-id":
						ws["workspace_id"] = "-ambiguous"
					}
				}
				if method == "pane.list" {
					list := body["panes"].([]any)
					pane := list[0].(map[string]any)
					switch mode {
					case "duplicate-pane":
						body["panes"] = append(list, pane)
					case "missing-pane":
						body["panes"] = []any{}
					case "wrong-cwd":
						pane["cwd"] = `C:\Other`
					case "wrong-tab":
						pane["tab_id"] = "foreign-tab"
					}
				}
				if method == "pane.get" && mode == "changed-terminal" {
					body["pane"].(map[string]any)["terminal_id"] = "replacement"
				}
				if method == "workspace.get" && count == 2 && mode == "changed-topology" {
					body["workspace"].(map[string]any)["pane_count"] = 2
				}
				if method == "agent.list" {
					switch mode {
					case "existing-agent":
						body["agents"] = []any{map[string]any{"agent_id": "already-running"}}
					case "missing-agents":
						delete(body, "agents")
					}
				}
			}
			p, err := f.service.AdoptBatchWorkspace(t.Context(), request().Scope, "", f.progress)
			if err == nil || !reflect.DeepEqual(p, f.progress) || len(f.writes) != 0 {
				t.Fatalf("accepted invalid discovery: progress=%+v err=%v writes=%v", p, err, f.writes)
			}
		})
	}
}

func TestContinueBatchDurableStagesAndSameInvocation(t *testing.T) {
	for _, adoptedTab := range []bool{false, true} {
		t.Run(map[bool]string{false: "workspace", true: "tab"}[adoptedTab], func(t *testing.T) {
			f := newBatchResumeRelayFixture(t)
			p := f.adopt()
			if adoptedTab {
				f.tab = true
				p.Phase, p.PaneID, p.TerminalID = "batch_tab_adopted", "job", "job-terminal"
			}
			got, err := f.service.ContinueBatch(t.Context(), request().Scope, "same synthetic task", "", p, f.save)
			want := p
			want.Phase, want.PaneID, want.TerminalID, want.RequestID = "batch_submitted", "job", "job-terminal", "synthetic-cli-receipt"
			writes, saves := []string{"tab.create", "pane.run"}, []string{"batch_creating_tab", "batch_tab_adopted", "batch_submitting", "batch_submitted"}
			if adoptedTab {
				writes, saves = writes[1:], saves[2:]
			}
			if err != nil || !reflect.DeepEqual(got, want) || !slices.Equal(f.writes, writes) || !slices.Equal(f.saves, saves) {
				t.Fatalf("progress=%+v err=%v writes=%v saves=%v", got, err, f.writes, f.saves)
			}
		})
	}
}

func TestContinueBatchDoesNotReplayAfterSaveOrTransportUncertainty(t *testing.T) {
	for _, tc := range []struct {
		name, failSave, failCall, phase string
		writes                          []string
	}{
		{"before-tab", "batch_creating_tab", "", "batch_creating_tab", nil},
		{"after-tab", "batch_tab_adopted", "", "batch_creating_tab", []string{"tab.create"}},
		{"before-prompt", "batch_submitting", "", "batch_submitting", []string{"tab.create"}},
		{"after-prompt", "batch_submitted", "", "batch_submitting", []string{"tab.create", "pane.run"}},
		{"lost-tab", "", "tab.create", "batch_creating_tab", []string{"tab.create"}},
		{"lost-prompt", "", "pane.run", "batch_submitting", []string{"tab.create", "pane.run"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBatchResumeRelayFixture(t)
			p := f.adopt()
			f.failSave, f.failCall = tc.failSave, tc.failCall
			got, err := f.service.ContinueBatch(t.Context(), request().Scope, "same synthetic task", "", p, f.save)
			if err == nil || got.Phase != tc.phase || !slices.Equal(f.writes, tc.writes) {
				t.Fatalf("wrong uncertainty boundary: phase=%s err=%v writes=%v", got.Phase, err, f.writes)
			}
			before := len(f.calls)
			if _, err := f.service.ContinueBatch(t.Context(), request().Scope, "same synthetic task", "", got, f.save); !errors.Is(err, ErrUnsupported) || len(f.calls) != before {
				t.Fatal("uncertain stage was reentered")
			}
		})
	}
}

func TestContinueBatchRejectsChangedSavedPiTerminalBeforeDispatch(t *testing.T) {
	f := newBatchResumeRelayFixture(t)
	p := f.adopt()
	f.tab = true
	p.Phase, p.PaneID, p.TerminalID = "batch_tab_adopted", "job", "previous-job-terminal"
	got, err := f.service.ContinueBatch(t.Context(), request().Scope, "same synthetic task", "", p, f.save)
	if !errors.Is(err, ErrTargetChanged) || !reflect.DeepEqual(got, p) || len(f.writes) != 0 || len(f.saves) != 0 {
		t.Fatalf("changed terminal accepted: err=%v writes=%v saves=%v", err, f.writes, f.saves)
	}
}

func TestContinueBatchRechecksNewPiTerminalAfterSavingItsReceipt(t *testing.T) {
	f := newBatchResumeRelayFixture(t)
	p := f.adopt()
	f.tweak = func(method string, _ int, envelope map[string]any) {
		if method == "pane.get" {
			pane := envelope["result"].(map[string]any)["pane"].(map[string]any)
			if pane["pane_id"] == "job" {
				pane["terminal_id"] = "replacement-terminal"
			}
		}
	}
	got, err := f.service.ContinueBatch(t.Context(), request().Scope, "same synthetic task", "", p, f.save)
	if !errors.Is(err, ErrTargetChanged) || got.Phase != "batch_tab_adopted" || !slices.Equal(f.writes, []string{"tab.create"}) {
		t.Fatalf("new terminal was not rechecked before input: phase=%s err=%v writes=%v", got.Phase, err, f.writes)
	}
	if _, err = f.service.ContinueBatch(t.Context(), request().Scope, "same synthetic task", "", got, f.save); !errors.Is(err, ErrTargetChanged) || !slices.Equal(f.writes, []string{"tab.create"}) {
		t.Fatal("changed terminal allowed a replay")
	}
}

func TestBatchResumeWindowsCWDComparisonDoesNotBroadenTheDirectory(t *testing.T) {
	for _, tc := range []struct {
		cwd  string
		want bool
	}{
		{`c:/synthetic`, true},
		{`C:\Synthetic\.\`, true},
		{`C:\Synthetic\child\..`, true},
		{`C:\Synthetic\child`, false},
		{`C:\Synthetic-other`, false},
		{`C:\Synthetic\..\Other`, false},
		{`D:\Synthetic`, false},
		{`\\localhost\c$\Synthetic`, false},
		{`C:\Synthetic:stream`, false},
		{`C:Synthetic`, false},
		{`C:\Synthetic.`, false},
	} {
		if got := sameBatchResumeCWD(tc.cwd, `C:\Synthetic`); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.cwd, got, tc.want)
		}
	}
	f := newBatchResumeRelayFixture(t)
	f.tweak = func(method string, _ int, envelope map[string]any) {
		if method == "pane.get" {
			envelope["result"].(map[string]any)["pane"].(map[string]any)["cwd"] = `c:/synthetic`
		}
	}
	// pane.list and pane.get may use different Windows spellings for one path.
	_ = f.adopt()
}

func TestContinueBatchChecksForExistingAgentsBeforeEveryFirstPrompt(t *testing.T) {
	for _, afterTab := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-tab", true: "after-tab"}[afterTab], func(t *testing.T) {
			f := newBatchResumeRelayFixture(t)
			p := f.adopt()
			f.tweak = func(method string, _ int, envelope map[string]any) {
				if method == "agent.list" && (!afterTab || f.tab) {
					envelope["result"].(map[string]any)["agents"] = []any{map[string]any{"agent_id": "existing-agent"}}
				}
			}
			_, err := f.service.ContinueBatch(t.Context(), request().Scope, "same synthetic task", "", p, f.save)
			if err == nil || f.counts["pane.run"] != 0 || (!afterTab && f.counts["tab.create"] != 0) {
				t.Fatalf("submitted with an existing agent: err=%v writes=%v", err, f.writes)
			}
		})
	}
}

func TestContinueBatchAcceptsOnlyTheTwoVerifiedTabsAsActive(t *testing.T) {
	for _, active := range []string{"control-tab", "job-tab", "unrelated-tab"} {
		t.Run(active, func(t *testing.T) {
			f := newBatchResumeRelayFixture(t)
			p := f.adopt()
			f.tab = true
			p.Phase, p.PaneID, p.TerminalID = "batch_tab_adopted", "job", "job-terminal"
			f.tweak = func(method string, _ int, envelope map[string]any) {
				if method == "workspace.get" {
					envelope["result"].(map[string]any)["workspace"].(map[string]any)["active_tab_id"] = active
				}
			}
			_, err := f.service.ContinueBatch(t.Context(), request().Scope, "same synthetic task", "", p, f.save)
			if active == "unrelated-tab" {
				if !errors.Is(err, ErrTargetChanged) || len(f.writes) != 0 {
					t.Fatal("unverified active tab accepted", err)
				}
			} else if err != nil || !slices.Equal(f.writes, []string{"pane.run"}) {
				t.Fatal("valid focus change broke saved identity", err)
			}
		})
	}
}
