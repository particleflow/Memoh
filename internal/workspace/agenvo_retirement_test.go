package workspace

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/config"
	ctr "github.com/felinics/memoh/internal/container"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func TestRetiredPrimaryIsHiddenAndNeverReplacedWithNative(t *testing.T) {
	row := dbstore.BotRemoteRuntimeBindingRecord{ID: remoteTestTargetID, BotID: remoteTestBotID, RuntimeID: remoteTestRuntimeID, IsPrimary: true, RuntimeProvider: agenvo.Provider, RuntimeUserID: remoteTestOwnerID, BotOwnerUserID: remoteTestOwnerID}
	store := &fakeRemoteBindingStore{records: []dbstore.BotRemoteRuntimeBindingRecord{row}}
	remote := &RemoteWorkspaceService{store: store}
	mounts, err := remote.ListMounts(t.Context(), remoteTestBotID)
	if err != nil || len(mounts) != 0 {
		t.Fatalf("retired mount remained visible: %+v %v", mounts, err)
	}
	if _, err := remote.ResolveMount(t.Context(), remoteTestBotID, remoteTestTargetID); !errors.Is(err, ErrWorkspaceTargetNotFound) {
		t.Fatal(err)
	}
	nativeInfo := ctr.ContainerInfo{ID: "workspace-" + remoteTestBotID, Image: "debian:bookworm", Labels: map[string]string{BotLabelKey: remoteTestBotID, WorkspaceLabelKey: WorkspaceLabelValue}}
	native := &legacyRouteTestService{created: true, container: nativeInfo, byLabel: []ctr.ContainerInfo{nativeInfo}}
	manager := NewManager(slog.Default(), native, nil, config.WorkspaceConfig{}, "", nil)
	manager.SetRemoteWorkspaceService(remote)
	if _, err := manager.CurrentWorkspaceTargetID(t.Context(), remoteTestBotID); !errors.Is(err, agenvo.ErrLegacyRetired) {
		t.Fatal("retired default fell back", err)
	}
	if _, err := manager.MCPClient(t.Context(), remoteTestBotID); !errors.Is(err, agenvo.ErrLegacyRetired) {
		t.Fatal("retired default acquired a client", err)
	}
	targets, err := manager.ListWorkspaceTargets(t.Context(), remoteTestBotID)
	if err != nil || len(targets) != 1 || targets[0].TargetID != "native" || targets[0].Primary {
		t.Fatalf("advertised replacement default: %+v %v", targets, err)
	}
	if !store.records[0].IsPrimary || store.records[0].RuntimeProvider != row.RuntimeProvider || store.records[0].ID != row.ID {
		t.Fatal("stored primary changed")
	}
}
