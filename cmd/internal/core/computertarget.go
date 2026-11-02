package core

import (
	"context"
	"slices"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/computertarget"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/nativeherdr"
	"github.com/felinics/memoh/internal/userruntime"
	"github.com/felinics/memoh/internal/workspace"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

// provideComputerTargetService retains the shared task adapter for native Memoh Runtime.
// No legacy MCP/OAuth caller is installed; old registrations cannot dispatch.
func provideComputerTargetService(store *postgresstore.Store, botService *bots.Service, accounts *accounts.Service) *computertarget.Service {
	return computertarget.NewService(store, &applicationBotPermissionChecker{bots: botService, accounts: accounts}, nil)
}

func setupComputerTargetService(provider *computertarget.Service, runtimes *userruntime.Service, manager *workspace.Manager, store *postgresstore.Store) {
	runtimes.SetComputerTargetService(provider)
	executor := nativeherdr.NewExecutor(func(ctx context.Context, scope computertarget.Scope, expected computertarget.Target) (*bridge.Client, bridge.WorkspaceInfo, error) {
		current, err := provider.ResolveTarget(ctx, scope)
		if err != nil {
			return nil, bridge.WorkspaceInfo{}, err
		}
		if current != expected || current.NativeRuntimeID == "" {
			return nil, bridge.WorkspaceInfo{}, computertarget.ErrTargetChanged
		}
		mount, err := store.GetMount(ctx, scope.BotID, scope.TargetID)
		if err != nil || mount.RuntimeID != current.NativeRuntimeID || mount.RuntimeProvider != "memoh" {
			return nil, bridge.WorkspaceInfo{}, computertarget.ErrTargetChanged
		}
		resolved, err := manager.ResolveWorkspaceTarget(ctx, scope.BotID, scope.TargetID)
		if err != nil {
			return nil, bridge.WorkspaceInfo{}, computertarget.ErrUnavailable
		}
		if resolved.TargetID != scope.TargetID || resolved.Kind != workspace.WorkspaceTargetRemote || resolved.Info.Backend != bridge.WorkspaceBackendRemote || (resolved.Info.OS != "windows" && resolved.Info.OS != "win32") {
			return nil, bridge.WorkspaceInfo{}, computertarget.ErrInvalidTarget
		}
		// General workspace clients are file-only for managed computer targets,
		// including hooks. Only this grant/fingerprint-checked ledger transport
		// obtains the current raw connection for a bounded official CLI call.
		connection, ok := runtimes.Connection(current.NativeRuntimeID)
		if !ok || connection == nil || connection.Client == nil || (connection.Info.OS != "windows" && connection.Info.OS != "win32") || !slices.Contains(connection.Info.Capabilities, userruntime.CapabilityExec) || !slices.Contains(connection.Info.Capabilities, userruntime.CapabilityFS) || !slices.Contains(connection.Info.Capabilities, userruntime.CapabilityHostFS) {
			return nil, bridge.WorkspaceInfo{}, computertarget.ErrUnavailable
		}
		return connection.Client, resolved.Info, nil
	})
	provider.SetNativeCaller(executor.Invoke)
}
