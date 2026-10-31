package userruntime

import (
	"encoding/json"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func TestNativeIdentitySurvivesOfflineAndRejectsInvalidProfile(t *testing.T) {
	profile := agenvo.NativeHerdrProfile{
		InstanceID: "synthetic-herdr", Session: "default", WorkDir: `C:\Synthetic\work`,
		BashPath: `C:\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`,
		HerdrConfigRoot: `C:\Synthetic\config\herdr`,
	}
	raw, err := json.Marshal(map[string]any{"herdr": profile})
	if err != nil {
		t.Fatal(err)
	}
	row := dbstore.UserRuntimeRecord{ID: serviceTestRuntimeID, Provider: "memoh", ProviderConfig: raw}
	offline := runtimeFromRecord(row, nil)
	if offline.Online || offline.NativeHerdr == nil {
		t.Fatalf("offline profile binding missing: %+v", offline)
	}
	binding := *offline.NativeHerdr
	if binding.ConnectionID != row.ID || binding.DeviceID != row.ID || binding.InstanceID != profile.InstanceID || binding.Session != profile.Session || binding.TargetFingerprint == "" {
		t.Fatalf("wrong principal: %+v", binding)
	}
	online := runtimeFromRecord(row, &Connection{Info: RuntimeInfo{OS: "windows"}})
	if !online.Online || online.NativeHerdr == nil || *online.NativeHerdr != binding {
		t.Fatalf("connection state changed principal: %+v", online)
	}
	profile.Session = "replacement"
	row.ProviderConfig, err = json.Marshal(map[string]any{"herdr": profile})
	if err != nil {
		t.Fatal(err)
	}
	changed := runtimeFromRecord(row, nil)
	if changed.NativeHerdr == nil || changed.NativeHerdr.TargetFingerprint == binding.TargetFingerprint {
		t.Fatal("profile replacement did not change observable fingerprint")
	}
	for _, provider := range []string{"memoh", "agenvo"} {
		row.Provider = provider
		row.ProviderConfig = []byte(`{"herdr":null}`)
		if runtimeFromRecord(row, nil).NativeHerdr != nil {
			t.Fatalf("invalid %s profile exposed native principal", provider)
		}
	}
}
