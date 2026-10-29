package sessiontask

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestLiveComputerBindingRequiresCurrentRegistrationAfterOwnedHistory(t *testing.T) {
	f := newFixture()
	r := f.operations[f.op]
	before, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := f.service.ComputerBinding(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
	if err != nil || binding.SessionID != f.grandchild || binding.TargetID != r.TargetID || f.liveReads != 1 {
		t.Fatalf("live binding=%+v reads=%d err=%v", binding, f.liveReads, err)
	}
	delete(f.targets, r.TargetID)
	if _, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); err != nil || f.liveReads != 1 {
		t.Fatalf("history depended on live binding: reads=%d err=%v", f.liveReads, err)
	}
	if _, err := f.service.ComputerBinding(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) || f.liveReads != 2 {
		t.Fatalf("deleted binding remained live: reads=%d err=%v", f.liveReads, err)
	}
	// Ownership, the selected PC and current Bot permissions must fail before
	// even looking up a device registration.
	if _, err := f.service.GetLive(t.Context(), "bot", "other", f.root, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
		t.Fatal("foreign owner reached live target")
	}
	ctx := toolcontext.WithComputerScope(t.Context(), "pc-b")
	if _, err := f.service.GetLive(ctx, "bot", "user", f.root, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
		t.Fatal("selected PC escaped original operation")
	}
	if _, err := f.service.GetLive(t.Context(), "bot", "user", f.root, Subagent, f.child); !errors.Is(err, ErrUnavailable) {
		t.Fatal("child session fabricated a computer binding")
	}
	f.historyDenied = true
	if _, err := f.service.GetLive(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
		t.Fatal("revoked Bot permission reached live target")
	}
	if f.liveReads != 2 {
		t.Fatal("invalid history read a live registration")
	}
	after, err := json.Marshal(f.operations[f.op])
	if err != nil || string(before) != string(after) {
		t.Fatal("live authorization changed the original receipt")
	}
}

func TestLiveComputerBindingPreservesOriginalInstance(t *testing.T) {
	for _, snapshot := range []string{"legacy", "snapshot"} {
		t.Run(snapshot, func(t *testing.T) {
			f := newFixture()
			r := f.operations[f.op]
			original := agenvo.Target{NativeRuntimeID: "synthetic-runtime", DeviceID: "synthetic-device", InstanceID: "original-instance", WorkDir: `C:\original`}
			f.targets[r.TargetID] = original
			r.TargetFingerprint = agenvo.TargetFingerprint(original)
			facts := map[string]any{"kind": "herdr_pi_task"}
			if snapshot == "snapshot" {
				facts["target_snapshot"] = original
			}
			var err error
			r.Details, err = json.Marshal(facts)
			if err != nil {
				t.Fatal(err)
			}
			f.operations[f.op] = r
			if _, err := f.service.GetLive(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); err != nil {
				t.Fatal(err)
			}
			current := original
			current.WorkDir = `C:\updated-profile`
			f.targets[r.TargetID] = current
			_, err = f.service.GetLive(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
			if snapshot == "snapshot" && err != nil {
				t.Fatal("same-instance profile change lost existing identity authorization", err)
			}
			if snapshot == "legacy" && !errors.Is(err, ErrUnavailable) {
				t.Fatal("legacy row no longer required its exact fingerprint")
			}
			current.InstanceID = "replacement-instance"
			f.targets[r.TargetID] = current
			if _, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); err != nil {
				t.Fatal("instance replacement erased original history", err)
			}
			if _, err := f.service.ComputerBinding(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
				t.Fatal("same target ID authorized a replacement instance")
			}
		})
	}
}
