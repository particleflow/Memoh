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
	OwnedAgenvoRuntime(context.Context, string, string) (dbstore.UserRuntimeRecord, error)
}

// Admission reports only new-input admission for the registered native identity.
// Paused is not a drain result, process exit proof or authorization to stop a host.
type Admission struct {
	ConnectionID string `json:"connection_id"`
	DeviceID     string `json:"device_id"`
	InstanceID   string `json:"instance_id"`
	RequestID    string `json:"request_id,omitempty"`
	Revision     string `json:"revision"`
	Paused       bool   `json:"paused"`
	Scope        string `json:"scope"`
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
	row, err := store.OwnedAgenvoRuntime(ctx, owner, runtime)
	if errors.Is(err, db.ErrNotFound) {
		if native, ok := s.store.(nativeProfileStore); ok {
			row, err = native.OwnedNativeHerdrRuntime(ctx, owner, runtime)
		}
	}
	if err != nil {
		return nil, row, Target{}, ErrForbidden
	}
	var target Target
	if row.Provider == "memoh" {
		target, err = DecodeNativeTarget(row.ID, row.ProviderConfig)
	} else {
		target, err = DecodeTarget(row.ProviderConfig)
	}
	if err != nil {
		return nil, row, target, db.ErrComputerAdmissionScope
	}
	if target.NativeRuntimeID == "" {
		if _, err = s.store.OwnedHTTPMCPConnectionBot(ctx, owner, target.ConnectionID); err != nil {
			return nil, row, target, ErrForbidden
		}
	}
	return store, row, target, nil
}

// ReadAdmission performs no native call and never creates or resumes a gate.
func (s *Service) ReadAdmission(ctx context.Context, owner, runtime string) (Admission, error) {
	store, _, target, err := s.admissionTarget(ctx, owner, runtime)
	if err != nil {
		return Admission{}, err
	}
	row, err := store.ReadComputerAdmission(ctx, owner, target.ConnectionID, target.DeviceID, target.InstanceID)
	if errors.Is(err, db.ErrNotFound) {
		row = dbstore.ComputerAdmission{ConnectionID: target.ConnectionID, DeviceID: target.DeviceID, InstanceID: target.InstanceID}
		err = nil
	}
	return admissionResponse(row), err
}

// SetAdmission uses the existing owner's MCP authorization and native read-only
// probe. The database compares the same registration again under the admission
// lock; request ID and revision must be retained across an uncertain response.
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
	if record.Provider != "memoh" {
		if err = s.Probe(ctx, owner, record.ProviderConfig); err != nil {
			return Admission{}, err
		}
	}
	row, err := store.ChangeComputerAdmission(ctx, owner, runtime, record.ProviderConfig, in.RequestID, revision, *in.Paused)
	return admissionResponse(row), err
}
