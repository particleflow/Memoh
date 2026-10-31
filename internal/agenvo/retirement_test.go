package agenvo

import (
	"errors"
	"testing"
)

func TestNativeOnlyWiringRejectsLegacyBeforeAnyTransport(t *testing.T) {
	legacy, store, f := serviceFixture(t)
	store.record.RuntimeProvider = Provider
	s := NewService(store, legacy.permissions, f.call) // Same wiring as core and delivery.
	original := string(store.record.RuntimeProviderConfig)
	if _, err := s.ResolveTarget(t.Context(), request().Scope); !errors.Is(err, ErrLegacyRetired) {
		t.Fatal(err)
	}
	out, err := s.Execute(t.Context(), request())
	if !errors.Is(err, ErrLegacyRetired) || out.State != "not_started" {
		t.Fatalf("%+v %v", out, err)
	}
	if len(f.calls) != 0 || string(store.record.RuntimeProviderConfig) != original {
		t.Fatal("retired target caused a call, write, or configuration change")
	}
}

func TestSharedErrorsDoNotMisidentifyNativeAsLegacy(t *testing.T) {
	if ErrUnsupported.Error() != "operation not supported by computer target" {
		t.Fatal(ErrUnsupported)
	}
}
