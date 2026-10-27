package agenvo

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	dbstore "github.com/felinics/memoh/internal/db/store"
)

func TestNativeProfileCutoverProposalPreservesGateAndOtherFields(t *testing.T) {
	const targetID = "22222222-2222-4222-8222-222222222222"
	const pauseID = "33333333-3333-4333-8333-333333333333"
	profile := syntheticNativeProfile()
	raw, _ := json.Marshal(map[string]any{"herdr": profile, "other": map[string]any{"retained": true}})
	row := dbstore.UserRuntimeRecord{ID: syntheticNativeID, UserID: "owner", Provider: "memoh", ActivatedAt: time.Now(), ProviderConfig: raw}
	target, err := DecodeNativeTarget(row.ID, raw)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := nativeProfileConfigDigest(raw)
	if err != nil {
		t.Fatal(err)
	}
	gate := dbstore.ComputerAdmission{ConnectionID: row.ID, DeviceID: row.ID, InstanceID: target.InstanceID, RequestID: pauseID, Revision: 4, Paused: true}
	in := NativeProfileCutoverRequest{RuntimeID: row.ID, TargetID: targetID, InstanceID: target.InstanceID, ExpectedConfigSHA256: digest, ExpectedTargetFingerprint: TargetFingerprint(target), PauseRequestID: pauseID, ExpectedPauseRevision: "4", AgentConfigDir: `C:\Synthetic\alternate-pi`}
	next, err := ValidateNativeProfileCutover("owner", targetID, row, gate, in)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(next, &decoded); err != nil {
		t.Fatal(err)
	}
	var proposed NativeHerdrProfile
	if err := json.Unmarshal(decoded["herdr"], &proposed); err != nil {
		t.Fatal(err)
	}
	profile.Agent.ConfigDir = in.AgentConfigDir
	if proposed != profile || string(decoded["other"]) != `{"retained":true}` || !gate.Paused || gate.Revision != 4 {
		t.Fatal("proposal changed unrelated facts")
	}
	if string(row.ProviderConfig) != string(raw) {
		t.Fatal("validator wrote to existing configuration")
	}
	for _, scenario := range []string{"owner", "target", "runtime", "instance", "revision", "request", "unpaused", "config", "fingerprint", "bad_path", "malformed_revision"} {
		t.Run(scenario, func(t *testing.T) {
			request, original, currentGate, owner, authorizedTarget := in, row, gate, "owner", targetID
			switch scenario {
			case "owner":
				owner = "other"
			case "target":
				authorizedTarget = "44444444-4444-4444-8444-444444444444"
			case "runtime":
				original.ID = "44444444-4444-4444-8444-444444444444"
			case "instance":
				request.InstanceID = "different"
			case "revision":
				currentGate.Revision++
			case "request":
				currentGate.RequestID = "44444444-4444-4444-8444-444444444444"
			case "unpaused":
				currentGate.Paused = false
			case "config":
				request.ExpectedConfigSHA256 = "different"
			case "fingerprint":
				request.ExpectedTargetFingerprint = "different"
			case "bad_path":
				request.AgentConfigDir = `\\server\share`
			case "malformed_revision":
				request.ExpectedPauseRevision = "04"
			}
			_, err := ValidateNativeProfileCutover(owner, authorizedTarget, original, currentGate, request)
			if err == nil || !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrInvalidTarget) && !errors.Is(err, ErrNativeProfileConflict) {
				t.Fatalf("unsafe request accepted: %v", err)
			}
		})
	}
}

func TestNativeProfileCutoverDecodeRejectsExtraFields(t *testing.T) {
	for _, raw := range []string{`{"agent_config_dir":"x","model":"override"}`, `{} {}`, `null`, `[]`} {
		_, err := DecodeNativeProfileCutover([]byte(raw))
		if err == nil {
			t.Fatalf("accepted malformed or extra input: %s", raw)
		}
	}
}
