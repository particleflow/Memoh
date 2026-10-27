package botdelegation

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestPersistedPCScopeMapsSameRuntimeWithoutClearingSourceBinding(t *testing.T) {
	a, f, b, p := fixture()
	root := f.sessions["root"]
	root.Metadata = map[string]any{"workspace_target_id": p.SourceMountID}
	f.sessions["root"] = root
	b.SourceSelection = p.SourceMountID
	r := Record{Identity: b, SourceSelection: p.SourceMountID, Payload: Payload{Devices: []DevicePair{p}, TargetSelection: p.TargetMountID}}
	s := &Service{Authority: a}
	parent := toolcontext.WithComputerScope(t.Context(), p.SourceMountID)
	stillParent := s.BindScope(parent, r)
	if toolcontext.ComputerScope(stillParent) != p.SourceMountID {
		t.Fatal("source scope was cleared or replaced")
	}
	if err := toolcontext.CheckComputerTarget(stillParent, p.TargetMountID); !errors.Is(err, toolcontext.ErrComputerScope) {
		t.Fatalf("source context adopted a foreign mount: %v", err)
	}
	destination := toolcontext.WithComputerScope(s.BindScope(t.Context(), r), p.TargetMountID)
	if err := toolcontext.CheckDelegationTarget(destination, b.TargetBotID, p.TargetMountID, bots.PermissionWorkspaceRead); err != nil {
		t.Fatalf("same PC through destination mount: %v", err)
	}
	if err := toolcontext.CheckDelegationTarget(destination, b.TargetBotID, "pc-b", bots.PermissionWorkspaceRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("other PC: %v", err)
	}
	if err := toolcontext.CheckDelegationTarget(destination, b.TargetBotID, "native", bots.PermissionWorkspaceRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("PC scope expanded to native: %v", err)
	}
	f.denied["manager/chat"] = true
	if err := toolcontext.CheckDelegation(destination); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked later tool: %v", err)
	}
}

func TestRemoteSourceKeepsProvenanceForNativeSpecialistScope(t *testing.T) {
	a, f, b, p := nativeFixture(t)
	r := Record{Identity: b, SourceSelection: p.SourceMountID, Payload: Payload{TargetSelection: "native"}}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var restored Record
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	service := &Service{Authority: a}
	source := toolcontext.WithComputerScope(t.Context(), p.SourceMountID)
	preserved := service.BindScope(source, restored)
	if toolcontext.ComputerScope(preserved) != p.SourceMountID {
		t.Fatal("source computer scope overwritten")
	}
	if err := toolcontext.CheckComputerTarget(preserved, "native"); !errors.Is(err, toolcontext.ErrComputerScope) {
		t.Fatalf("foreign source context broadened: %v", err)
	}
	destination := toolcontext.WithComputerScope(service.BindScope(t.Context(), restored), "native")
	for _, permission := range []string{"", bots.PermissionWorkspaceRead, bots.PermissionWorkspaceExec} {
		if err := toolcontext.CheckDelegationTarget(destination, b.TargetBotID, "native", permission); err != nil {
			t.Fatal(err)
		}
	}
	ids, scoped, err := toolcontext.DelegationTargets(destination, b.TargetBotID)
	if err != nil || !scoped || len(ids) != 1 || ids[0] != "native" {
		t.Fatalf("targets=%v scoped=%v err=%v", ids, scoped, err)
	}
	if err := toolcontext.CheckDelegationTarget(destination, b.TargetBotID, p.TargetMountID, bots.PermissionWorkspaceRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("specialist acquired PC: %v", err)
	}
	if err := toolcontext.CheckDelegationTarget(destination, b.SourceBotID, "native", bots.PermissionWorkspaceRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("foreign bot adopted scope: %v", err)
	}
	for _, bot := range []string{b.SourceBotID, b.TargetBotID} {
		f.denied[bot+"/workspace_exec"] = true
		if err := toolcontext.CheckDelegationTarget(destination, b.TargetBotID, "native", bots.PermissionWorkspaceExec); !errors.Is(err, ErrDenied) {
			t.Fatalf("revoked execution for %s: %v", bot, err)
		}
		delete(f.denied, bot+"/workspace_exec")
	}
	m := f.mounts[b.SourceBotID+"/"+p.SourceMountID]
	m.RuntimeRevoked = true
	f.mounts[b.SourceBotID+"/"+p.SourceMountID] = m
	if err := toolcontext.CheckDelegation(destination); !errors.Is(err, ErrDenied) {
		t.Fatalf("source provenance revocation ignored: %v", err)
	}
}
