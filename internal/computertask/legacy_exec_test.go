package computertask

import (
	"context"
	"errors"
	"testing"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/agenvo"
	workspacepkg "github.com/felinics/memoh/internal/workspace"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

type legacyTaskBridge struct {
	calls int
}

func (*legacyTaskBridge) MCPClient(context.Context, string) (*bridge.Client, error) {
	return bridge.UnsupportedClient(), nil
}

func (*legacyTaskBridge) WorkspaceInfo(context.Context, string) (bridge.WorkspaceInfo, error) {
	return bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendContainer, DefaultWorkDir: "/data"}, nil
}

func (*legacyTaskBridge) ListWorkspaceTargets(context.Context, string) ([]workspacepkg.WorkspaceTarget, error) {
	return nil, nil
}

func (p *legacyTaskBridge) ResolveWorkspaceTarget(_ context.Context, _ string, id string) (workspacepkg.ResolvedWorkspaceTarget, error) {
	return workspacepkg.ResolvedWorkspaceTarget{TargetID: id, Kind: "remote", Client: bridge.UnsupportedClient(), Info: bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendRemote, OS: "windows", DefaultWorkDir: `C:\Synthetic`}, Execute: func(context.Context, agenvo.Request) (agenvo.Result, error) {
		p.calls++
		return agenvo.Result{State: "not_started"}, agenvo.ErrComputerTaskRequired
	}}, nil
}

func TestStandardMCPUnknownCannotBeRetriedThroughLegacyExec(t *testing.T) {
	source, _, f := setup(t)
	f.lost = true
	c := standardClient(t, source, "pc-a", false)
	receipt := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": "printf 42"}, false))
	if receipt.State != "unknown" {
		t.Fatal(receipt.State)
	}
	// A NAS-scoped caller is allowed to target this PC but cannot bypass the
	// task ledger using the formerly supported ordinary exec tool.
	bridge := &legacyTaskBridge{}
	provider := tools.NewContainerProvider(nil, bridge, nil, "/data")
	registered, err := provider.Tools(t.Context(), tools.SessionContext{BotID: "bot-a", UserID: "user-a", SessionID: "session-a", WorkspaceTargetID: "native"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range registered {
		if tool.Name != "exec" {
			continue
		}
		found = true
		_, err = tool.Execute(&toolexec.ToolExecContext{Context: t.Context()}, toolexec.ArgumentsFromValue(map[string]any{"target_id": "pc-a", "command": "printf 42", "work_dir": "."}))
		if !errors.Is(err, agenvo.ErrComputerTaskRequired) {
			t.Fatal("old exec bypassed ledger", err)
		}
	}
	if !found || bridge.calls != 1 || f.runs != 1 {
		t.Fatalf("found=%v old-route=%d submissions=%d", found, bridge.calls, f.runs)
	}
}
