package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	dbstore "github.com/felinics/memoh/internal/db/store"
)

const syntheticNativeID = "11111111-1111-4111-8111-111111111111"

func syntheticNativeProfile() NativeHerdrProfile {
	return NativeHerdrProfile{InstanceID: "herdr-main", Session: "default", WorkDir: `C:\Synthetic\work`, BashPath: `C:\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\bin\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\config\herdr`, Agent: AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "fixture", ConfigDir: `C:\Synthetic\pi`}}
}

func TestNativeTargetSeparatesAuthenticationAndPinsLocalReferences(t *testing.T) {
	p := syntheticNativeProfile()
	raw, _ := json.Marshal(map[string]any{"herdr": p, "unrelated": "preserved"})
	target, err := DecodeNativeTarget(syntheticNativeID, raw)
	if err != nil || target.NativeRuntimeID != syntheticNativeID || target.ConnectionID != syntheticNativeID || target.DeviceID != syntheticNativeID {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	for _, field := range []string{"runtime", "cli", "config", "session", "agent"} {
		changed := target
		switch field {
		case "runtime":
			changed.NativeRuntimeID = "other"
		case "cli":
			changed.HerdrCLI = `C:\Other\herdr.exe`
		case "config":
			changed.HerdrConfigRoot = `C:\Other\herdr`
		case "session":
			changed.Session = "other"
		case "agent":
			changed.Agent.Model = "other"
		}
		if TargetFingerprint(changed) == TargetFingerprint(target) {
			t.Fatalf("%s not pinned", field)
		}
	}
	spoof, _ := json.Marshal(target)
	if _, err := DecodeTarget(spoof); !errors.Is(err, ErrInvalidTarget) {
		t.Fatal("MCP config switched to native auth")
	}
	for _, bad := range []string{"legacy", "wrapper", "root", "unknown_secret", "runtime", "unc", "wildcard", "stream"} {
		profile := p
		switch bad {
		case "legacy":
			profile.Agent.ExecutionMode = ""
		case "wrapper":
			profile.HerdrCLI = `C:\Synthetic\herdr.cmd`
		case "root":
			profile.HerdrConfigRoot = `C:\Synthetic\different`
		case "unc":
			profile.HerdrCLI = `\\server\share\herdr.exe`
		case "wildcard":
			profile.HerdrConfigRoot = `C:\Synthetic\*\herdr`
		case "stream":
			profile.HerdrCLI = `C:\Synthetic\herdr.exe:stream.exe`
		}
		raw, _ := json.Marshal(map[string]any{"herdr": profile})
		id := syntheticNativeID
		if bad == "runtime" {
			id = ""
		}
		if bad == "unknown_secret" {
			var v map[string]map[string]any
			_ = json.Unmarshal(raw, &v)
			v["herdr"]["token"] = "synthetic-not-a-credential"
			raw, _ = json.Marshal(v)
		}
		if _, err := DecodeNativeTarget(id, raw); !errors.Is(err, ErrInvalidTarget) {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestNativeProducerLocatorIsOptionalAtomicAndFingerprintBound(t *testing.T) {
	p := syntheticNativeProfile()
	before, err := p.target(syntheticNativeID)
	if err != nil {
		t.Fatal(err)
	}
	p.Producer = ProducerLocator{ProgramPath: `C:\Synthetic\producer.exe`, ConfigDir: `C:\Synthetic\producer-config`, StateDir: `C:\Synthetic\producer-state`}
	target, err := p.target(syntheticNativeID)
	if err != nil || target.Producer != p.Producer || TargetFingerprint(target) == TargetFingerprint(before) {
		t.Fatal("producer locator is not task-bound", err)
	}
	for _, field := range []string{"program", "config", "state"} {
		changed := target
		switch field {
		case "program":
			changed.Producer.ProgramPath = `C:\Other\producer.exe`
		case "config":
			changed.Producer.ConfigDir = `C:\Other\config`
		case "state":
			changed.Producer.StateDir = `C:\Other\state`
		}
		if TargetFingerprint(changed) == TargetFingerprint(target) {
			t.Fatalf("%s locator is not pinned", field)
		}
	}
	for _, bad := range []ProducerLocator{
		{ProgramPath: p.Producer.ProgramPath},
		{ProgramPath: `C:\Synthetic\producer.cmd`, ConfigDir: p.Producer.ConfigDir, StateDir: p.Producer.StateDir},
		{ProgramPath: p.Producer.ProgramPath, ConfigDir: `C:/Synthetic/config`, StateDir: p.Producer.StateDir},
		{ProgramPath: p.Producer.ProgramPath, ConfigDir: `C:\Synthetic\config\`, StateDir: p.Producer.StateDir},
		{ProgramPath: p.Producer.ProgramPath, ConfigDir: p.Producer.ConfigDir, StateDir: `C:\Synthetic\\state`},
	} {
		p.Producer = bad
		if _, err := p.target(syntheticNativeID); !errors.Is(err, ErrInvalidTarget) {
			t.Fatalf("accepted incomplete or noncanonical locator: %+v", bad)
		}
	}
}

func TestNativeServiceChecksEveryCallWithoutMCPAndDoesNotReplay(t *testing.T) {
	s, store, f := serviceFixture(t)
	store.record.RuntimeID = syntheticNativeID
	store.record.RuntimeProvider = "memoh"
	store.record.RuntimeProviderConfig, _ = json.Marshal(map[string]any{"herdr": syntheticNativeProfile()})
	s.SetNativeCaller(nil)
	missing, _, err := s.Client(t.Context(), request().Scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := missing.Probe(t.Context(), request().Scope); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing native caller lost unavailable classification", err)
	}
	calls := 0
	s.SetNativeCaller(func(context.Context, Scope, Target, string, map[string]any) (Receipt, error) {
		calls++
		return Receipt{}, ErrUnknown
	})
	c, target, err := s.Client(t.Context(), request().Scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.native(t.Context(), request().Scope, target, "pane.run", map[string]any{}); !errors.Is(err, ErrUnknown) || calls != 1 || len(f.calls) != 0 {
		t.Fatalf("err=%v native=%d MCP=%d", err, calls, len(f.calls))
	}
	store.record.RuntimeRevoked = true
	if _, err = c.native(t.Context(), request().Scope, target, "pane.read", nil); !errors.Is(err, ErrForbidden) || calls != 1 {
		t.Fatal("revoked target contacted")
	}
	store.record.RuntimeRevoked = false
	p := syntheticNativeProfile()
	p.HerdrConfigRoot = `C:\Changed\herdr`
	store.record.RuntimeProviderConfig, _ = json.Marshal(map[string]any{"herdr": p})
	if _, err = c.native(t.Context(), request().Scope, target, "pane.read", nil); !errors.Is(err, ErrTargetChanged) || calls != 1 {
		t.Fatal("changed registration contacted")
	}
}

type nativeConfigurationStore struct {
	*serviceStore
	raw    dbstore.JSON
	writes int
}

func (s *nativeConfigurationStore) OwnedNativeHerdrRuntime(_ context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	if owner != "owner" || id != syntheticNativeID {
		return dbstore.UserRuntimeRecord{}, ErrForbidden
	}
	return dbstore.UserRuntimeRecord{ID: id, UserID: owner, Provider: "memoh", ProviderConfig: s.raw}, nil
}

func (s *nativeConfigurationStore) ConfigureNativeHerdrRuntime(ctx context.Context, owner, id string, before, after dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	if string(before) != string(s.raw) {
		return dbstore.UserRuntimeRecord{}, ErrTargetChanged
	}
	s.raw = after
	s.writes++
	return s.OwnedNativeHerdrRuntime(ctx, owner, id)
}

func TestNativeConfigurationPreservesOwnerDataAndDoesNotEnrollOrDispatch(t *testing.T) {
	s, base, f := serviceFixture(t)
	store := &nativeConfigurationStore{serviceStore: base, raw: []byte(`{"unrelated":{"version":7}}`)}
	s.store = store
	if _, err := s.ConfigureNativeHerdr(t.Context(), "foreign", syntheticNativeID, syntheticNativeProfile()); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	row, err := s.ConfigureNativeHerdr(t.Context(), "owner", syntheticNativeID, syntheticNativeProfile())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	_ = json.Unmarshal(row.ProviderConfig, &got)
	if string(got["unrelated"]) != `{"version":7}` || store.writes != 1 || len(f.calls) != 0 {
		t.Fatalf("unexpected side effects: %+v", got)
	}
	if _, err = DecodeNativeTarget(row.ID, row.ProviderConfig); err != nil {
		t.Fatal(err)
	}
}
