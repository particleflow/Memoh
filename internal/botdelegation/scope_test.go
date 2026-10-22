package botdelegation

import (
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
