package core

import (
	"context"
	"slices"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/bots"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/nativeherdr"
	"github.com/felinics/memoh/internal/userruntime"
	"github.com/felinics/memoh/internal/workspace"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

// provideAgenvoService retains the shared task adapter for native Memoh Runtime.
// No legacy MCP/OAuth caller is installed; old registrations cannot dispatch.
func provideAgenvoService(store *postgresstore.Store, botService *bots.Service, accounts *accounts.Service) *agenvo.Service {
	return agenvo.NewService(store, &applicationBotPermissionChecker{bots: botService, accounts: accounts}, nil)
}

func setupAgenvoService(provider *agenvo.Service, runtimes *userruntime.Service, manager *workspace.Manager, store *postgresstore.Store) {
	runtimes.SetAgenvoService(provider)
	executor := nativeherdr.NewExecutor(func(ctx context.Context, scope agenvo.Scope, expected agenvo.Target) (*bridge.Client, bridge.WorkspaceInfo, error) {
		current, err := provider.ResolveTarget(ctx, scope)
		if err != nil {
			return nil, bridge.WorkspaceInfo{}, err
		}
		if current != expected || current.NativeRuntimeID == "" {
			return nil, bridge.WorkspaceInfo{}, agenvo.ErrTargetChanged
		}
		mount, err := store.GetMount(ctx, scope.BotID, scope.TargetID)
		if err != nil || mount.RuntimeID != current.NativeRuntimeID || mount.RuntimeProvider != "memoh" {
			return nil, bridge.WorkspaceInfo{}, agenvo.ErrTargetChanged
		}
		resolved, err := manager.ResolveWorkspaceTarget(ctx, scope.BotID, scope.TargetID)
		if err != nil {
			return nil, bridge.WorkspaceInfo{}, agenvo.ErrUnavailable
		}
		if resolved.TargetID != scope.TargetID || resolved.Kind != workspace.WorkspaceTargetRemote || resolved.Info.Backend != bridge.WorkspaceBackendRemote || (resolved.Info.OS != "windows" && resolved.Info.OS != "win32") {
			return nil, bridge.WorkspaceInfo{}, agenvo.ErrInvalidTarget
		}
		// General workspace clients are file-only for managed computer targets,
		// including hooks. Only this grant/fingerprint-checked ledger transport
		// obtains the current raw connection for a bounded official CLI call.
		connection, ok := runtimes.Connection(current.NativeRuntimeID)
		if !ok || connection == nil || connection.Client == nil || (connection.Info.OS != "windows" && connection.Info.OS != "win32") || !slices.Contains(connection.Info.Capabilities, userruntime.CapabilityExec) || !slices.Contains(connection.Info.Capabilities, userruntime.CapabilityFS) || !slices.Contains(connection.Info.Capabilities, userruntime.CapabilityHostFS) {
			return nil, bridge.WorkspaceInfo{}, agenvo.ErrUnavailable
		}
		return connection.Client, resolved.Info, nil
	})
	provider.SetNativeCaller(executor.Invoke)
}
