package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

type historicalStore struct {
	*fixtureStore
	connectionRevoked bool
}

func (s *historicalStore) OwnedHTTPMCPConnectionBot(_ context.Context, owner, connection string) (string, error) {
	if s.connectionRevoked || owner != "owner" || connection != "11111111-1111-4111-8111-111111111111" {
		return "", errors.New("revoked")
	}
	return "owner-bot", nil
}

func TestLegacyIdentityHistoryIsReadOnlyAndRevocable(t *testing.T) {
	source, store, transport, initial := followupFixture(t, true)
	mount := store.mounts["pc-a"]
	old := agenvo.Target{ConnectionID: "11111111-1111-4111-8111-111111111111", DeviceID: "old-device", InstanceID: "old-instance", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`}
	mount.RuntimeProvider = agenvo.Provider
	mount.RuntimeProviderConfig, _ = json.Marshal(old)
	store.mounts["pc-a"] = mount
	hs := &historicalStore{fixtureStore: store}
	source.service.relay = agenvo.NewService(hs, permissionChecker{}, transport.call)
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "read-run"}
	in := Followup{OperationID: initial.ID, RequestID: uuid.NewString(), Mode: "bound_session", Task: "historical input"}
	row := store.records[initial.ID]
	var d Details
	if err := json.Unmarshal(row.Details, &d); err != nil {
		t.Fatal(err)
	}
	d.Followups = []FollowupRecord{{RequestID: in.RequestID, Fingerprint: fingerprint(in), Mode: in.Mode, Progress: agenvo.AgentFollowupProgress{State: "unknown"}}}
	row.State = "unknown"
	row.TargetFingerprint = agenvo.TargetFingerprint(old)
	row.Details, _ = json.Marshal(d)
	store.records[row.ID] = row
	before := observationSnapshot(t, store)
	got, err := source.service.observe(t.Context(), b, row.ID, false)
	if err != nil || got.State != "unknown" {
		t.Fatalf("history: %+v %v", got, err)
	}
	got, err = source.service.followup(t.Context(), b, in)
	if !errors.Is(err, agenvo.ErrUnknown) || got.Followup == nil || got.Followup.State != "unknown" {
		t.Fatalf("receipt: %+v %v", got, err)
	}
	if _, err := source.service.cancel(t.Context(), b, row.ID); err == nil {
		t.Fatal("legacy write allowed")
	}
	if _, err := source.service.observe(t.Context(), b, row.ID, true); err == nil {
		t.Fatal("legacy refresh allowed")
	}
	if len(transport.methods) != 0 || string(before) != string(observationSnapshot(t, store)) {
		t.Fatal("history invoked transport or changed ledger")
	}
	bad := b
	bad.UserID = "other"
	if _, err := source.service.observe(t.Context(), bad, row.ID, false); err == nil {
		t.Fatal("foreign user read")
	}
	changed := store.mounts["pc-a"]
	other := old
	other.InstanceID = "changed-instance"
	changed.RuntimeProviderConfig, _ = json.Marshal(other)
	store.mounts["pc-a"] = changed
	if _, err := source.service.observe(t.Context(), b, row.ID, false); !errors.Is(err, agenvo.ErrTargetChanged) {
		t.Fatalf("changed fingerprint: %v", err)
	}
	store.mounts["pc-a"] = mount
	hs.connectionRevoked = true
	if _, err := source.service.observe(t.Context(), b, row.ID, false); err == nil {
		t.Fatal("revoked connection read")
	}
	hs.connectionRevoked = false
	store.revoked = true
	if _, err := source.service.followup(t.Context(), b, in); err == nil {
		t.Fatal("revoked mount read")
	}
}
