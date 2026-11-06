package workdir

import (
	"errors"
	"testing"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestComputerScopeFiltersWorkdirCatalogAndRejectsForeignResolution(t *testing.T) {
	store := &fakeWorkdirStore{list: []dbstore.BotWorkdirRecord{{ID: "a", TargetKind: TargetKindRemote, RemoteBindingID: "pc-a"}, {ID: "b", TargetKind: TargetKindRemote, RemoteBindingID: "pc-b"}, {ID: "nas", TargetKind: TargetKindNative}}}
	s := &Service{store: store}
	ctx := toolcontext.WithComputerScope(t.Context(), "pc-a")
	items, err := s.List(ctx, "bot", false)
	if err != nil || len(items) != 1 || items[0].ID != "a" {
		t.Fatalf("catalog=%+v err=%v", items, err)
	}
	for _, record := range store.list[1:] {
		store.get = record
		if _, err = s.Get(ctx, "bot", record.ID); !errors.Is(err, toolcontext.ErrComputerScope) {
			t.Fatal("foreign workdir exposed")
		}
		if _, err = s.ResolveForSession(ctx, "bot", record.ID); !errors.Is(err, toolcontext.ErrComputerScope) {
			t.Fatal("foreign workdir bound")
		}
	}
}
