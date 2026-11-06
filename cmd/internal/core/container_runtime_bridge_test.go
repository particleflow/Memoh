package core

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agent/runtime/claudecode"
	"github.com/felinics/memoh/internal/agent/runtime/codex"
	"github.com/felinics/memoh/internal/agent/runtime/external"
	"github.com/felinics/memoh/internal/agent/runtime/grok"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

var (
	_ codex.BridgeSource      = containerRuntimeBridge{}
	_ claudecode.BridgeSource = containerRuntimeBridge{}
	_ grok.BridgeSource       = containerRuntimeBridge{}
)

type guardedRuntimeFixture struct {
	info        bridge.WorkspaceInfo
	err         error
	client      *bridge.Client
	clientCalls int
}

type nativeHostFixture struct {
	guardedRuntimeFixture
	nativeCalls   int
	observedScope string
}

func (f *nativeHostFixture) NativeWorkspaceInfo(ctx context.Context, _ string) (bridge.WorkspaceInfo, error) {
	f.observedScope = toolcontext.ComputerScope(ctx)
	return bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendContainer}, nil
}

func (f *nativeHostFixture) NativeMCPClient(ctx context.Context, _ string) (*bridge.Client, error) {
	f.nativeCalls++
	f.observedScope = toolcontext.ComputerScope(ctx)
	return f.client, nil
}

func TestComputerBindingSeparatesOriginalAgentHostFromTaskTarget(t *testing.T) {
	ctx, err := toolcontext.WithComputerBinding(t.Context(), toolcontext.ComputerBinding{BotID: "bot", UserID: "user", SessionID: "thread", TargetID: "pc-a", RunID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	f := &nativeHostFixture{guardedRuntimeFixture: guardedRuntimeFixture{info: bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendRemote}, client: &bridge.Client{}}}
	host := containerRuntimeBridge{source: f}
	for range 2 { // Both cold acquisition and a later turn must use the host.
		info, err := host.WorkspaceInfo(ctx, "bot")
		if err != nil || external.RequireContainerWorkspace(info, "codex") != nil {
			t.Fatal("Agent host was resolved as task PC", err)
		}
		if _, err = host.MCPClient(ctx, "bot"); err != nil {
			t.Fatal(err)
		}
	}
	if f.clientCalls != 0 || f.nativeCalls != 2 || f.observedScope != "pc-a" {
		t.Fatal("host isolation changed MCP task scope")
	}
	if err := toolcontext.CheckComputerTarget(ctx, "native"); !errors.Is(err, toolcontext.ErrComputerScope) {
		t.Fatal("task tools gained NAS access")
	}
	if _, err := host.MCPClient(ctx, "other-bot"); err == nil {
		t.Fatal("binding crossed Bot ownership")
	}
}

func (f *guardedRuntimeFixture) WorkspaceInfo(context.Context, string) (bridge.WorkspaceInfo, error) {
	return f.info, f.err
}

func (f *guardedRuntimeFixture) MCPClient(context.Context, string) (*bridge.Client, error) {
	f.clientCalls++
	return f.client, nil
}

func TestHostPreservesUpstreamWorkspaceBehavior(t *testing.T) {
	for _, backend := range []string{bridge.WorkspaceBackendContainer, bridge.WorkspaceBackendRemote} {
		source := &guardedRuntimeFixture{info: bridge.WorkspaceInfo{Backend: backend, DefaultWorkDir: "/synthetic"}, client: &bridge.Client{}}
		host := containerRuntimeBridge{source: source}
		info, err := host.WorkspaceInfo(t.Context(), "synthetic-bot")
		if err != nil || info != source.info {
			t.Fatalf("host changed workspace info: %+v %v", info, err)
		}
		client, err := host.MCPClient(t.Context(), "synthetic-bot")
		if err != nil || client != source.client || source.clientCalls != 1 {
			t.Fatalf("host changed client routing: %v", err)
		}
		upstreamErr := external.RequireContainerWorkspace(info, "codex")
		if (backend == bridge.WorkspaceBackendRemote) != errors.Is(upstreamErr, external.ErrContainerWorkspaceRequired) {
			t.Fatalf("upstream remote guard changed: %v", upstreamErr)
		}
	}
	want := errors.New("synthetic workspace unavailable")
	source := &guardedRuntimeFixture{err: want}
	_, err := (containerRuntimeBridge{source: source}).MCPClient(t.Context(), "synthetic-bot")
	if !errors.Is(err, want) || source.clientCalls != 0 {
		t.Fatalf("failed lookup fell back: %v", err)
	}
}
