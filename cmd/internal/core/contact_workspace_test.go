package core

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/config"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/workspace"
)

type contactWorkspaceLedger struct{ botdelegation.Ledger }

func (contactWorkspaceLedger) ForTarget(context.Context, string, string) (botdelegation.Record, error) {
	return botdelegation.Record{}, pgx.ErrNoRows
}

type contactWorkspaceMounts struct {
	dbstore.BotRemoteRuntimeBindingStore
	record dbstore.BotRemoteRuntimeBindingRecord
}

func (s contactWorkspaceMounts) GetPrimaryMount(context.Context, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	return s.record, nil
}

func (s contactWorkspaceMounts) GetMount(context.Context, string, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	return s.record, nil
}

func TestContactContinuationRestoresSavedNativeSelection(t *testing.T) {
	f, authority, identity := newDelegationPlanFixture(t, "native")
	f.session.ChannelType = "local"
	f.session.RuntimeType = thread.RuntimeModel
	host := &delegationHost{service: &botdelegation.Service{Authority: authority, Ledger: contactWorkspaceLedger{}}}
	manager := workspace.NewManager(slog.Default(), nil, nil, config.WorkspaceConfig{}, "", nil)
	manager.SetRemoteWorkspaceService(workspace.NewRemoteWorkspaceService(contactWorkspaceMounts{record: f.mounts[planSourceBot+"/"+planSourceMount]}, nil))
	// This is a valid offline primary, not a revoked or foreign registration.
	if _, err := manager.ResolveWorkspaceTarget(t.Context(), planSourceBot, ""); !errors.Is(err, workspace.ErrRemoteRuntimeOffline) {
		t.Fatalf("fixture: %v", err)
	}
	for _, name := range []string{"fresh queued user turn", "fresh result continuation context"} {
		t.Run(name, func(t *testing.T) {
			req := application.ChatRequest{BotID: identity.SourceBotID, UserID: identity.UserID, ThreadID: identity.SourceSessionID, CurrentChannel: "web"}
			if name == "fresh queued user turn" {
				req.CurrentChannel = ""
			}
			ctx, got, err := host.prepare(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			target, err := manager.CurrentWorkspaceTargetID(ctx, req.BotID)
			if err != nil || target != "native" {
				t.Fatalf("persisted native selection lost: target=%q error=%v; untouched primary is offline", target, err)
			}
			if got.WorkspaceTargetID != "" {
				t.Fatal("native inheritance must not add explicit workspace-read requirement")
			}
		})
	}
}

func TestContactWorkspaceInheritanceBoundaries(t *testing.T) {
	for _, name := range []string{"explicit remote", "stored remote", "missing selection", "malformed selection", "conflicting snapshot", "wrong owner", "revoked chat", "external runtime", "external channel", "workdir owns target"} {
		t.Run(name, func(t *testing.T) {
			f, a, b := newDelegationPlanFixture(t, "native")
			f.session.ChannelType = "local"
			f.session.RuntimeType = thread.RuntimeModel
			h := &delegationHost{service: &botdelegation.Service{Authority: a, Ledger: contactWorkspaceLedger{}}}
			req := application.ChatRequest{BotID: b.SourceBotID, UserID: b.UserID, ThreadID: b.SourceSessionID, CurrentChannel: "web"}
			wantTarget, wantRequest := "", ""
			denied := false
			switch name {
			case "explicit remote":
				req.WorkspaceTargetID = planSourceMount
				wantRequest = planSourceMount
			case "stored remote":
				f.session.Metadata["workspace_target_id"] = planSourceMount
			case "missing selection":
				f.session.Metadata = nil
			case "malformed selection":
				f.session.Metadata["workspace_target_id"] = 42
				f.session.Metadata["workspace_target"] = map[string]any{"target_id": "native"}
				denied = true
			case "conflicting snapshot":
				f.session.Metadata["workspace_target"] = map[string]any{"target_id": planSourceMount}
				denied = true
			case "wrong owner":
				f.session.CreatedByUserID = "other"
				denied = true
			case "revoked chat":
				f.denied = true
				denied = true
			case "external runtime":
				f.session.RuntimeType = "codex"
			case "external channel":
				req.CurrentChannel = "telegram"
			case "workdir owns target":
				f.session.WorkdirID = "existing-workdir"
			}
			ctx, got, err := h.prepare(t.Context(), req)
			if denied {
				if !errors.Is(err, botdelegation.ErrDenied) {
					t.Fatalf("invalid selection/grant accepted: %v", err)
				}
				return
			}
			if err != nil || workspace.WorkspaceTargetFromContext(ctx) != wantTarget || got.WorkspaceTargetID != wantRequest {
				t.Fatalf("target=%q request=%q error=%v", workspace.WorkspaceTargetFromContext(ctx), got.WorkspaceTargetID, err)
			}
		})
	}
}
