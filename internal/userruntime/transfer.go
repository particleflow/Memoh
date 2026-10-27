package userruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

var (
	ErrTransferUnavailable = errors.New("device transfers disabled")
	ErrTransferCredential  = errors.New("device credential rejected")
	ErrTransferConflict    = errors.New("device credential changed; read current status")
)

// TransferCredentials is a store-backed verifier and owner management component.
// It has no listener, scheduler or credential cache. Only digests enter storage.
type TransferDevice struct {
	CredentialID, ConnectionID, DeviceID, InstanceID string
}

func (d TransferDevice) Valid() bool {
	_, err := uuid.Parse(d.ConnectionID)
	return err == nil && d.DeviceID != "" && len(d.DeviceID) <= 256 && d.InstanceID != "" && len(d.InstanceID) <= 256
}

type TransferCredentials struct {
	store   dbstore.RuntimeTransferStore
	enabled bool
}

func NewTransferCredentials(store dbstore.RuntimeTransferStore, enabled bool) *TransferCredentials {
	return &TransferCredentials{store: store, enabled: enabled}
}

type TransferCredentialStatus struct {
	CredentialID string     `json:"credential_id"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}
type ChangeTransferCredentialRequest struct {
	// Empty means first enrollment. Rotation must name the current credential.
	ExpectedID string     `json:"expected_id"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}
type TransferCredentialEnrollment struct {
	TransferCredentialStatus
	Credential string `json:"credential"`
}

func (s *TransferCredentials) available() error {
	if s == nil || !s.enabled || s.store == nil {
		return ErrTransferUnavailable
	}
	return nil
}

