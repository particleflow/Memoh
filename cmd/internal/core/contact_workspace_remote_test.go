package core

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/config"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/workspace"
	"github.com/google/uuid"
)

type contactTargetStore struct {
	dbstore.BotRemoteRuntimeBindingStore
	saved, primary dbstore.BotRemoteRuntimeBindingRecord
	missing        bool
	lookups        []string
}

func (s *contactTargetStore) GetPrimaryMount(context.Context, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	s.lookups = append(s.lookups, "primary")
	return s.primary, nil
}
func (s *contactTargetStore) GetMount(_ context.Context, bot, id string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	s.lookups = append(s.lookups, id)
	if s.missing || bot != s.saved.BotID || id != s.saved.ID {
		return dbstore.BotRemoteRuntimeBindingRecord{}, db.ErrNotFound
	}
	return s.saved, nil
}

type contactReadPermission struct {
	botdelegation.Permissions
	denyRead bool
}

func (p contactReadPermission) Allowed(ctx context.Context, user, bot, permission string) (bool, error) {
	if permission == bots.PermissionWorkspaceRead && p.denyRead {
		return false, nil
	}
	return p.Permissions.Allowed(ctx, user, bot, permission)
}

func TestRemoteContactRestoresRecordedTargetAndResolverErrors(t *testing.T) {
	for _, name := range []string{"changed primary", "queued without channel", "ordinary bridge without delegation profile", "offline", "revoked runtime", "owner changed", "missing mount", "revoked workspace read", "conflicting metadata"} {
		t.Run(name, func(t *testing.T) {
			f, a, b := newDelegationPlanFixture(t, planSourceMount)
			f.session.ChannelType = "local"
			f.session.RuntimeType = thread.RuntimeModel
			h := &delegationHost{service: &botdelegation.Service{Authority: a, Ledger: contactWorkspaceLedger{}}}
			s := &contactTargetStore{saved: f.mounts[planSourceBot+"/"+planSourceMount]}
			s.primary = s.saved
			s.primary.ID = uuid.NewString()
			m := workspace.NewManager(slog.Default(), nil, nil, config.WorkspaceConfig{}, "", nil)
			m.SetRemoteWorkspaceService(workspace.NewRemoteWorkspaceService(s, nil))
			req := application.ChatRequest{BotID: b.SourceBotID, UserID: b.UserID, ThreadID: b.SourceSessionID, CurrentChannel: "web"}
			wantErr := workspace.ErrRemoteRuntimeOffline
			deny := false
			switch name {
			case "queued without channel":
				req.CurrentChannel = ""
			case "ordinary bridge without delegation profile":
				f.registration.ProviderConfig = []byte(`{}`)
				s.saved.RuntimeProviderConfig = []byte(`{}`)
			case "revoked runtime":
				s.saved.RuntimeRevoked = true
				wantErr = workspace.ErrRemoteRuntimeRevoked
			case "owner changed":
				s.saved.RuntimeUserID = "another-owner"
				wantErr = workspace.ErrRemoteRuntimeOwnerMismatch
			case "missing mount":
				s.missing = true
				wantErr = workspace.ErrWorkspaceTargetNotFound
			case "revoked workspace read":
				a.Permissions = contactReadPermission{Permissions: a.Permissions, denyRead: true}
				deny = true
			case "conflicting metadata":
				f.session.Metadata["workspace_target"] = map[string]any{"target_id": s.primary.ID}
				deny = true
			}
			ctx, got, err := h.prepare(context.Background(), req)
			if deny {
				if !errors.Is(err, botdelegation.ErrDenied) {
					t.Fatal("invalid selection/grant accepted", err)
				}
				return
			}
			if err != nil || got.WorkspaceTargetID != planSourceMount || workspace.WorkspaceTargetFromContext(ctx) != planSourceMount {
				t.Fatalf("lost recorded target: %+v %v", got, err)
			}
			target, err := m.CurrentWorkspaceTargetID(ctx, b.SourceBotID)
			if err != nil || target != planSourceMount {
				t.Fatalf("followed primary: %q %v", target, err)
			}
			_, err = m.ResolveWorkspaceTarget(ctx, b.SourceBotID, got.WorkspaceTargetID)
			if !errors.Is(err, wantErr) {
				t.Fatalf("resolver error %v want %v", err, wantErr)
			}
			for _, id := range s.lookups {
				if id != planSourceMount {
					t.Fatal("fallback to primary", s.lookups)
				}
			}
		})
	}
}
