package agenvo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func endpointFixture(t *testing.T) (*Service, *agentProfileStore, *fixture) {
	t.Helper()
	s, base, f := serviceFixture(t)
	store := &agentProfileStore{serviceStore: base, config: base.record.RuntimeProviderConfig}
	s.store = store
	return s, store, f
}

func TestEndpointConfigurationPreservesTaskTargetAndOtherConfiguration(t *testing.T) {
	s, store, f := endpointFixture(t)
	store.config, _ = replaceRegistrationField(store.config, "future_field", map[string]string{"key": "retained"})
	before, err := s.ReadEndpointConfiguration(t.Context(), "owner", "existing")
	if err != nil || before.Locator != nil || len(f.calls) != 0 {
		t.Fatal(before, err, f.calls)
	}
	locator := EndpointLocator{ProgramPath: `D:\Endpoint\endpoint.mjs`, HostRoot: `E:\Host data\owned`}
	after, err := s.ConfigureEndpoint(t.Context(), "owner", "existing", ChangeEndpointConfiguration{ExpectedConfigurationFingerprint: before.ConfigurationFingerprint, Locator: &locator})
	if err != nil || after.Locator == nil || *after.Locator != locator || after.TargetFingerprint != before.TargetFingerprint || after.ConfigurationFingerprint == before.ConfigurationFingerprint || store.writes != 1 || len(f.calls) != 1 || f.calls[0] != "workspace.list" {
		t.Fatal(after, err, store.writes, f.calls)
	}
	store.record.RuntimeProviderConfig = store.config
	scope := Scope{BotID: "bot-a", TargetID: "mount-a", UserID: "user"}
	if _, err = s.ResolveTarget(PinRegistration(t.Context(), scope, before.TargetFingerprint), scope); err != nil {
		t.Fatal("old task pin no longer resolves", err)
	}
	if _, err = s.ConfigureEndpoint(t.Context(), "owner", "existing", ChangeEndpointConfiguration{ExpectedConfigurationFingerprint: before.ConfigurationFingerprint, Locator: &locator}); !errors.Is(err, ErrEndpointConfigConflict) || len(f.calls) != 1 {
		t.Fatal("stale configuration reached native or overwrote current data", err)
	}
	profile := AgentProfile{Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\Pi`}
	if _, err = s.ConfigureAgent(t.Context(), "owner", "existing", profile); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadEndpointConfiguration(t.Context(), "owner", "existing")
	if err != nil || got.Locator == nil || *got.Locator != locator || !strings.Contains(string(store.config), `"future_field"`) {
		t.Fatal("profile change dropped independent owner configuration", got, err)
	}
	if _, err = s.ConfigureEndpoint(t.Context(), "owner", "existing", ChangeEndpointConfiguration{ExpectedConfigurationFingerprint: got.ConfigurationFingerprint, Locator: &EndpointLocator{}}); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.ReadEndpointConfiguration(t.Context(), "owner", "existing")
	if err != nil || cleared.Locator != nil || strings.Contains(string(store.config), endpointConfigKey) || f.runs != 0 || store.created != 0 {
		t.Fatal("clear dispatched or failed", cleared, err)
	}
}

func TestEndpointLocatorRejectsAmbiguousPathsAndMissingIntent(t *testing.T) {
	for _, value := range []string{"", "relative", `C:relative`, `\drive-relative`, `C:\Host\..\target`, `C:\Host\.\file`, `C:\Host\file:stream`, `C:\Host\bad"name`, "C:\\Host\\bad\nname", `C:\`, `\\?\C:\Host`, strings.Repeat("x", 4097)} {
		t.Run(value, func(t *testing.T) {
			s, store, f := endpointFixture(t)
			before, _ := s.ReadEndpointConfiguration(t.Context(), "owner", "existing")
			for _, locator := range []*EndpointLocator{nil, {ProgramPath: value, HostRoot: `C:\Host`}, {ProgramPath: `C:\Endpoint\endpoint.mjs`, HostRoot: value}} {
				if _, err := s.ConfigureEndpoint(t.Context(), "owner", "existing", ChangeEndpointConfiguration{ExpectedConfigurationFingerprint: before.ConfigurationFingerprint, Locator: locator}); !errors.Is(err, ErrInvalidTarget) {
					t.Fatal("invalid path accepted", err)
				}
			}
			if store.writes != 0 || len(f.calls) != 0 {
				t.Fatal("invalid input wrote or contacted native")
			}
		})
	}
	for _, value := range []string{`C:\Host with spaces\endpoint.mjs`, `D:/端点/host`, `\\synthetic-server\share\host`} {
		if err := (EndpointLocator{ProgramPath: value, HostRoot: value}).validate(); err != nil {
			t.Fatal("explicit Windows path rejected", err)
		}
	}
}

