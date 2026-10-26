package userruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type retiredRuntimeStore struct {
	dbstore.UserRuntimeStore
	rows []dbstore.UserRuntimeRecord
}

func (s retiredRuntimeStore) ExpirePendingUserRuntimes(context.Context, string) error { return nil }
func (s retiredRuntimeStore) ListUserRuntimes(context.Context, string) ([]dbstore.UserRuntimeRecord, error) {
	return s.rows, nil
}

func TestLegacyRegistrationRejectedBeforeCredentialOrSourceAccess(t *testing.T) {
	// Unimplemented store calls panic: rejection must precede any registry write.
	s := NewService(retiredRuntimeStore{}, nil)
	for _, req := range []CreateRuntimeRequest{
		{Name: "legacy", Provider: "agenvo"},
		{Name: "legacy", Agenvo: &agenvo.Target{}},
		{Name: "legacy", Provider: "memoh", Agenvo: &agenvo.Target{}},
	} {
		if _, err := s.CreateRuntime(t.Context(), "owner", req); !errors.Is(err, agenvo.ErrLegacyRetired) {
			t.Fatalf("legacy registration accepted: %v", err)
		}
	}
}

func TestRuntimeListOmitsActiveLegacyWithoutProbingAndKeepsNative(t *testing.T) {
	now := time.Now()
	rows := []dbstore.UserRuntimeRecord{
		{ID: "old", UserID: "owner", Provider: "agenvo", ActivatedAt: now},
		{ID: serviceTestRuntimeID, UserID: "owner", Provider: "memoh", ActivatedAt: now},
	}
	s := NewService(retiredRuntimeStore{rows: rows}, nil)
	// No adapter is necessary to list native records; old rows must not be probed.
	got, err := s.ListRuntimes(t.Context(), "owner")
	if err != nil || len(got) != 1 || got[0].ID != serviceTestRuntimeID {
		t.Fatalf("inventory: %+v %v", got, err)
	}
	if rows[0].Provider != "agenvo" || rows[0].ActivatedAt != now {
		t.Fatal("historical row mutated")
	}
}

func TestTransferTargetRejectsRetiredProvider(t *testing.T) {
	if _, err := transferRuntimeTarget(dbstore.RuntimeTransferRecord{Provider: "agenvo"}); !errors.Is(err, ErrTransferCredential) {
		t.Fatal(err)
	}
}
