package containerfs

import (
	"context"
	"testing"

	"github.com/felinics/memoh/internal/media"
	workspacepkg "github.com/felinics/memoh/internal/workspace"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

// nativeSelectingClientProvider mirrors the production workspace manager: a
// request without a target override resolves the Bot's bound primary target
// (a managed computer whose client is FilesOnly), while a native target pin
// resolves the Bot's own native workspace container.
type nativeSelectingClientProvider struct {
	native, restricted *bridge.Client
	seenTargets        []string
}

func (p *nativeSelectingClientProvider) MCPClient(ctx context.Context, _ string) (*bridge.Client, error) {
	target := bridge.WorkspaceTargetFromContext(ctx)
	p.seenTargets = append(p.seenTargets, target)
	if target == workspacepkg.WorkspaceTargetNative {
		return p.native, nil
	}
	return p.restricted, nil
}

// A Bot whose primary workspace target is a managed computer must still be
// able to attach files from its own native workspace: media storage resolves
// the native container regardless of the bound primary target.
func TestMediaStorageResolvesNativeWorkspaceClient(t *testing.T) {
	root, server := transferGuardFixture(t)
	fake := &nativeSelectingClientProvider{native: root, restricted: root.FilesOnly()}
	service := media.NewService(nil, New(fake))
	asset, err := service.IngestContainerFile(t.Context(), "synthetic-bot", "/data/result.pdf")
	if err != nil || asset.ContentHash == "" || server.mediaRPCs.Load() == 0 {
		t.Fatalf("workspace-file attachment ingestion failed against a bot with a managed primary: %+v %v", asset, err)
	}
	pinned := false
	for _, target := range fake.seenTargets {
		if target == workspacepkg.WorkspaceTargetNative {
			pinned = true
		}
	}
	if !pinned {
		t.Fatalf("media storage resolved a client without the native target pin: %v", fake.seenTargets)
	}
}
