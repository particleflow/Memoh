package core

import (
	"context"
	"errors"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/toolcontext"
)

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
