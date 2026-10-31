package catalogsync

import (
	"encoding/json"
	"testing"
)

func TestMergeDiscoveryPreservesUserChangesAndUnknowns(t *testing.T) {
	current, err := NewConfig([]byte(`{"context_window":100,"compatibilities":["vision"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]json.RawMessage
	if err = json.Unmarshal(current, &values); err != nil {
		t.Fatal(err)
	}
	values["context_window"] = json.RawMessage("222")
	delete(values, "compatibilities") // user explicitly removed this flag
	values["description"] = json.RawMessage(`"custom"`)
	current, err = json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := Merge(current, []byte(`{"context_window":300,"compatibilities":["vision","reasoning"],"max_output_tokens":64}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(merged, &values); err != nil {
		t.Fatal(err)
	}
	if string(values["context_window"]) != "222" || values["compatibilities"] != nil || string(values["description"]) != `"custom"` || string(values["max_output_tokens"]) != "64" {
		t.Fatalf("merged = %s", merged)
	}
}

func TestMergeLeavesLegacyConfigurationAndUnknownFactsAlone(t *testing.T) {
	got, err := Merge([]byte(`{"description":"legacy","context_window":123}`), []byte(`{"context_window":456,"compatibilities":["vision"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err = json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["context_window"]) != "123" || m["compatibilities"] != nil || m["catalog_discovered"] != nil {
		t.Fatalf("legacy changed: %s", got)
	}
}

func TestUserEditRemainsOwnedEvenWhenRestoredToEarlierValue(t *testing.T) {
	current, err := NewConfig([]byte(`{"context_window":100}`))
	if err != nil {
		t.Fatal(err)
	}
	edited, err := RecordUserEdits(current, []byte(`{"context_window":222}`))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RecordUserEdits(edited, []byte(`{"context_window":100}`))
	if err != nil {
		t.Fatal(err)
	}
	merged, err := Merge(restored, []byte(`{"context_window":300}`))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err = json.Unmarshal(merged, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["context_window"]) != "100" {
		t.Fatalf("explicit user setting overwritten: %s", merged)
	}
}
