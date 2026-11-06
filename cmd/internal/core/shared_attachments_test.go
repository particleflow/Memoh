package core

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

// Outbound delivery may only borrow another Bot's original inside the
// conversation a grant belongs to. Without that identity there is nothing to
// authorize against, and the hash stays as unresolvable as one the sender never
// had.
func TestSharedAttachmentStore_RequiresTheCallersConversation(t *testing.T) {
	t.Parallel()

	store := &sharedAttachmentStore{loader: &delegationAssetLoader{}}
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	cases := []struct {
		name string
		ctx  context.Context
	}{
		{name: "no identity", ctx: context.Background()},
		{
			name: "identity for another bot",
			ctx: toolcontext.WithMediaIdentity(context.Background(), toolcontext.MediaIdentity{
				BotID: "other-bot", UserID: "user-1", SessionID: "session-1",
			}),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := store.StatShared(tt.ctx, "manager-bot", hash); !errors.Is(err, media.ErrAssetNotFound) {
				t.Fatalf("StatShared() error = %v, want %v", err, media.ErrAssetNotFound)
			}
			if _, _, err := store.OpenShared(tt.ctx, "manager-bot", hash); !errors.Is(err, media.ErrAssetNotFound) {
				t.Fatalf("OpenShared() error = %v, want %v", err, media.ErrAssetNotFound)
			}
		})
	}
}

// A store with no grant source answers the same way, so a process that runs
// delivery without delegations keeps its existing behaviour.
func TestSharedAttachmentStore_WithoutLoaderIsNotGranted(t *testing.T) {
	t.Parallel()

	store := &sharedAttachmentStore{}
	ctx := toolcontext.WithMediaIdentity(context.Background(), toolcontext.MediaIdentity{
		BotID: "manager-bot", UserID: "user-1", SessionID: "session-1",
	})
	if _, err := store.StatShared(ctx, "manager-bot", "hash"); !errors.Is(err, media.ErrAssetNotFound) {
		t.Fatalf("StatShared() error = %v, want %v", err, media.ErrAssetNotFound)
	}
}
