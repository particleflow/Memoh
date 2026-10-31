package core

import (
	"context"
	"errors"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/imagecapability"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

// Reuse the same live-authorized loader as native conversation attachments.
// The durable delegation selects the original owner; no media alias is written.
func bindImageCapabilityReferences(provider *imagecapability.Provider, assets *media.Service, delegations *botdelegation.Service) {
	loader := &delegationAssetLoader{base: &gatewayAssetLoaderAdapter{media: assets}, host: delegations.Host.(*delegationHost)}
	provider.SetReferenceOpener(loader.OpenForGateway)
}

// Reuse live Bot chat permission and the persisted session-to-Bot binding.
// Session identities come from host admission, never image tool arguments.
func imageCapabilityAuthorizer(permissions agenvo.PermissionChecker, getSession func(context.Context, string) (thread.Thread, error)) func(context.Context, tools.SessionContext) error {
	return func(ctx context.Context, session tools.SessionContext) error {
		denied := errors.New("image capability conversation access denied")
		if permissions == nil || getSession == nil || session.BotID == "" || session.UserID == "" || session.SessionID == "" || toolcontext.CheckDelegation(ctx) != nil {
			return denied
		}
		allowed, err := permissions.HasBotPermission(ctx, session.BotID, session.UserID, bots.PermissionChat)
		if err != nil || !allowed {
			return denied
		}
		stored, err := getSession(ctx, session.SessionID)
		if err != nil || stored.BotID != session.BotID {
			return denied
		}
		return nil
	}
}
