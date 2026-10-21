package core

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agent/runtime/claudecode"
	"github.com/felinics/memoh/internal/agent/runtime/codex"
	"github.com/felinics/memoh/internal/agent/runtime/external"
	"github.com/felinics/memoh/internal/agent/runtime/grok"
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

func (f *guardedRuntimeFixture) WorkspaceInfo(context.Context, string) (bridge.WorkspaceInfo, error) {
	return f.info, f.err
}

func (f *guardedRuntimeFixture) MCPClient(context.Context, string) (*bridge.Client, error) {
	f.clientCalls++
	return f.client, nil
}

func TestHostRejectsAgenvoBeforeDirectRuntimeClientAccess(t *testing.T) {
	for _, runtime := range []string{codex.RuntimeType, claudecode.RuntimeType, grok.RuntimeType} {
		t.Run(runtime, func(t *testing.T) {
			source := &guardedRuntimeFixture{info: bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendAgenvo}, client: &bridge.Client{}}
			host := containerRuntimeBridge{source: source, runtime: runtime}
			if _, err := host.WorkspaceInfo(t.Context(), "synthetic-bot"); !errors.Is(err, external.ErrContainerWorkspaceRequired) {
				t.Fatalf("info did not reject unsupported runtime: %v", err)
			}
			if client, err := host.MCPClient(t.Context(), "synthetic-bot"); client != nil || !errors.Is(err, external.ErrContainerWorkspaceRequired) || source.clientCalls != 0 {
				t.Fatalf("unsupported target exposed a client: calls=%d err=%v", source.clientCalls, err)
			}
		})
	}
}

func TestHostPreservesUpstreamWorkspaceBehavior(t *testing.T) {
	for _, backend := range []string{bridge.WorkspaceBackendContainer, bridge.WorkspaceBackendRemote} {
		source := &guardedRuntimeFixture{info: bridge.WorkspaceInfo{Backend: backend, DefaultWorkDir: "/synthetic"}, client: &bridge.Client{}}
		host := containerRuntimeBridge{source: source, runtime: "codex"}
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
