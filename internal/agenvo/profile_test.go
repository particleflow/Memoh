package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	dbstore "github.com/felinics/memoh/internal/db/store"
)

type agentProfileStore struct {
	*serviceStore
	config dbstore.JSON
	writes int
}

func (s *agentProfileStore) OwnedAgenvoRuntime(_ context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	if owner != "owner" || id != "existing" {
		return dbstore.UserRuntimeRecord{}, ErrForbidden
	}
	return dbstore.UserRuntimeRecord{ID: id, UserID: owner, ProviderConfig: s.config}, nil
}

func (s *agentProfileStore) ConfigureAgenvoRuntime(_ context.Context, owner, id string, previous, next dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	if string(previous) != string(s.config) || owner != "owner" || id != "existing" {
		return dbstore.UserRuntimeRecord{}, ErrForbidden
	}
	s.writes++
	s.config = next
	return dbstore.UserRuntimeRecord{ID: id, UserID: owner, ProviderConfig: next}, nil
}

func TestAgentProfileUpdatesExistingOwnerRegistrationWithoutCredentialsOrGrants(t *testing.T) {
	s, base, f := serviceFixture(t)
	store := &agentProfileStore{serviceStore: base, config: base.record.RuntimeProviderConfig}
	s.store = store
	p := AgentProfile{Provider: "synthetic-magpie", Model: "synthetic-model", ConfigDir: `C:\Synthetic\Pi`}
	if _, err := s.ConfigureAgent(t.Context(), "other-owner", "existing", p); !errors.Is(err, ErrForbidden) {
		t.Fatal("foreign owner updated profile")
	}
	bad := p
	bad.ConfigDir = "relative"
	if _, err := s.ConfigureAgent(t.Context(), "owner", "existing", bad); !errors.Is(err, ErrInvalidTarget) {
		t.Fatal("ambiguous config directory accepted")
	}
	row, err := s.ConfigureAgent(t.Context(), "owner", "existing", p)
	if err != nil {
		t.Fatal(err)
	}
	var got Target
	if json.Unmarshal(row.ProviderConfig, &got) != nil {
		t.Fatal("invalid stored target")
	}
	want := f.target
	want.Agent = p
	if got != want || store.writes != 1 || base.created != 0 || len(f.calls) != 0 {
		t.Fatal("configuration altered mapping, created registration or dispatched remotely")
	}
	row, err = s.ConfigureAgent(t.Context(), "owner", "existing", AgentProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(row.ProviderConfig, &got) != nil {
		t.Fatal("invalid disabled profile")
	}
	// Decode into a fresh object because omitted zero fields do not clear Go destinations.
	disabled, err := DecodeTarget(row.ProviderConfig)
	if err != nil || disabled.Agent != (AgentProfile{}) {
		t.Fatal("profile did not disable")
	}
}
