package core

import (
	"context"
	"fmt"

	"github.com/felinics/memoh/internal/agent/runtime/external"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

type runtimeBridgeSource interface {
	MCPClient(context.Context, string) (*bridge.Client, error)
	WorkspaceInfo(context.Context, string) (bridge.WorkspaceInfo, error)
}

type nativeRuntimeHost interface {
	NativeMCPClient(context.Context, string) (*bridge.Client, error)
	NativeWorkspaceInfo(context.Context, string) (bridge.WorkspaceInfo, error)
}

// containerRuntimeBridge keeps the Agenvo integration boundary in the host.
// Upstream drivers remain unchanged: a command-only target cannot host their
// container executable, filesystem, credentials or persistent process.
type containerRuntimeBridge struct {
	source  runtimeBridgeSource
	runtime string
}

func (b containerRuntimeBridge) WorkspaceInfo(ctx context.Context, botID string) (bridge.WorkspaceInfo, error) {
	var info bridge.WorkspaceInfo
	var err error
	if binding, ok := toolcontext.HostComputerBinding(ctx); ok {
		host, available := b.source.(nativeRuntimeHost)
		if !available || binding.BotID != botID {
			return info, external.ErrContainerWorkspaceRequired
		}
		// This is the explicit Agent hosting resource. The original ctx still
		// carries PC scope to its MCP mount; task tools never receive this bridge.
		info, err = host.NativeWorkspaceInfo(ctx, botID)
	} else {
		info, err = b.source.WorkspaceInfo(ctx, botID)
	}
	if err != nil {
		return bridge.WorkspaceInfo{}, err
	}
	if info.Backend == bridge.WorkspaceBackendAgenvo {
		return bridge.WorkspaceInfo{}, fmt.Errorf("%w: %s on workspace backend %q", external.ErrContainerWorkspaceRequired, b.runtime, info.Backend)
	}
	return info, nil
}

func (b containerRuntimeBridge) MCPClient(ctx context.Context, botID string) (*bridge.Client, error) {
	// Reject before the driver can obtain a client and materialize credentials
	// or launch a process. No alternate workspace is selected on failure.
	if _, err := b.WorkspaceInfo(ctx, botID); err != nil {
		return nil, err
	}
	if _, ok := toolcontext.HostComputerBinding(ctx); ok {
		return b.source.(nativeRuntimeHost).NativeMCPClient(ctx, botID)
	}
	return b.source.MCPClient(ctx, botID)
}
