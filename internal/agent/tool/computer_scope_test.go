package tools

import (
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/toolcontext"
	workspacepkg "github.com/felinics/memoh/internal/workspace"
)

func TestComputerSessionDiscoveryDoesNotExposeOtherMachines(t *testing.T) {
	for _, selected := range []string{"native", "pc-a"} {
		t.Run(selected, func(t *testing.T) {
			clients := &containerTestTargetProvider{targets: []workspacepkg.WorkspaceTarget{
				{TargetID: "native", Kind: "native", Name: "Server Workspace", Status: "online"},
				{TargetID: "pc-a", Kind: "remote", Name: "Synthetic PC A", Status: "online"},
				{TargetID: "pc-b", Kind: "remote", Name: "Synthetic PC B", Status: "online"},
			}}
			provider := NewContainerProvider(nil, clients, nil, "")
			list, err := provider.Tools(t.Context(), SessionContext{BotID: "synthetic-bot", WorkspaceTargetID: selected})
			if err != nil {
				t.Fatal(err)
			}
			output, err := toolByNameForTest(t, list, ToolListExecutionLocations()).Execute(&toolexec.ToolExecContext{Context: t.Context()}, toolexec.ArgumentsFromValue(nil))
			if err != nil {
				t.Fatal(err)
			}
			locations := decodeListExecutionLocations(t, output).Locations
			if selected == "native" {
				if len(locations) != 3 {
					t.Fatal("NAS session lost its Bot-authorized target inventory")
				}
			} else if len(locations) != 1 || locations[0].TargetID != selected {
				t.Fatalf("PC session exposed another machine: %+v", locations)
			}
		})
	}
}

func TestComputerScopeSurvivesMCPRoundTrip(t *testing.T) {
	want := SessionContext{BotID: "synthetic-bot", WorkspaceTargetID: "pc-a", WorkspaceTargetKind: "remote", WorkspaceTargetName: "Synthetic A", WorkdirPath: "/synthetic/project"}
	got := sessionFromMCP(toMCPSession(want))
	if got.WorkspaceTargetID != want.WorkspaceTargetID || got.WorkspaceTargetKind != want.WorkspaceTargetKind || got.WorkspaceTargetName != want.WorkspaceTargetName || got.WorkdirPath != want.WorkdirPath {
		t.Fatal("MCP transport lost server-resolved workspace scope")
	}
	public := toMCPSession(want)
	public.PublicRequest = true
	got = sessionFromMCP(public)
	if got.WorkspaceTargetID != "" || got.WorkdirPath != "" {
		t.Fatal("public routing input became computer authority")
	}
}

func TestComputerScopeRejectsGenericCapabilityManagement(t *testing.T) {
	p := &CapabilityProvider{}
	session := SessionContext{BotID: "bot", WorkspaceTargetID: "pc-a"}
	tools, err := p.Tools(t.Context(), session)
	if err != nil || len(tools) != 0 {
		t.Fatal("PC exposed global connection management")
	}
	for _, spec := range capabilitySpecs() {
		if _, err = p.execute(&toolexec.ToolExecContext{Context: t.Context()}, session, spec, map[string]any{"action": "list"}); !errors.Is(err, toolcontext.ErrComputerScope) {
			t.Fatal("stale capability descriptor escaped scope")
		}
	}
}

func TestPCSessionCannotUseServerBrowserOrComputer(t *testing.T) {
	provider := &BrowserProvider{}
	session := SessionContext{BotID: "synthetic-bot", WorkspaceTargetID: "pc-a"}
	list, err := provider.Tools(t.Context(), session)
	if err != nil || len(list) != 0 {
		t.Fatalf("PC session exposed Server Workspace desktop tools: %v", err)
	}
	for _, contextual := range []bool{false, true} {
		ctx := t.Context()
		sess := session
		if contextual {
			ctx = toolcontext.WithComputerScope(ctx, "pc-a")
			sess.WorkspaceTargetID = "" // stale descriptor cannot erase run scope
		}
		for _, tool := range provider.browserTools(sess) {
			args := map[string]any{"observe": "screenshot"}
			switch tool.Name {
			case ToolBrowserAction().String():
				args = map[string]any{"action": "navigate", "url": "https://example.invalid"}
			case ToolComputerAction().String():
				args = map[string]any{"action": "click", "x": 1, "y": 1}
			case ToolBrowserRemoteSession().String():
				args = map[string]any{"action": "status"}
			}
			_, err := tool.Execute(&toolexec.ToolExecContext{Context: ctx}, toolexec.ArgumentsFromValue(args))
			if !errors.Is(err, toolcontext.ErrComputerScope) {
				t.Errorf("%s did not enforce PC scope: %v", tool.Name, err)
			}
		}
	}
}

func TestComputerSessionRejectsCrossTargetBeforeResolution(t *testing.T) {
	for _, requested := range []string{"pc-b", "native"} {
		t.Run(requested, func(t *testing.T) {
			clients := &containerTestTargetProvider{}
			provider := NewContainerProvider(nil, clients, nil, "")
			_, err := provider.resolveToolTarget(t.Context(), SessionContext{BotID: "synthetic-bot", WorkspaceTargetID: "pc-a"}, requested)
			if err == nil || clients.resolvedInput != "" {
				t.Fatalf("cross-target call reached resolver: target=%q error=%v", clients.resolvedInput, err)
			}
		})
	}
}

func TestComputerSessionExecToolRejectsCrossTargetBeforeResolution(t *testing.T) {
	for _, requested := range []string{"pc-b", "native"} {
		t.Run(requested, func(t *testing.T) {
			clients := &containerTestTargetProvider{}
			provider := NewContainerProvider(nil, clients, nil, "")
			tools, err := provider.Tools(t.Context(), SessionContext{BotID: "synthetic-bot", WorkspaceTargetID: "pc-a"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = toolByNameForTest(t, tools, ToolExec()).Execute(&toolexec.ToolExecContext{Context: t.Context()}, toolexec.ArgumentsFromValue(map[string]any{
				"command": "printf synthetic", "target_id": requested,
			}))
			if !errors.Is(err, toolcontext.ErrComputerScope) || clients.resolvedInput != "" {
				t.Fatalf("exec crossed session scope: resolved=%q err=%v", clients.resolvedInput, err)
			}
		})
	}
}
