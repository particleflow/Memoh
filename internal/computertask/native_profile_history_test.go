package computertask

import (
	"encoding/json"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func TestNativeProfileHistorySurvivesCutoverWithoutExecution(t *testing.T) {
	f, s, ctx := newNativeTaskFixture(t)
	target, err := s.relay.ResolveTarget(ctx, scope(f.binding, f.binding.TargetID))
	if err != nil {
		t.Fatal(err)
	}
	d := Details{Kind: "herdr_pi_task", TargetSnapshot: &target, Phase: "batch_submitted", NativeEvidence: &NativeEvidenceLedger{Version: 1, Observations: []NativeObservationRecord{{ID: "saved-observation", Origin: "native_read"}}}}
	raw, _ := json.Marshal(d)
	r := dbstore.ComputerOperationRecord{ID: "saved-operation", BotID: f.binding.BotID, SessionID: f.binding.SessionID, UserID: f.binding.UserID, TargetID: f.binding.TargetID, RunID: f.binding.RunID, State: "unknown", TargetFingerprint: agenvo.TargetFingerprint(target), Details: raw, Revision: 7}
	f.store.records[r.ID] = r
	before := observationSnapshot(t, f.store.fixtureStore)
	check := func() {
		t.Helper()
		got, e := s.observe(ctx, f.binding, r.ID, false)
		if e != nil || got.State != "unknown" {
			t.Fatalf("history: %+v %v", got, e)
		}
		page, e := s.nativeHistory(ctx, f.binding, r.ID, 0)
		if e != nil || page.NativeEvidence == nil || len(page.NativeEvidence.Observations) != 1 {
			t.Fatalf("receipt history: %+v %v", page, e)
		}
	}
	check()
	mount := f.store.mounts[f.binding.TargetID]
	profile := f.profile
	profile.Agent.ConfigDir = `C:\Synthetic\alternate`
	mount.RuntimeProviderConfig, _ = json.Marshal(map[string]any{"herdr": profile})
	f.store.mounts[f.binding.TargetID] = mount
	check()
	if _, e := s.observe(ctx, f.binding, r.ID, true); e == nil {
		t.Fatal("old execution pin allowed result read")
	}
	if _, e := s.Recover(ctx, f.binding, r.ID); e == nil {
		t.Fatal("old execution pin allowed live recovery")
	}
	if _, e := s.cancel(ctx, f.binding, r.ID); e == nil {
		t.Fatal("old execution pin allowed cancellation")
	}
	for _, field := range []string{"user", "bot", "session"} {
		bad := f.binding
		switch field {
		case "user":
			bad.UserID = "other"
		case "bot":
			bad.BotID = "other"
		case "session":
			bad.SessionID = "other"
		}
		if _, e := s.observe(ctx, bad, r.ID, false); e == nil {
			t.Fatalf("foreign %s read", field)
		}
	}
	f.denied = true
	if _, e := s.observe(ctx, f.binding, r.ID, false); e == nil {
		t.Fatal("revoked permission read")
	}
	f.denied = false
	f.store.revoked = true
	check()
	f.store.revoked = false
	profile.InstanceID = "new-instance"
	mount.RuntimeProviderConfig, _ = json.Marshal(map[string]any{"herdr": profile})
	f.store.mounts[f.binding.TargetID] = mount
	check()
	delete(f.store.mounts, f.binding.TargetID)
	check()
	if _, e := s.observe(ctx, f.binding, r.ID, true); e == nil {
		t.Fatal("deleted registration allowed live refresh")
	}
	if string(before) != string(observationSnapshot(t, f.store.fixtureStore)) || f.batch.runs != 0 || len(f.batch.calls) != 0 {
		t.Fatal("history changed ledger or invoked transport")
	}
}
