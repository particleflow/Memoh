package deliverymeta

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExplicitResultFilenameContract(t *testing.T) {
	for _, name := range []string{"windows_photo_a0_result.png", "report.final.pdf", "result_1", "result-one", "MEMOH_DELIVERY_legacy"} {
		if !(Artifact{SourceRef: name, Mime: "image/png"}).Valid() {
			t.Errorf("exact flat result filename rejected: %s", name)
		}
	}
	for _, name := range []string{"../photo.png", `dir\photo.png`, "dir/photo.png", "C:photo.png", "photo.png:stream", ".photo.png", "photo..png", "photo.png.", "photo.png ", "CON.png", "nul.txt", "COM1.png", "lpt9.pdf", "memoh_delivery_photo.png", "photo$(touch_bad).png", strings.Repeat("a", 61) + ".png"} {
		if (Artifact{SourceRef: name, Mime: "image/png"}).Valid() {
			t.Errorf("unsafe/aliased result filename accepted: %s", name)
		}
	}
	if ValidArtifacts([]Artifact{{SourceRef: "photo.png", Mime: "image/png"}, {SourceRef: "PHOTO.PNG", Mime: "image/png"}}) {
		t.Fatal("Windows case aliases admitted twice")
	}
}

func TestExplicitFilenameKeepsLegacyDeliveryIdentity(t *testing.T) {
	p := Plan{Version: 1, Task: Task{OperationID: "11111111-1111-4111-8111-111111111111", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}, Artifact: Artifact{SourceRef: "windows_photo_a0_result", Mime: "image/png"}}
	const original = "1f487c23733928d14aa1a6f9619dfa8cc533bd29d287886c08a8c454babf1896"
	if p.DeliveryID() != original {
		t.Fatal("historical logical source changed delivery identity")
	}
	raw, _ := json.Marshal(p)
	var resumed Plan
	if json.Unmarshal(raw, &resumed) != nil || resumed.DeliveryID() != original || resumed.Artifact.SourceRef != p.Artifact.SourceRef {
		t.Fatal("original plan changed during serialization")
	}
	// A different literal declaration is a different future delivery. Never
	// change an already-completed operation's old source_ref to adopt a suffix.
	resumed.Artifact.SourceRef += ".png"
	if resumed.DeliveryID() == original {
		t.Fatal("changed declaration silently adopted historical delivery")
	}
}