func TestEndpointConfigurationDeniesForeignOrRevokedSource(t *testing.T) {
	for _, failure := range []string{"owner", "runtime", "connection"} {
		t.Run(failure, func(t *testing.T) {
			s, store, f := endpointFixture(t)
			owner, id := "owner", "existing"
			switch failure {
			case "owner":
				owner = "another-owner"
			case "runtime":
				id = "another-registration"
			case "connection":
				store.sourceAllowed = false
			}
			if _, err := s.ReadEndpointConfiguration(t.Context(), owner, id); !errors.Is(err, ErrForbidden) || len(f.calls) != 0 || store.writes != 0 {
				t.Fatal(err)
			}
		})
	}
}

type uncertainEndpointStore struct {
	*agentProfileStore
	failBefore bool
}

func (s *uncertainEndpointStore) ConfigureAgenvoRuntime(ctx context.Context, owner, id string, previous, next dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	if s.failBefore {
		return dbstore.UserRuntimeRecord{}, db.ErrNotFound
	}
	_, err := s.agentProfileStore.ConfigureAgenvoRuntime(ctx, owner, id, previous, next)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	return dbstore.UserRuntimeRecord{}, ErrUnknown
}

func TestEndpointUnknownResponseReconcilesWithoutNativeExecution(t *testing.T) {
	s, store, f := endpointFixture(t)
	before, _ := s.ReadEndpointConfiguration(t.Context(), "owner", "existing")
	s.store = &uncertainEndpointStore{agentProfileStore: store}
	locator := EndpointLocator{ProgramPath: `C:\Endpoint\endpoint.mjs`, HostRoot: `C:\Host data`}
	in := ChangeEndpointConfiguration{ExpectedConfigurationFingerprint: before.ConfigurationFingerprint, Locator: &locator}
	if _, err := s.ConfigureEndpoint(t.Context(), "owner", "existing", in); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	got, err := s.ReadEndpointConfiguration(t.Context(), "owner", "existing")
	if err != nil || got.Locator == nil || *got.Locator != locator || got.TargetFingerprint != before.TargetFingerprint || store.writes != 1 || len(f.calls) != 1 || f.runs != 0 {
		t.Fatal("unknown response caused replay or identity loss", got, err)
	}
	s.store = &uncertainEndpointStore{agentProfileStore: store, failBefore: true}
	in.ExpectedConfigurationFingerprint = got.ConfigurationFingerprint
	if _, err = s.ConfigureEndpoint(t.Context(), "owner", "existing", in); !errors.Is(err, ErrEndpointConfigConflict) || store.writes != 1 {
		t.Fatal("lost CAS did not fail closed", err)
	}
}

func TestEndpointFingerprintCanonicalJSONAndMalformedStoredLocator(t *testing.T) {
	_, store, _ := endpointFixture(t)
	raw, _ := replaceRegistrationField(store.config, "number", json.Number("9007199254740993"))
	a, err := endpointConfiguration(raw)
	if err != nil {
		t.Fatal(err)
	}
	var pretty bytes.Buffer
	if err = json.Indent(&pretty, raw, "", "  "); err != nil {
		t.Fatal(err)
	}
	b, err := endpointConfiguration(pretty.Bytes())
	if err != nil || a.ConfigurationFingerprint != b.ConfigurationFingerprint {
		t.Fatal("JSON formatting changed fingerprint", err)
	}
	changed, _ := replaceRegistrationField(raw, "number", json.Number("9007199254740992"))
	c, _ := endpointConfiguration(changed)
	if a.ConfigurationFingerprint == c.ConfigurationFingerprint {
		t.Fatal("large integer precision lost")
	}
	for _, bad := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`{}`), json.RawMessage(`{"program_path":"C:\\Host\\endpoint.mjs","host_root":"C:\\Host","owner_id":"forged"}`)} {
		config, _ := replaceRegistrationField(raw, endpointConfigKey, bad)
		if _, err = endpointConfiguration(config); !errors.Is(err, ErrInvalidTarget) {
			t.Fatal("malformed stored locator trusted", err)
		}
	}
}
