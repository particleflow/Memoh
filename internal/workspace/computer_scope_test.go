package workspace

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/felinics/memoh/internal/config"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

type scopeBindingStore struct {
	fakeRemoteBindingStore
	listed int
	got    []string
}

func (s *scopeBindingStore) ListMounts(ctx context.Context, bot string) ([]dbstore.BotRemoteRuntimeBindingRecord, error) {
	s.listed++
	return s.fakeRemoteBindingStore.ListMounts(ctx, bot)
}

func (s *scopeBindingStore) GetMount(ctx context.Context, bot, target string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	s.got = append(s.got, target)
	return s.fakeRemoteBindingStore.GetMount(ctx, bot, target)
}

func TestComputerScopeInventoryDoesNotEnumerateOtherMounts(t *testing.T) {
	store := &scopeBindingStore{fakeRemoteBindingStore: fakeRemoteBindingStore{records: []dbstore.BotRemoteRuntimeBindingRecord{
		{ID: remoteTestTargetID, BotID: remoteTestBotID, RuntimeUserID: remoteTestOwnerID, BotOwnerUserID: remoteTestOwnerID, RuntimeName: "Synthetic A"},
		{ID: remoteTestTargetID2, BotID: remoteTestBotID, RuntimeUserID: remoteTestOwnerID, BotOwnerUserID: remoteTestOwnerID, RuntimeName: "Synthetic B"},
	}}}
	manager := NewManager(slog.Default(), nil, nil, config.WorkspaceConfig{}, "", nil)
	manager.SetRemoteWorkspaceService(&RemoteWorkspaceService{store: store})
	ctx := toolcontext.WithComputerScope(t.Context(), remoteTestTargetID)
	targets, err := manager.ListWorkspaceTargets(ctx, remoteTestBotID)
	if err != nil || len(targets) != 1 || targets[0].TargetID != remoteTestTargetID || store.listed != 0 || len(store.got) != 1 || store.got[0] != remoteTestTargetID {
		t.Fatalf("scoped inventory: targets=%v list=%d get=%v err=%v", targets, store.listed, store.got, err)
	}
	// The scope is not a grant: another Bot cannot inherit A's mount.
	if _, err := manager.ListWorkspaceTargets(ctx, remoteTestBotID2); err == nil {
		t.Fatal("scope bypassed Bot mount ownership")
	}
	store.got = nil
	for _, requested := range []string{remoteTestTargetID2, WorkspaceTargetNative} {
		_, err := manager.ResolveWorkspaceTarget(ctx, remoteTestBotID, requested)
		if !errors.Is(err, toolcontext.ErrComputerScope) || len(store.got) != 0 {
			t.Fatalf("cross-target lookup happened: get=%v err=%v", store.got, err)
		}
	}
	// Changing a mutable workspace default cannot widen the immutable scope.
	_, err = manager.ResolveWorkspaceTarget(WithWorkspaceTarget(ctx, WorkspaceTargetNative), remoteTestBotID, "")
	if !errors.Is(err, ErrRemoteRuntimeOffline) || len(store.got) != 1 || store.got[0] != remoteTestTargetID {
		t.Fatalf("default escaped selected PC: get=%v err=%v", store.got, err)
	}
}

func TestUnboundPublicCallCannotUseImplicitPrimaryComputer(t *testing.T) {
	store := &scopeBindingStore{fakeRemoteBindingStore: fakeRemoteBindingStore{records: []dbstore.BotRemoteRuntimeBindingRecord{{ID: remoteTestTargetID, BotID: remoteTestBotID, IsPrimary: true, RuntimeUserID: remoteTestOwnerID, BotOwnerUserID: remoteTestOwnerID}}}}
	manager := NewManager(slog.Default(), nil, nil, config.WorkspaceConfig{}, "", nil)
	manager.SetRemoteWorkspaceService(&RemoteWorkspaceService{store: store})
	ctx, cancel := toolcontext.Bind(t.Context(), toolcontext.Session{PublicRequest: true})
	defer cancel()
	if _, err := manager.ResolveWorkspaceTarget(ctx, remoteTestBotID, ""); !errors.Is(err, toolcontext.ErrComputerBinding) || len(store.got) != 0 {
		t.Fatalf("implicit primary bypass: %v lookups=%v", err, store.got)
	}
}
