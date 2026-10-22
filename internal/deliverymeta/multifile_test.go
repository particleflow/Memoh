package deliverymeta

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBoundedFilePlanIdentity(t *testing.T) {
	task := Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "fixture", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: RootHash(`C:\Fixture`)}
	artifacts := []Artifact{{SourceRef: "one", Mime: "text/plain"}, {SourceRef: "two", Mime: "text/plain"}}
	p := Plan{Version: 2, Task: task, Artifacts: artifacts}
	if !p.Valid() || p.DeliveryID() != "" || p.ManifestName() != "" {
		t.Fatal("group treated as one upload")
	}
	files := p.FilePlans()
	p.Artifacts = []Artifact{artifacts[1], artifacts[0]}
	reordered := p.FilePlans()
	if len(files) != 2 || files[0].DeliveryID() != reordered[1].DeliveryID() || files[0].DeliveryID() == files[1].DeliveryID() {
		t.Fatal("unstable per-file identity")
	}
	if (Closed{Plan: p, Closed: true, SHA256: strings.Repeat("c", 64), SizeBytes: 7}).Valid() {
		t.Fatal("group accepted by single-file decoder")
	}
	for _, bad := range [][]Artifact{nil, {}, {artifacts[0], artifacts[0]}, {artifacts[0], {SourceRef: "ONE", Mime: "text/plain"}}} {
		if ValidArtifacts(bad) {
			t.Fatal("invalid group accepted")
		}
	}
	all := make([]Artifact, MaxArtifacts+1)
	for i := range all {
		all[i] = Artifact{SourceRef: fmt.Sprintf("file_%d", i), Mime: "text/plain"}
	}
	if !ValidArtifacts(all[:MaxArtifacts]) || ValidArtifacts(all) {
		t.Fatal("16/17 boundary")
	}
	p.Version = 1
	p.Artifact = artifacts[0]
	if p.Valid() {
		t.Fatal("ambiguous single/group accepted")
	}
}
