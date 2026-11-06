package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/albumwriteback"
)

func TestAlbumWritebackProviderNeedsAService(t *testing.T) {
	t.Parallel()

	provider := NewAlbumWritebackProvider(nil, nil)
	tools, err := provider.Tools(context.Background(), goldenSession)
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("unconfigured provider registered %d tools", len(tools))
	}
	if got := provider.Usage(context.Background(), goldenSession, availableToolsForTest(ToolQueueAlbumWriteback())); got != "" {
		t.Fatalf("unconfigured usage = %q", got)
	}
}

func TestAlbumWritebackProviderDescribesQueueingWithoutPhoneInternals(t *testing.T) {
	t.Parallel()

	provider := NewAlbumWritebackProvider(nil, &albumwriteback.Service{})
	tools, err := provider.Tools(context.Background(), goldenSession)
	if err != nil || len(tools) != 1 || tools[0].Name != ToolQueueAlbumWriteback().String() {
		t.Fatalf("tools = %v, %v", tools, err)
	}
	description := tools[0].Description
	for _, expected := range []string{"pair_key", "content_hash", "手机", "相册"} {
		if !strings.Contains(description, expected) {
			t.Fatalf("description is missing %q: %s", expected, description)
		}
	}
	usage := provider.Usage(context.Background(), goldenSession, availableToolsForTest(ToolQueueAlbumWriteback()))
	if !strings.Contains(usage, ToolQueueAlbumWriteback().String()) {
		t.Fatalf("usage does not name the tool: %q", usage)
	}
}