func (s *TransferCredentials) owned(ctx context.Context, owner, runtime string) (dbstore.RuntimeTransferRecord, error) {
	if err := s.available(); err != nil {
		return dbstore.RuntimeTransferRecord{}, err
	}
	if _, err := uuid.Parse(owner); err != nil {
		return dbstore.RuntimeTransferRecord{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(runtime); err != nil {
		return dbstore.RuntimeTransferRecord{}, ErrInvalidInput
	}
	r, err := s.store.ReadOwnedRuntimeTransfer(ctx, runtime, owner)
	if err != nil {
		return dbstore.RuntimeTransferRecord{}, transferReadError(err)
	}
	return r, nil
}

func transferStatus(r dbstore.RuntimeTransferRecord) TransferCredentialStatus {
	status := TransferCredentialStatus{CredentialID: r.CredentialID}
	if !r.ExpiresAt.IsZero() {
		t := r.ExpiresAt
		status.ExpiresAt = &t
	}
	return status
}

func (s *TransferCredentials) Status(ctx context.Context, owner, runtime string) (TransferCredentialStatus, error) {
	r, err := s.owned(ctx, owner, runtime)
	return transferStatus(r), err
}

// Enroll is called only by the authenticated device owner during initial setup
// or explicit rotation. A lost enrollment response requires read + explicit CAS
// rotation; this method never returns stored plaintext or automatically retries.
func (s *TransferCredentials) Enroll(ctx context.Context, owner, runtime string, req ChangeTransferCredentialRequest) (TransferCredentialEnrollment, error) {
	old, err := s.owned(ctx, owner, runtime)
	if err != nil {
		return TransferCredentialEnrollment{}, err
	}
	if old.CredentialID != req.ExpectedID {
		return TransferCredentialEnrollment{}, ErrTransferConflict
	}
	target, err := transferRuntimeTarget(old)
	if err != nil {
		return TransferCredentialEnrollment{}, ErrTransferCredential
	}
	next := old
	next.ExpiresAt = time.Time{}
	if req.ExpiresAt != nil {
		if !req.ExpiresAt.After(time.Now()) {
			return TransferCredentialEnrollment{}, ErrInvalidInput
		}
		next.ExpiresAt = req.ExpiresAt.UTC()
	}
	next.TargetDigest, err = transferIdentityDigest(target)
	if err != nil {
		return TransferCredentialEnrollment{}, ErrTransferCredential
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return TransferCredentialEnrollment{}, err
	}
	next.CredentialID = uuid.NewString()
	credential := "mft_" + next.CredentialID + "." + hex.EncodeToString(secret)
	next.Digest = transferDigest(credential)
	changed, err := s.store.ChangeRuntimeTransfer(ctx, old, next)
	if err != nil {
		return TransferCredentialEnrollment{}, err
	}
	if !changed {
		return TransferCredentialEnrollment{}, ErrTransferConflict
	}
	return TransferCredentialEnrollment{TransferCredentialStatus: transferStatus(next), Credential: credential}, nil
}

func (s *TransferCredentials) Revoke(ctx context.Context, owner, runtime, expected string) error {
	old, err := s.owned(ctx, owner, runtime)
	if err != nil {
		return err
	}
	if expected == "" || old.CredentialID != expected {
		return ErrTransferConflict
	}
	next := old
	next.CredentialID = ""
	next.Digest = ""
	next.TargetDigest = ""
	next.ExpiresAt = time.Time{}
	changed, err := s.store.ChangeRuntimeTransfer(ctx, old, next)
	if err != nil {
		return err
	}
	if !changed {
		return ErrTransferConflict
	}
	return nil
}

func (s *TransferCredentials) device(ctx context.Context, id string) (TransferDevice, dbstore.RuntimeTransferRecord, error) {
	if err := s.available(); err != nil {
		return TransferDevice{}, dbstore.RuntimeTransferRecord{}, err
	}
	r, err := s.store.ReadRuntimeTransfer(ctx, id)
	if err != nil {
		return TransferDevice{}, r, transferReadError(err)
	}
	if r.CredentialID != id || r.Digest == "" || (!r.ExpiresAt.IsZero() && !r.ExpiresAt.After(time.Now())) {
		return TransferDevice{}, r, ErrTransferCredential
	}
	target, err := transferRuntimeTarget(r)
	if err != nil {
		return TransferDevice{}, r, ErrTransferCredential
	}
	digest, err := transferIdentityDigest(target)
	if err != nil || digest != r.TargetDigest {
		return TransferDevice{}, r, ErrTransferCredential
	}
	d := TransferDevice{CredentialID: id, ConnectionID: target.ConnectionID, DeviceID: target.DeviceID, InstanceID: target.InstanceID}
	if !d.Valid() {
		return TransferDevice{}, r, ErrTransferCredential
	}
	return d, r, nil
}

func (s *TransferCredentials) VerifyDevice(ctx context.Context, credential string) (TransferDevice, error) {
	// Exact purpose/length/canonical format; neither account JWT nor upstream
	// Agenvo/Remote Runtime credentials enter the lookup path.
	if len(credential) != 4+36+1+64 || !strings.HasPrefix(credential, "mft_") {
		return TransferDevice{}, ErrTransferCredential
	}
	id, secret, ok := strings.Cut(credential[4:], ".")
	parsed, err := uuid.Parse(id)
	if !ok || err != nil || parsed.String() != id {
		return TransferDevice{}, ErrTransferCredential
	}
	if raw, err := hex.DecodeString(secret); err != nil || len(raw) != 32 || hex.EncodeToString(raw) != secret {
		return TransferDevice{}, ErrTransferCredential
	}
	d, r, err := s.device(ctx, id)
	if err != nil {
		return TransferDevice{}, err
	}
	if subtle.ConstantTimeCompare([]byte(transferDigest(credential)), []byte(r.Digest)) != 1 {
		return TransferDevice{}, ErrTransferCredential
	}
	return d, nil
}

func (s *TransferCredentials) RevalidateDevice(ctx context.Context, d TransferDevice) error {
	current, _, err := s.device(ctx, d.CredentialID)
	if err != nil {
		return err
	}
	if current != d {
		return ErrTransferCredential
	}
	return nil
}

func transferDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func transferRuntimeTarget(r dbstore.RuntimeTransferRecord) (agenvo.Target, error) {
	if r.Provider == "memoh" {
		return agenvo.DecodeNativeTarget(r.RuntimeID, r.Config)
	}
	return agenvo.Target{}, ErrTransferCredential
}

func transferIdentityDigest(t agenvo.Target) (string, error) {
	raw, err := json.Marshal([3]string{t.ConnectionID, t.DeviceID, t.InstanceID})
	if err != nil {
		return "", err
	}
	return transferDigest(string(raw)), nil
}

func transferReadError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return ErrTransferCredential
	}
	return ErrTransferUnavailable
}
