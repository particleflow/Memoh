package deliverymeta

import "testing"

func TestArtifactRejectsPathsAndWindowsDevices(t *testing.T) {
	for _, name := range []string{"../secret", `foo\bar`, "foo/bar", "CON", "nul", "COM1", "lpt9", ManifestPrefix + "abc"} {
		if (Artifact{SourceRef: name, Mime: "text/plain"}).Valid() {
			t.Fatal("unsafe source reference", name)
		}
	}
	if !(Artifact{SourceRef: "result_1", Mime: "application/pdf"}).Valid() {
		t.Fatal("valid logical result rejected")
	}
	if RootHash(`C:\Fixture`) != RootHash("c:/fixture/") {
		t.Fatal("existing Windows root spellings disagree")
	}
}
