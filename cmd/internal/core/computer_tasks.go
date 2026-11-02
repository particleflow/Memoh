package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/accounts"
	toolapproval "github.com/felinics/memoh/internal/agent/decision/approval"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/computertarget"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/config"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/providers"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/userruntime"
	"github.com/felinics/memoh/internal/workspace"
)

func provideComputerTaskSource(log *slog.Logger, store *postgresstore.Store, relay *computertarget.Service, assets *media.Service, manager *workspace.Manager, approval *toolapproval.Service, botService *bots.Service, accountService *accounts.Service, runtimes *userruntime.Service, delegations *botdelegation.Service, providerService *providers.Service, cfg config.Config) *computertask.Source {
	permissions := &applicationBotPermissionChecker{bots: botService, accounts: accountService}
	authorize := computerTaskAuthorizer(permissions, manager.ResolveWorkspaceTarget, approval.EvaluatePolicy)
	service := computertask.NewService(store, store, relay, assets, authorize).WithNativeObservationLimit(cfg.ComputerTasks.NativeObservationLimit).WithPiModelList(magpieModelList(providerService))
	loader := &delegationAssetLoader{host: delegations.Host.(*delegationHost)}
	service.WithInputReferences(loader.openComputerInput)
	service.WithDefaultWorkDir(computerTaskWorkspaceDefault(store.GetMount, runtimes.SelectedWorkspaceDirectory))
	service.WithInputFiles(func(ctx context.Context, binding toolcontext.ComputerBinding, targetID string, target computertarget.Target) (computertask.InputFileSystem, error) {
		// Resolve the same already-authorized native mount. Explicit root context
		// suppresses account directory defaults; it never maps a NAS /data path.
		resolved, err := manager.ResolveWorkspaceTarget(toolcontext.WithExplicitWorkdir(ctx, target.WorkDir), binding.BotID, targetID)
		if err != nil || resolved.TargetID != targetID || resolved.Client == nil {
			return nil, computertask.ErrDenied
		}
		return resolved.Client.FilesOnly(), nil
	})
	source := computertask.NewSource(service)
	source.SetResumeContext(computerTaskResumeContext(delegations))
	source.SetLogger(log)
	// Reconnect the PC task lifecycle to the existing result delivery: terminal
	// outcome saves feed the follow-up scan, and the scan reuses this source's
	// authorized observation read when an endpoint delivery commit wakes it.
	if host, ok := delegations.Host.(*delegationHost); ok && host != nil {
		service.WithTerminalObservation(host.computerTerminalObserved)
		host.computerSource = source
		host.computerResume = computerTaskResumeContext(delegations)
	}
	return source
}

// magpieModelList reads the live Magpie catalog through the saved provider
// wiring: the one enabled saved provider named after the Magpie protocol, via
// the existing read-only GET /v1/models catalog probe. No new provider or
// credential configuration is introduced; an unreadable or ambiguous catalog
// surfaces as an error instead of any cached or guessed list.
func magpieModelList(providerService *providers.Service) computertask.PiModelList {
	return func(ctx context.Context) ([]computertask.PiModel, error) {
		if providerService == nil {
			return nil, errors.New("pi model list unavailable: provider service not configured")
		}
		saved, err := providerService.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("pi model list unavailable: %w", err)
		}
		var matched []string
		for _, p := range saved {
			if p.Enable && strings.Contains(strings.ToLower(p.Name), providers.MediaCatalogProtocolMagpie) {
				matched = append(matched, p.ID)
			}
		}
		if len(matched) != 1 {
			return nil, fmt.Errorf("pi model list unavailable: %d enabled saved providers named after %s", len(matched), providers.MediaCatalogProtocolMagpie)
		}
		catalog, err := providerService.DiscoverMediaCatalog(ctx, matched[0])
		if err != nil {
			return nil, fmt.Errorf("pi model list unavailable: %w", err)
		}
		models := make([]computertask.PiModel, 0, len(catalog.Models))
		for _, m := range catalog.Models {
			models = append(models, computertask.PiModel{ID: m.ID, Kind: m.Kind, Input: m.Input, Output: m.Output})
		}
		return models, nil
	}
}

// HTTP management has no active turn scope. Rebuild it from the operation's
// authenticated owning conversation, preserving both recorded mount snapshots.
func computerTaskResumeContext(delegations *botdelegation.Service) func(context.Context, toolcontext.ComputerBinding) (context.Context, error) {
	return func(ctx context.Context, b toolcontext.ComputerBinding) (context.Context, error) {
		if current, ok := toolcontext.HostComputerBinding(ctx); !ok || current != b || b.RunID == "" {
			return ctx, computertask.ErrDenied
		}
		r, err := delegations.ForSession(ctx, b.BotID, b.UserID, b.SessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ctx, nil
		}
		if err != nil {
			return ctx, computertask.ErrDenied
		}
		ctx = delegations.BindScope(ctx, r)
		if toolcontext.CheckDelegationTarget(ctx, b.BotID, b.TargetID, "") != nil {
			return ctx, computertask.ErrDenied
		}
		return ctx, nil
	}
}

// Use the same persisted attachment bridge as image tools, with the trusted
// computer conversation binding (including same-Bot child threads). The task
// ledger remains owned by the specialist; the original retains its source Bot.
func (a *delegationAssetLoader) openComputerInput(ctx context.Context, b toolcontext.ComputerBinding, hash string) (media.ReadSeekCloser, string, error) {
	if current, ok := toolcontext.HostComputerBinding(ctx); !ok || current != b {
		return nil, "", computertask.ErrDenied
	}
	set, result, err := a.sharedForConversation(ctx, delegationMediaIdentity{bot: b.BotID, user: b.UserID, session: b.SessionID}, hash)
	if err != nil || set == nil {
		return nil, "", err
	}
	if !result && (!toolcontext.HasDelegationScope(ctx) || toolcontext.CheckDelegation(ctx) != nil) {
		return nil, "", computertask.ErrDenied
	}
	reader, attachment, err := set.OpenSeekable(ctx, hash, result)
	return reader, attachment.Mime, err
}

func computerTaskAuthorizer(permissions computertarget.PermissionChecker, resolve func(context.Context, string, string) (workspace.ResolvedWorkspaceTarget, error), evaluate func(context.Context, toolapproval.CreatePendingInput) (toolapproval.Evaluation, error)) computertask.Authorize {
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

func computerConnectionCheck() func(context.Context, mcp.Connection) error {
	return func(ctx context.Context, connection mcp.Connection) error {
		if err := toolcontext.CheckDelegation(ctx); err != nil {
			return err
		}
		// Raw MCP has no trustworthy per-device scope. On a PC conversation
		// only the constrained computer service may access remote devices.
		if toolcontext.ComputerScope(ctx) != "" {
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
