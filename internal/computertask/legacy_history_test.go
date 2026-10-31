package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
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

func (*historicalStore) ReadComputerDeliveryEntry(context.Context, dbstore.ComputerOperationRecord, string) (dbstore.ComputerDeliveryEntry, error) {
	return dbstore.ComputerDeliveryEntry{}, errors.New("unexpected entry read")
}

func (*historicalStore) SaveComputerDeliveryEntry(context.Context, dbstore.ComputerOperationRecord, dbstore.ComputerDeliveryEntry) (dbstore.ComputerOperationRecord, error) {
	panic("history must not write delivery entries")
}

func (*historicalStore) ListComputerDeliveryEntries(context.Context, dbstore.ComputerOperationRecord, string, int) ([]dbstore.ComputerDeliveryEntry, error) {
	entry := deliveryEntry{Reference: DeliveryRequest{Version: 1, Direction: "upload", ContentHash: strings.Repeat("c", 64), SizeBytes: 7, Mime: "text/plain"}, Scope: strings.Repeat("d", 64), At: time.Unix(100, 0), Outcome: "committed"}
	raw, _ := json.Marshal(entry)
	return []dbstore.ComputerDeliveryEntry{{DeliveryID: "historical-delivery", Version: 1, Ordinal: 1, Data: raw}}, nil
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
	source.service.store = hs
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "read-run"}
	in := Followup{OperationID: initial.ID, RequestID: uuid.NewString(), Mode: "bound_session", Task: "historical input"}
	row := store.records[initial.ID]
	var d Details
	if err := json.Unmarshal(row.Details, &d); err != nil {
		t.Fatal(err)
	}
	d.NativeEvidence = &NativeEvidenceLedger{Version: 1, Observations: []NativeObservationRecord{{ID: "historical-sample", Origin: "native_read"}}}
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
	checkPages := func(binding toolcontext.ComputerBinding, allowed bool) {
		t.Helper()
		history, historyErr := source.service.nativeHistory(t.Context(), binding, row.ID, 0)
		delivery, deliveryErr := source.service.deliveryPage(t.Context(), binding, row.ID, "")
		receipt, receiptErr := source.FollowupNative(t.Context(), binding, in)
		if allowed {
			if historyErr != nil || history.NativeEvidence == nil || len(history.NativeEvidence.Observations) != 1 || history.NativeEvidence.Observations[0].ID != "historical-sample" || deliveryErr != nil || len(delivery.Files) != 1 || delivery.Files[0].DeliveryID != "historical-delivery" || !errors.Is(receiptErr, agenvo.ErrUnknown) || receipt.Followup == nil || receipt.Followup.State != "unknown" {
				t.Fatalf("history=%v delivery=%v receipt=%+v error=%v", historyErr, deliveryErr, receipt, receiptErr)
			}
		} else if historyErr == nil || deliveryErr == nil || receipt.Followup != nil || !errors.Is(receiptErr, ErrDenied) && !errors.Is(receiptErr, agenvo.ErrTargetChanged) {
			t.Fatalf("unauthorized history=%v delivery=%v receipt=%+v error=%v", historyErr, deliveryErr, receipt, receiptErr)
		}
	}
	checkPages(b, true)
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
	checkPages(bad, false)
	if _, err := source.service.observe(t.Context(), bad, row.ID, false); err == nil {
		t.Fatal("foreign user read")
	}
	changed := store.mounts["pc-a"]
	other := old
	other.InstanceID = "changed-instance"
	changed.RuntimeProviderConfig, _ = json.Marshal(other)
	store.mounts["pc-a"] = changed
	checkPages(b, true)
	if _, err := source.service.observe(t.Context(), b, row.ID, false); err != nil {
		t.Fatalf("changed fingerprint: %v", err)
	}
	store.mounts["pc-a"] = mount
	hs.connectionRevoked = true
	checkPages(b, true)
	if _, err := source.service.observe(t.Context(), b, row.ID, false); err != nil {
		t.Fatal(err)
	}
	hs.connectionRevoked = false
	store.revoked = true
	checkPages(b, true)
	source.SetResultAssociator(func(context.Context, dbstore.ComputerOperationRecord, []DeliveryRequest) error {
		t.Fatal("history created attachment association")
		return nil
	})
	delete(store.mounts, "pc-a")
	client := standardClient(t, source, "pc-a", false)
	read := parseResult(t, invoke(t, client, "computer_task_get", map[string]any{"operation_id": row.ID}, false))
	if read.ID != row.ID || read.State != "unknown" {
		t.Fatal("deleted binding changed historical receipt")
	}

	checkPages(b, true)
	if _, err := source.service.observe(t.Context(), b, row.ID, true); err == nil {
		t.Fatal("deleted registration allowed live refresh")
	}
	if string(before) != string(observationSnapshot(t, store)) || len(transport.methods) != 0 {
		t.Fatal("history mutated or dispatched")
	}
	row.State = "completed"
	store.records[row.ID] = row
	completedBefore := observationSnapshot(t, store)
	read = parseResult(t, invoke(t, client, "computer_task_get", map[string]any{"operation_id": row.ID}, false))
	if read.State != "completed" || string(completedBefore) != string(observationSnapshot(t, store)) {
		t.Fatal("completed archive changed")
	}
}
