package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/agenvo"
	workspacepkg "github.com/felinics/memoh/internal/workspace"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

// The old direct execution route must not bypass the durable task ledger.
// Root-relative path behavior is tested through computer_task_submit instead.
func TestAgenvoLegacyExecCannotBypassTaskLedger(t *testing.T) {
	for _, selected := range []bool{false, true} {
		clients := &containerTestTargetProvider{resolved: workspacepkg.ResolvedWorkspaceTarget{
			TargetID: "pc-a", Kind: "remote", Client: bridge.UnsupportedClient(),
			Info: bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendAgenvo, OS: "windows", DefaultWorkDir: `C:\Synthetic`},
			Execute: func(context.Context, agenvo.Request) (agenvo.Result, error) {
				t.Fatal("legacy exec reached relay")
				return agenvo.Result{}, nil
			},
		}}
		if selected {
			clients.info = clients.resolved.Info
		} else {
			clients.info = bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendContainer, DefaultWorkDir: "/data"}
		}
		provider := NewContainerProvider(nil, clients, nil, "/data")
		session := SessionContext{BotID: "bot", UserID: "caller", WorkspaceTargetID: "native"}
		if selected {
			session.WorkspaceTargetID = "pc-a"
			session.WorkspaceTargetKind = "remote"
		}
		registered, err := provider.Tools(t.Context(), session)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, tool := range registered {
			if tool.Name != ToolExec().String() {
				continue
			}
			found = true
			for _, cwd := range []string{".", "child", `C:\Synthetic`, "../escape"} {
				_, err = tool.Execute(&toolexec.ToolExecContext{Context: t.Context()}, toolexec.ArgumentsFromValue(map[string]any{"target_id": "pc-a", "command": "printf 42", "work_dir": cwd}))
				if !errors.Is(err, agenvo.ErrComputerTaskRequired) {
					t.Fatalf("legacy cwd %q: %v", cwd, err)
				}
			}
		}
		if selected && found {
			t.Fatal("PC advertised unledgered exec")
		}
		if !selected && !found {
			t.Fatal("NAS native exec disappeared")
		}
	}
}
