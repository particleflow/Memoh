package userruntime

import (
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/db"
)

func TestWorkspaceDefaultAbsentPreservesLocalWithoutProbe(t *testing.T) {
	svc, store, rpc, _ := storageFixture(t)
	svc.SetWorkspaceStorage("", "")
	before := string(store.raw)
	for _, resolve := range []func(string, string) (string, error){
		func(owner, id string) (string, error) {
			return svc.ConfiguredWorkspaceDirectory(t.Context(), owner, id)
		},
		func(owner, id string) (string, error) { return svc.SelectedWorkspaceDirectory(t.Context(), owner, id) },
	} {
		if path, err := resolve("owner", serviceTestRuntimeID); err != nil || path != "" {
			t.Fatal("absent NAS selection replaced existing local default", err)
		}
		if _, err := resolve("other", serviceTestRuntimeID); !errors.Is(err, db.ErrNotFound) {
			t.Fatal("default lookup lost owner boundary", err)
		}
	}
	if rpc.reads.Load() != 0 || store.writes != 0 || string(store.raw) != before {
		t.Fatal("absent default probed NAS or changed registration")
	}
}
