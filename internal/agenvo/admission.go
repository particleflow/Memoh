package agenvo

import (
	"context"
	"errors"
	"strconv"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type admissionStore interface {
	dbstore.ComputerAdmissionStore
	OwnedNativeHerdrRuntime(context.Context, string, string) (dbstore.UserRuntimeRecord, error)
}

// Admission reports only new-input admission for the registered native identity.
// Paused is not a drain result, process exit proof or authorization to stop a host.
type Admission struct {
	ConnectionID      string `json:"connection_id"`
	DeviceID          string `json:"device_id"`
	InstanceID        string `json:"instance_id"`
	RequestID         string `json:"request_id,omitempty"`
	Revision          string `json:"revision"`
	Paused            bool   `json:"paused"`
	Scope             string `json:"scope"`
	ConfigSHA256      string `json:"config_sha256,omitempty"`
	TargetFingerprint string `json:"target_fingerprint,omitempty"`
	AgentConfigDir    string `json:"agent_config_dir,omitempty"`
}

type ChangeAdmission struct {
	RequestID        string `json:"request_id"`
	ExpectedRevision string `json:"expected_revision"`
	Paused           *bool  `json:"paused"`
}

func admissionResponse(row dbstore.ComputerAdmission) Admission {
	return Admission{ConnectionID: row.ConnectionID, DeviceID: row.DeviceID, InstanceID: row.InstanceID, RequestID: row.RequestID, Revision: strconv.FormatInt(row.Revision, 10), Paused: row.Paused, Scope: "owner_connection_instance"}
}

func (s *Service) admissionTarget(ctx context.Context, owner, runtime string) (admissionStore, dbstore.UserRuntimeRecord, Target, error) {
	if s == nil || s.store == nil {
		return nil, dbstore.UserRuntimeRecord{}, Target{}, ErrUnavailable
	}
	store, ok := s.store.(admissionStore)
	if !ok {
		return nil, dbstore.UserRuntimeRecord{}, Target{}, ErrUnavailable
	}
	row, err := store.OwnedNativeHerdrRuntime(ctx, owner, runtime)
	if err != nil {
		return nil, row, Target{}, ErrForbidden
	}
	if row.Provider != "memoh" {
		return nil, row, Target{}, ErrLegacyRetired
	}
	target, err := DecodeNativeTarget(row.ID, row.ProviderConfig)
	if err != nil {
		return nil, row, target, db.ErrComputerAdmissionScope
	}

	return store, row, target, nil
}

// ReadAdmission performs no native call and never creates or resumes a gate.
func (s *Service) ReadAdmission(ctx context.Context, owner, runtime string) (Admission, error) {
	if s == nil || s.store == nil {
		return Admission{}, ErrUnavailable
	}
	store, ok := s.store.(dbstore.NativeAdmissionSnapshotStore)
	if !ok {
		return Admission{}, ErrUnavailable
	}
	snapshot, err := store.ReadNativeAdmissionSnapshot(ctx, owner, runtime)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return Admission{}, ErrForbidden
		}
		return Admission{}, err
	}
	if snapshot.RuntimeID != runtime {
		return Admission{}, ErrForbidden
	}
	if snapshot.Provider != "memoh" {
		return Admission{}, ErrLegacyRetired
	}
	target, err := DecodeNativeTarget(snapshot.RuntimeID, snapshot.ProviderConfig)
	if err != nil {
		return Admission{}, db.ErrComputerAdmissionScope
	}
	gate := dbstore.ComputerAdmission{ConnectionID: target.ConnectionID, DeviceID: target.DeviceID, InstanceID: target.InstanceID}
	if snapshot.Gate != nil {
		gate = *snapshot.Gate
		if gate.ConnectionID != target.ConnectionID || gate.DeviceID != target.DeviceID || gate.InstanceID != target.InstanceID {
			return Admission{}, db.ErrComputerAdmissionScope
		}
	}
	digest, err := NativeProfileConfigDigest(snapshot.ProviderConfig)
	if err != nil {
		return Admission{}, db.ErrComputerAdmissionScope
	}
	out := admissionResponse(gate)
	out.ConfigSHA256, out.TargetFingerprint, out.AgentConfigDir = digest, TargetFingerprint(target), target.Agent.ConfigDir
	return out, nil
}

// SetAdmission checks the existing owned native registration. The database
// compares that registration under the admission lock; retain request ID and
// revision across an uncertain response. No remote call is required.
func (s *Service) SetAdmission(ctx context.Context, owner, runtime string, in ChangeAdmission) (Admission, error) {
	request, err := uuid.Parse(in.RequestID)
	revision, revErr := strconv.ParseInt(in.ExpectedRevision, 10, 64)
	if err != nil || request == uuid.Nil || request.String() != in.RequestID || revErr != nil || revision < 0 || strconv.FormatInt(revision, 10) != in.ExpectedRevision || in.Paused == nil {
		return Admission{}, ErrInvalidTarget
	}
	store, record, _, err := s.admissionTarget(ctx, owner, runtime)
	if err != nil {
		return Admission{}, err
	}
	// Native admission is an owner control-plane change and remains available
	// while the PC is disconnected. It does not grant execution or resume work.
	row, err := store.ChangeComputerAdmission(ctx, owner, runtime, record.ProviderConfig, in.RequestID, revision, *in.Paused)
	return admissionResponse(row), err
}
