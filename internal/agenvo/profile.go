package agenvo

import (
	"context"

	dbstore "github.com/felinics/memoh/internal/db/store"
)

type profileStore interface {
	OwnedAgenvoRuntime(context.Context, string, string) (dbstore.UserRuntimeRecord, error)
	ConfigureAgenvoRuntime(context.Context, string, string, dbstore.JSON, dbstore.JSON) (dbstore.UserRuntimeRecord, error)
}

// ConfigureAgent updates an existing owner registration, not its credentials,
// grants or device mapping. Zero profile disables Agent dispatch.
func (s *Service) ConfigureAgent(ctx context.Context, owner, id string, profile AgentProfile) (dbstore.UserRuntimeRecord, error) {
	store, ok := s.store.(profileStore)
	if !ok {
		return dbstore.UserRuntimeRecord{}, ErrUnavailable
	}
	if profile != (AgentProfile{}) && profile.Validate() != nil {
		return dbstore.UserRuntimeRecord{}, ErrInvalidTarget
	}
	row, err := store.OwnedAgenvoRuntime(ctx, owner, id)
	if err != nil {
		return row, ErrForbidden
	}
	target, err := DecodeTarget(row.ProviderConfig)
	if err != nil {
		return row, err
	}
	if _, err = s.store.OwnedHTTPMCPConnectionBot(ctx, owner, target.ConnectionID); err != nil {
		return dbstore.UserRuntimeRecord{}, ErrForbidden
	}
	var value any
	if profile != (AgentProfile{}) {
		value = profile
	}
	raw, err := replaceRegistrationField(row.ProviderConfig, "agent", value)
	if err != nil {
		return row, err
	}
	return store.ConfigureAgenvoRuntime(ctx, owner, id, row.ProviderConfig, raw)
}
