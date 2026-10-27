package sessiontask

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type oldRegistration struct {
	row    dbstore.BotRemoteRuntimeBindingRecord
	denied bool
}

func (s *oldRegistration) GetMount(_ context.Context, bot, id string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	if s.denied || s.row.BotID != bot || s.row.ID != id {
		return dbstore.BotRemoteRuntimeBindingRecord{}, errors.New("revoked")
	}
	return s.row, nil
}

func (s *oldRegistration) OwnedHTTPMCPConnectionBot(_ context.Context, owner, connection string) (string, error) {
	if s.denied || owner != "owner" || connection != "11111111-1111-4111-8111-111111111111" {
		return "", errors.New("revoked")
	}
	return "owner-bot", nil
}

type oldPermission struct{}

func (oldPermission) HasBotPermission(_ context.Context, bot, user, permission string) (bool, error) {
	return bot == "bot" && user == "user" && permission == "workspace_exec", nil
}

func TestOldIdentityTaskProjectionRequiresCurrentAuthorization(t *testing.T) {
	f := newFixture()
	target := agenvo.Target{ConnectionID: "11111111-1111-4111-8111-111111111111", DeviceID: "old-device", InstanceID: "old-instance", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`}
	raw, _ := json.Marshal(target)
	old := &oldRegistration{row: dbstore.BotRemoteRuntimeBindingRecord{ID: "pc-a", BotID: "bot", RuntimeID: "old-runtime", RuntimeUserID: "owner", BotOwnerUserID: "owner", RuntimeProvider: agenvo.Provider, RuntimeProviderConfig: raw}}
	row := f.operations[f.op]
	row.TargetFingerprint = agenvo.TargetFingerprint(target)
	row.State = "unknown"
	f.operations[f.op] = row
	f.service = NewService(f, f, dirs{f}, agenvo.NewService(old, oldPermission{}, nil), "synthetic-cursor-secret")
	task, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
	if err != nil || task.Status != "unknown" {
		t.Fatalf("old task: %+v %v", task, err)
	}
	old.denied = true
	if _, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("revocation: %v", err)
	}
}
