package core

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/felinics/memoh/internal/accounts"
	toolapproval "github.com/felinics/memoh/internal/agent/decision/approval"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/config"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace"
)

func provideComputerTaskSource(store *postgresstore.Store, relay *agenvo.Service, assets *media.Service, manager *workspace.Manager, approval *toolapproval.Service, botService *bots.Service, accountService *accounts.Service, cfg config.Config) *computertask.Source {
	permissions := &applicationBotPermissionChecker{bots: botService, accounts: accountService}
	authorize := computerTaskAuthorizer(permissions, manager.ResolveWorkspaceTarget, approval.EvaluatePolicy)
	return computertask.NewSource(computertask.NewService(store, store, relay, assets, authorize).WithNativeObservationLimit(cfg.ComputerTasks.NativeObservationLimit))
}

func computerTaskAuthorizer(permissions agenvo.PermissionChecker, resolve func(context.Context, string, string) (workspace.ResolvedWorkspaceTarget, error), evaluate func(context.Context, toolapproval.CreatePendingInput) (toolapproval.Evaluation, error)) computertask.Authorize {
	return func(ctx context.Context, b toolcontext.ComputerBinding, target, kind, task string) error {
		if kind == "herdr_pi_task" {
			// A general Agent can read/write/run arbitrary commands. A task's
			// prose must never match a narrow foreground-command bypass rule.
			for _, permission := range []string{bots.PermissionWorkspaceRead, bots.PermissionWorkspaceWrite, bots.PermissionWorkspaceExec} {
				if err := toolcontext.CheckDelegationTarget(ctx, b.BotID, target, permission); err != nil {
					return computertask.ErrDenied
				}
				ok, err := permissions.HasBotPermission(ctx, b.BotID, b.UserID, permission)
				if err != nil || !ok {
					return computertask.ErrDenied
				}
			}
			resolved, err := resolve(ctx, b.BotID, target)
			if errors.Is(err, workspace.ErrRemoteRuntimeOffline) {
				// Native resolution checks the selected Runtime connection.
				// Availability/authentication failure is not a scope denial; all
				// binding and permission checks above still apply unchanged.
				return computertask.ErrPreflightUnavailable
			}
			if err != nil {
				return computertask.ErrDenied
			}
			cfg := settings.NormalizeToolApprovalConfig(resolved.Approval)
			if cfg.Enabled {
				if cfg.Read.Mode == settings.ToolApprovalDeny || cfg.Write.Mode == settings.ToolApprovalDeny || cfg.Exec.Mode == settings.ToolApprovalDeny {
					return computertask.ErrDenied
				}
				if cfg.Read.Mode != settings.ToolApprovalAllow || cfg.Write.Mode != settings.ToolApprovalAllow || cfg.Exec.Mode != settings.ToolApprovalAllow {
					return computertask.ErrApproval
				}
			}
			return nil
		}
		eval, err := evaluate(ctx, toolapproval.CreatePendingInput{BotID: b.BotID, SessionID: b.SessionID, ToolName: "exec", WorkspaceTargeted: true, ToolInput: map[string]any{"target_id": target, "command": task}})
		if err != nil {
			return computertask.ErrDenied
		}
		switch eval.Decision {
		case toolapproval.DecisionBypass:
			return nil
		case toolapproval.DecisionNeedsApproval:
			return computertask.ErrApproval
		default:
			return computertask.ErrDenied
		}
	}
}

func computerConnectionCheck(store *postgresstore.Store) func(context.Context, mcp.Connection) error {
	return func(ctx context.Context, connection mcp.Connection) error {
		if err := toolcontext.CheckDelegation(ctx); err != nil {
			return err
		}
		// Raw MCP has no trustworthy per-device scope. On a PC conversation
		// only the constrained computer service may access remote devices.
		if toolcontext.ComputerScope(ctx) != "" {
			return mcp.ErrToolNotFound
		}
		config, err := json.Marshal(connection.Config)
		if err != nil {
			return computertask.ErrDenied
		}
		managed, err := store.IsManagedComputerEndpoint(ctx, connection.ID, config)
		if err != nil {
			return computertask.ErrDenied
		}
		if managed {
			return mcp.ErrToolNotFound
		}
		return nil
	}
}

// The aggregate connector protocol does not identify a device scope. Keep it
// out of PC-bound turns rather than trusting an Agent-supplied scope header.
type computerScopedConnectorSource struct{ mcp.ToolSource }

func (s computerScopedConnectorSource) ListTools(ctx context.Context, session mcp.ToolSessionContext) ([]mcp.ToolDescriptor, error) {
	ctx, cancel := toolcontext.Bind(ctx, session)
	defer cancel()
	if err := toolcontext.CheckDelegation(ctx); err != nil {
		return nil, err
	}
	if toolcontext.ComputerScope(ctx) != "" {
		return []mcp.ToolDescriptor{}, nil
	}
	return s.ToolSource.ListTools(ctx, session)
}

func (s computerScopedConnectorSource) CallTool(ctx context.Context, session mcp.ToolSessionContext, name string, args map[string]any) (map[string]any, error) {
	ctx, cancel := toolcontext.Bind(ctx, session)
	defer cancel()
	if err := toolcontext.CheckDelegation(ctx); err != nil {
		return nil, err
	}
	if toolcontext.ComputerScope(ctx) != "" {
		return nil, toolcontext.ErrComputerScope
	}
	return s.ToolSource.CallTool(ctx, session, name, args)
}
