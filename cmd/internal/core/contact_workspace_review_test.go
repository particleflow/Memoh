package core

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/workspace"
)

type contactReviewPermissions struct {
	botdelegation.Permissions
	invalidAccount bool
	wrongTeam      bool
}

func (p contactReviewPermissions) ValidateSession(ctx context.Context, user string) error {
	if p.invalidAccount {
		return errors.New("controlled invalid account")
	}
	return p.Permissions.ValidateSession(ctx, user)
}

func (p contactReviewPermissions) BotTeam(ctx context.Context, bot string) (string, error) {
	if p.wrongTeam {
		return "other-team", nil
	}
	return p.Permissions.BotTeam(ctx, bot)
}

func TestContactWorkspaceIndependentReview(t *testing.T) {
	for _, name := range []string{"expired account", "different Bot", "different team", "unresolved queue route", "native snapshot only", "cancel context retained"} {
		t.Run(name, func(t *testing.T) {
			f, authority, identity := newDelegationPlanFixture(t, "native")
			f.session.ChannelType = "local"
			host := &delegationHost{service: &botdelegation.Service{Authority: authority, Ledger: contactWorkspaceLedger{}}}
			req := application.ChatRequest{BotID: identity.SourceBotID, UserID: identity.UserID, ThreadID: identity.SourceSessionID, CurrentChannel: "web"}
			ctx := t.Context()
			denied := false
			switch name {
			case "expired account":
				authority.Permissions = contactReviewPermissions{Permissions: f, invalidAccount: true}
				denied = true
			case "different Bot":
				req.BotID = planTargetBot
				denied = true
			case "different team":
				authority.Permissions = contactReviewPermissions{Permissions: f, wrongTeam: true}
				denied = true
			case "unresolved queue route":
				req.CurrentChannel = ""
				f.session.RouteID = "unresolved-route"
				denied = true
			case "native snapshot only":
				f.session.Metadata = map[string]any{"workspace_target": map[string]any{"target_id": "native"}}
			case "cancel context retained":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			gotCtx, got, err := host.prepare(ctx, req)
			if denied {
				if !errors.Is(err, botdelegation.ErrDenied) || workspace.WorkspaceTargetFromContext(gotCtx) != "" {
					t.Fatalf("unexpected authority result: target=%q err=%v", workspace.WorkspaceTargetFromContext(gotCtx), err)
				}
				return
			}
			if err != nil || workspace.WorkspaceTargetFromContext(gotCtx) != "native" {
				t.Fatalf("recorded native not restored: target=%q err=%v", workspace.WorkspaceTargetFromContext(gotCtx), err)
			}
			if got.WorkspaceTargetID != "" || got.BotID != req.BotID || got.ThreadID != req.ThreadID || got.UserID != req.UserID {
				t.Fatal("restoration changed explicit selection or original identity")
			}
			if name == "cancel context retained" && !errors.Is(gotCtx.Err(), context.Canceled) {
				t.Fatal("restoration detached original cancellation")
			}
		})
	}
}
