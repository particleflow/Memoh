package core

import (
	"context"
	"errors"
	"testing"

	approval "github.com/felinics/memoh/internal/agent/decision/approval"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace"
)

type computerPermissions struct{ denied string }

func (p computerPermissions) HasBotPermission(_ context.Context, _, _, permission string) (bool, error) {
	return permission != p.denied, nil
}

type unscopedConnectorFixture struct{ lists, calls int }

func (f *unscopedConnectorFixture) ListTools(context.Context, mcp.ToolSessionContext) ([]mcp.ToolDescriptor, error) {
	f.lists++
	return []mcp.ToolDescriptor{{Name: "connector"}}, nil
}

func (f *unscopedConnectorFixture) CallTool(context.Context, mcp.ToolSessionContext, string, map[string]any) (map[string]any, error) {
	f.calls++
	return map[string]any{"ok": true}, nil
}

func TestComputerScopeBlocksUnscopedConnectorCatalogAndStaleCall(t *testing.T) {
	f := &unscopedConnectorFixture{}
	s := computerScopedConnectorSource{f}
	pc := mcp.ToolSessionContext{WorkspaceTargetID: "pc-a"}
	items, err := s.ListTools(t.Context(), pc)
	if err != nil || len(items) != 0 {
		t.Fatal("PC session exposed an unscoped connector")
	}
	if _, err := s.CallTool(t.Context(), pc, "connector", nil); !errors.Is(err, toolcontext.ErrComputerScope) {
		t.Fatal("stale connector call bypassed scope")
	}
	if f.lists != 0 || f.calls != 0 {
		t.Fatal("rejected request reached connector")
	}
	nas := mcp.ToolSessionContext{WorkspaceTargetID: "native"}
	items, err = s.ListTools(t.Context(), nas)
	if err != nil || len(items) != 1 {
		t.Fatal("NAS connector catalog changed")
	}
	if _, err := s.CallTool(t.Context(), nas, "connector", nil); err != nil || f.calls != 1 {
		t.Fatal("NAS connector call changed")
	}
}

func TestComputerTaskAuthorizationKeepsForegroundPolicyAndAgentPermissionBoundary(t *testing.T) {
	b := toolcontext.ComputerBinding{BotID: "bot", UserID: "user", SessionID: "thread", TargetID: "native", RunID: "run"}
	for _, denied := range []string{"workspace_read", "workspace_write", "workspace_exec", ""} {
		for _, mode := range []settings.ToolApprovalMode{settings.ToolApprovalAllow, settings.ToolApprovalAsk, settings.ToolApprovalDeny} {
			cfg := workspace.DefaultRemoteToolApprovalConfig()
			cfg.Enabled = true
			cfg.Exec.Mode = mode
			auth := computerTaskAuthorizer(computerPermissions{denied}, func(context.Context, string, string) (workspace.ResolvedWorkspaceTarget, error) {
				return workspace.ResolvedWorkspaceTarget{Approval: cfg}, nil
			}, func(context.Context, approval.CreatePendingInput) (approval.Evaluation, error) {
				t.Fatal("Agent task must not use command bypass matching")
				return approval.Evaluation{}, nil
			})
			err := auth(t.Context(), b, "pc-a", "herdr_pi_task", "echo pretend-bypass")
			switch {
			case denied != "" || mode == settings.ToolApprovalDeny:
				if !errors.Is(err, computertask.ErrDenied) {
					t.Fatalf("deny %s mode %s: %v", denied, mode, err)
				}
			case mode == settings.ToolApprovalAsk:
				if !errors.Is(err, computertask.ErrApproval) {
					t.Fatal(err)
				}
			case err != nil:
				t.Fatal(err)
			}
		}
	}
	for _, decision := range []string{approval.DecisionBypass, approval.DecisionNeedsApproval, approval.DecisionDeny} {
		auth := computerTaskAuthorizer(computerPermissions{}, nil, func(_ context.Context, in approval.CreatePendingInput) (approval.Evaluation, error) {
			if in.BotID != b.BotID || in.SessionID != b.SessionID || !in.WorkspaceTargeted || in.ToolName != "exec" || in.ToolInput.(map[string]any)["target_id"] != "pc-a" || in.ToolInput.(map[string]any)["command"] != "printf 42" {
				t.Fatal("foreground policy context changed")
			}
			return approval.Evaluation{Decision: decision}, nil
		})
		err := auth(t.Context(), b, "pc-a", "foreground_bash", "printf 42")
		if (err == nil) != (decision == approval.DecisionBypass) {
			t.Fatalf("decision %s err=%v", decision, err)
		}
	}
}

func TestDelegatedNASRetainsCloudConnectorsAndRechecksRevocation(t *testing.T) {
	f := &unscopedConnectorFixture{}
	s := computerScopedConnectorSource{f}
	revoked := false
	ctx := toolcontext.WithDelegationScope(t.Context(), &toolcontext.DelegationScope{Check: func(context.Context) error {
		if revoked {
			return toolcontext.ErrComputerScope
		}
		return nil
	}})
	nas := mcp.ToolSessionContext{WorkspaceTargetID: "native", RunContext: ctx}
	if tools, err := s.ListTools(t.Context(), nas); err != nil || len(tools) != 1 {
		t.Fatalf("legitimate NAS connector lost: %v", err)
	}
	if _, err := s.CallTool(t.Context(), nas, "connector", nil); err != nil {
		t.Fatal(err)
	}
	revoked = true
	if _, err := s.CallTool(t.Context(), nas, "connector", nil); err == nil {
		t.Fatal("revoked connector dispatched")
	}
	if f.calls != 1 {
		t.Fatal("revocation reached upstream")
	}
	pc := mcp.ToolSessionContext{WorkspaceTargetID: "pc-a", RunContext: ctx}
	revoked = false
	if _, err := s.CallTool(t.Context(), pc, "connector", nil); !errors.Is(err, toolcontext.ErrComputerScope) {
		t.Fatal("PC restriction changed")
	}
}
