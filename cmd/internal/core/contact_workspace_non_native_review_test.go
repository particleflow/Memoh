package core

import (
	"log/slog"
	"testing"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/config"
	"github.com/felinics/memoh/internal/workspace"
)

func TestContactPersistedRemoteIndependentReview(t *testing.T) {
	f, authority, identity := newDelegationPlanFixture(t, planSourceMount)
	f.session.ChannelType = "local"
	f.session.RuntimeType = thread.RuntimeModel
	verified, err := authority.Source(t.Context(), identity.UserID, identity.SourceBotID, identity.SourceSessionID)
	if err != nil || verified.SourceSelection != planSourceMount {
		t.Fatalf("fixture: stored remote selection not currently authorized: selection=%q err=%v", verified.SourceSelection, err)
	}
	// Another legitimate primary must not replace this contact's saved target.
	primary := f.mounts[planSourceBot+"/"+planSourceMount]
	primary.ID = planTargetMount
	primary.RuntimeID = "66666666-6666-4666-8666-666666666666"
	primary.IsPrimary = true
	manager := workspace.NewManager(slog.Default(), nil, nil, config.WorkspaceConfig{}, "", nil)
	manager.SetRemoteWorkspaceService(workspace.NewRemoteWorkspaceService(contactWorkspaceMounts{record: primary}, nil))
	host := &delegationHost{service: &botdelegation.Service{Authority: authority, Ledger: contactWorkspaceLedger{}}}
	ctx, _, err := host.prepare(t.Context(), application.ChatRequest{BotID: identity.SourceBotID, UserID: identity.UserID, ThreadID: identity.SourceSessionID, CurrentChannel: "web"})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := manager.CurrentWorkspaceTargetID(ctx, identity.SourceBotID)
	if err != nil || actual != verified.SourceSelection {
		t.Fatalf("persisted authorized target lost: stored=%q inherited=%q resolved=%q err=%v", verified.SourceSelection, workspace.WorkspaceTargetFromContext(ctx), actual, err)
	}
}
