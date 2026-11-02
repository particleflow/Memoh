package core

import (
	"context"
	"io"

	"go.uber.org/fx"

	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/channel"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

// sharedAttachmentStore lets outbound delivery read a content hash the sending
// Bot does not own, when a delegation the Bot is part of grants it. The bytes
// are read from the granting Bot's original: nothing is copied into the sender,
// and no per-Bot directory gains a second copy of the hash.
//
// The grant is rechecked on every read, including each Read of an open stream,
// so a revoked delegation stops delivery mid-transfer exactly as it stops a
// model read.
type sharedAttachmentStore struct {
	channel.OutboundAttachmentStore
	loader *delegationAssetLoader
}

func (s *sharedAttachmentStore) StatShared(ctx context.Context, botID, contentHash string) (media.Asset, error) {
	set, result, err := s.grant(ctx, botID, contentHash)
	if err != nil {
		return media.Asset{}, err
	}
	owner, err := set.Owner(ctx, contentHash, result)
	if err != nil {
		return media.Asset{}, err
	}
	asset, err := set.Originals.Stat(ctx, owner, contentHash)
	if err != nil {
		return media.Asset{}, err
	}
	return asset, nil
}

func (s *sharedAttachmentStore) OpenShared(ctx context.Context, botID, contentHash string) (io.ReadCloser, media.Asset, error) {
	set, result, err := s.grant(ctx, botID, contentHash)
	if err != nil {
		return nil, media.Asset{}, err
	}
	owner, err := set.Owner(ctx, contentHash, result)
	if err != nil {
		return nil, media.Asset{}, err
	}
	asset, err := set.Originals.Stat(ctx, owner, contentHash)
	if err != nil {
		return nil, media.Asset{}, err
	}
	reader, err := set.Open(ctx, contentHash, result)
	if err != nil {
		return nil, media.Asset{}, err
	}
	return reader, asset, nil
}

// grant resolves the delegation that authorizes botID to read contentHash in
// this conversation. Both "no conversation" and "no delegation names this hash"
// answer not-found, so a caller without a grant looks exactly like a caller
// whose hash is gone.
func (s *sharedAttachmentStore) grant(ctx context.Context, botID, contentHash string) (*botdelegation.AttachmentSet, bool, error) {
	if s == nil || s.loader == nil {
		return nil, false, media.ErrAssetNotFound
	}
	identity, ok := toolcontext.MediaIdentityFrom(ctx)
	if !ok || identity.BotID != botID {
		return nil, false, media.ErrAssetNotFound
	}
	set, result, err := s.loader.sharedForConversation(ctx, delegationMediaIdentity{bot: identity.BotID, user: identity.UserID, session: identity.SessionID}, contentHash)
	if err != nil {
		return nil, false, err
	}
	if set == nil {
		// No delegation in this conversation names the hash. It is not this
		// Bot's to send.
		return nil, false, media.ErrAssetNotFound
	}
	return set, result, nil
}

// injectSharedOutboundAttachments lets channel delivery send a hash a Bot may
// read under a delegation without that Bot owning a copy. Delivery keeps its
// existing path for hashes the sender owns.
//
// The manager is optional: a process that runs no channel adapters has nothing
// to decorate, and the composition must not fail over a surface it does not
// run.
func injectSharedOutboundAttachments(deps sharedAttachmentDeps) {
	if deps.Manager == nil || deps.Delegations == nil {
		return
	}
	h, ok := deps.Delegations.Host.(*delegationHost)
	if !ok || h == nil {
		return
	}
	loader := &delegationAssetLoader{base: &gatewayAssetLoaderAdapter{media: h.media}, host: h}
	deps.Manager.WrapAttachmentStore(func(store channel.OutboundAttachmentStore) channel.OutboundAttachmentStore {
		return &sharedAttachmentStore{OutboundAttachmentStore: store, loader: loader}
	})
}

type sharedAttachmentDeps struct {
	fx.In

	Manager     *channel.Manager `optional:"true"`
	Delegations *botdelegation.Service
}
