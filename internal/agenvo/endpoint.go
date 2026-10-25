package agenvo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

const endpointConfigKey = "owner_endpoint"

var ErrEndpointConfigConflict = errors.New("owner endpoint configuration changed")

// EndpointLocator is owner configuration, not a shell command, execution target
// or stop permit. Existence, native ownership and launch protocol must still be
// checked through the existing authenticated host control path before use.
type EndpointLocator struct {
	ProgramPath string `json:"program_path"`
	HostRoot    string `json:"host_root"`
}

type EndpointConfiguration struct {
	ConnectionID             string           `json:"connection_id"`
	DeviceID                 string           `json:"device_id"`
	InstanceID               string           `json:"instance_id"`
	TargetFingerprint        string           `json:"target_fingerprint"`
	ConfigurationFingerprint string           `json:"configuration_fingerprint"`
	Locator                  *EndpointLocator `json:"locator,omitempty"`
}

type ChangeEndpointConfiguration struct {
	ExpectedConfigurationFingerprint string `json:"expected_configuration_fingerprint" validate:"required"`
	// A required object sets the locator. An explicit empty object clears it.
	Locator *EndpointLocator `json:"locator" validate:"required"`
}

func (p EndpointLocator) validate() error {
	if p == (EndpointLocator{}) {
		return nil
	}
	for _, value := range []string{p.ProgramPath, p.HostRoot} {
		_, parts, valid := absoluteWindowsParts(value)
		if !valid || len(parts) == 0 || strings.TrimSpace(value) != value {
			return ErrInvalidTarget
		}
		// Do not silently clean an owner-supplied locator or derive it from cwd.
		for _, part := range strings.Split(strings.ReplaceAll(value, "/", `\`), `\`) {
			if part == "." || part == ".." {
				return ErrInvalidTarget
			}
		}
	}
	return nil
}

// Reuse the registration JSON document but keep control location outside Target:
// adding/updating it must not invalidate existing operation target fingerprints.
func endpointConfiguration(raw dbstore.JSON) (EndpointConfiguration, error) {
	target, err := DecodeTarget(raw)
	if err != nil {
		return EndpointConfiguration{}, err
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(raw, &config) != nil {
		return EndpointConfiguration{}, ErrInvalidTarget
	}
	var locator *EndpointLocator
	if value, exists := config[endpointConfigKey]; exists {
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&locator) != nil || locator == nil || *locator == (EndpointLocator{}) || locator.validate() != nil {
			return EndpointConfiguration{}, ErrInvalidTarget
		}
	}
	// UseNumber preserves JSON integers above 2^53; map marshaling sorts keys at
	// every level so PostgreSQL jsonb formatting/order does not change the CAS ID.
	var canonical map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&canonical) != nil {
		return EndpointConfiguration{}, ErrInvalidTarget
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return EndpointConfiguration{}, ErrInvalidTarget
	}
	sum := sha256.Sum256(encoded)
	return EndpointConfiguration{ConnectionID: target.ConnectionID, DeviceID: target.DeviceID, InstanceID: target.InstanceID, TargetFingerprint: TargetFingerprint(target), ConfigurationFingerprint: hex.EncodeToString(sum[:]), Locator: locator}, nil
}

func replaceRegistrationField(raw dbstore.JSON, key string, value any) (dbstore.JSON, error) {
	var config map[string]json.RawMessage
	if json.Unmarshal(raw, &config) != nil || config == nil {
		return nil, ErrInvalidTarget
	}
	if value == nil {
		delete(config, key)
	} else {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		config[key] = encoded
	}
	return json.Marshal(config)
}

func (s *Service) ReadEndpointConfiguration(ctx context.Context, owner, id string) (EndpointConfiguration, error) {
	_, row, err := s.endpointRecord(ctx, owner, id)
	if err != nil {
		return EndpointConfiguration{}, err
	}
	return endpointConfiguration(row.ProviderConfig)
}

func (s *Service) ConfigureEndpoint(ctx context.Context, owner, id string, in ChangeEndpointConfiguration) (EndpointConfiguration, error) {
	hash, err := hex.DecodeString(in.ExpectedConfigurationFingerprint)
	if err != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != in.ExpectedConfigurationFingerprint || in.Locator == nil || in.Locator.validate() != nil {
		return EndpointConfiguration{}, ErrInvalidTarget
	}
	store, row, err := s.endpointRecord(ctx, owner, id)
	if err != nil {
		return EndpointConfiguration{}, err
	}
	current, err := endpointConfiguration(row.ProviderConfig)
	if err != nil {
		return EndpointConfiguration{}, err
	}
	if current.ConfigurationFingerprint != in.ExpectedConfigurationFingerprint {
		return EndpointConfiguration{}, ErrEndpointConfigConflict
	}
	var value any
	if *in.Locator != (EndpointLocator{}) {
		value = *in.Locator
	}
	next, err := replaceRegistrationField(row.ProviderConfig, endpointConfigKey, value)
	if err != nil {
		return EndpointConfiguration{}, err
	}
	// Only the established native read-only identity probe is used. It does not
	// attest the program/root or another connection/owner's physical host scope.
	if err = s.Probe(ctx, owner, row.ProviderConfig); err != nil {
		return EndpointConfiguration{}, err
	}
	row, err = store.ConfigureAgenvoRuntime(ctx, owner, id, row.ProviderConfig, next)
	if errors.Is(err, db.ErrNotFound) {
		return EndpointConfiguration{}, ErrEndpointConfigConflict
	}
	if err != nil {
		return EndpointConfiguration{}, err
	}
	return endpointConfiguration(row.ProviderConfig)
}

func (s *Service) endpointRecord(ctx context.Context, owner, id string) (profileStore, dbstore.UserRuntimeRecord, error) {
	if s == nil || s.store == nil {
		return nil, dbstore.UserRuntimeRecord{}, ErrUnavailable
	}
	store, ok := s.store.(profileStore)
	if !ok {
		return nil, dbstore.UserRuntimeRecord{}, ErrUnavailable
	}
	row, err := store.OwnedAgenvoRuntime(ctx, owner, id)
	if err != nil {
		return nil, row, ErrForbidden
	}
	target, err := DecodeTarget(row.ProviderConfig)
	if err != nil {
		return nil, row, err
	}
	if _, err = s.store.OwnedHTTPMCPConnectionBot(ctx, owner, target.ConnectionID); err != nil {
		return nil, row, ErrForbidden
	}
	return store, row, nil
}
