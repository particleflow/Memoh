package computertask

import (
	"context"
	"errors"
	"time"

	"github.com/felinics/memoh/internal/toolcontext"
)

const TransferAudience = "memoh:file-transfer:v1"

var ErrTransferCreateUnknown = errors.New("transfer upload creation requires reconciliation")

// VerifiedTransfer is a short-lived internal request authorization context, not
// a device credential or a requirement to issue a new token for every file.
// CredentialID is an opaque revocation selector, never the credential itself.
type VerifiedTransfer struct {
	Device       VerifiedTransferDevice
	Identity     DeliveryIdentity
	DeliveryID   string
	Reference    DeliveryRequest
	Audience     string
	CredentialID string
	IssuedAt     time.Time
	ExpiresAt    time.Time
}

// VerifiedTransferDevice is returned only by the configured device authenticator.
// Its credential can serve multiple deliveries. Expiry and revocation are owned
// by that authenticator; the request context's 15 minute limit is independent.
type VerifiedTransferDevice struct {
	CredentialID, ConnectionID, DeviceID, InstanceID string
}

func (d VerifiedTransferDevice) Valid() bool {
	return deliveryIDPattern.MatchString(d.CredentialID) && deliveryIDPattern.MatchString(d.ConnectionID) &&
		d.DeviceID != "" && len(d.DeviceID) <= 256 && d.InstanceID != "" && len(d.InstanceID) <= 256
}

type transferPrincipalKey struct{}

func (p VerifiedTransfer) Valid(now time.Time) bool {
	return (p.Device == (VerifiedTransferDevice{}) || p.Device.Valid() && p.Device.CredentialID == p.CredentialID) &&
		p.Audience == TransferAudience && deliveryIDPattern.MatchString(p.CredentialID) &&
		deliveryIDPattern.MatchString(p.DeliveryID) && p.Reference.valid() && p.Reference.SizeBytes > 0 &&
		!p.IssuedAt.IsZero() && !p.IssuedAt.After(now) && p.ExpiresAt.After(now) &&
		p.ExpiresAt.After(p.IssuedAt) && p.ExpiresAt.Sub(p.IssuedAt) <= 15*time.Minute
}

// BindVerifiedTransfer is for trusted HTTP/host adapters only, after verification.
// No token parser, issuer, account-JWT fallback or automatic renewal lives here.
func BindVerifiedTransfer(ctx context.Context, p VerifiedTransfer) (context.Context, error) {
	if ctx == nil || ctx.Err() != nil || !p.Valid(time.Now()) {
		return ctx, ErrDeliveryBinding
	}
	if old, ok := VerifiedTransferFromContext(ctx); ok && old != p {
		return ctx, ErrDeliveryBinding
	}
	ctx, err := toolcontext.WithComputerBinding(ctx, p.Identity.Binding)
	if err != nil {
		return ctx, ErrDeliveryBinding
	}
	ctx, err = WithDeliveryIdentity(ctx, p.Identity)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, transferPrincipalKey{}, p), nil
}

func VerifiedTransferFromContext(ctx context.Context) (VerifiedTransfer, bool) {
	if ctx == nil {
		return VerifiedTransfer{}, false
	}
	p, ok := ctx.Value(transferPrincipalKey{}).(VerifiedTransfer)
	return p, ok
}

type transferAccess struct {
	UploadID string `json:"upload_id,omitempty"`
	Creating bool   `json:"creating,omitempty"`
}

// ReserveTransfer binds the file grant using the existing operation revision
// CAS. Legacy entries cannot silently acquire an incoming PC grant. Existing
// execution writers preserve this ledger; pre-v3 delivery writers fail closed.
func (s *DeliveryService) ReserveTransfer(ctx context.Context) error {
	return s.transferMutation(ctx, func(e *deliveryEntry, exists bool) (bool, error) {
		if exists {
			if e.Transfer == nil {
				return false, ErrDeliveryVersion
			}
			return false, nil
		}
		e.Outcome, e.Transfer = "reserved", &transferAccess{}
		return true, nil
	})
}

// CheckTransfer takes a fresh database authorization snapshot. Revocation after
// that snapshot cannot retract bytes already sent or an OS operation in flight.
func (s *DeliveryService) CheckTransfer(ctx context.Context, requireOpen bool) error {
	return s.transferMutation(ctx, func(e *deliveryEntry, exists bool) (bool, error) {
		if !exists || e.Transfer == nil || requireOpen && (e.Outcome == "canceled" || e.Outcome == "failed") {
			return false, ErrDeliveryBinding
		}
		return false, nil
	})
}

func (s *DeliveryService) TransferUploadID(ctx context.Context) (string, error) {
	var id string
	err := s.transferMutation(ctx, func(e *deliveryEntry, exists bool) (bool, error) {
		if !exists || e.Transfer == nil || e.Reference.Direction != "upload" {
			return false, ErrDeliveryBinding
		}
		id = e.Transfer.UploadID
		return false, nil
	})
	return id, err
}

// ClaimTransferUpload persists an unknown-create fence before Media.Create.
// The account remains the UploadStore owner, preserving its per-owner quotas.
func (s *DeliveryService) ClaimTransferUpload(ctx context.Context) (string, error) {
	var id string
	err := s.transferMutation(ctx, func(e *deliveryEntry, exists bool) (bool, error) {
		if !exists || e.Transfer == nil || e.Reference.Direction != "upload" || e.Outcome == "canceled" || e.Outcome == "failed" {
			return false, ErrDeliveryBinding
		}
		id = e.Transfer.UploadID
		if id != "" {
			return false, nil
		}
		if e.Transfer.Creating {
			return false, ErrTransferCreateUnknown
		}
		e.Transfer.Creating = true
		return true, nil
	})
	return id, err
}

// FinishTransferUpload attaches the receipt after Media durably creates it.
// Empty id is allowed only by the adapter after a definite pre-creation refusal.
// Unknown storage/DB outcomes retain the fence; never guess or create again.
func (s *DeliveryService) FinishTransferUpload(ctx context.Context, id string) error {
	if id != "" && !deliveryIDPattern.MatchString(id) {
		return ErrInvalid
	}
	return s.transferMutation(ctx, func(e *deliveryEntry, exists bool) (bool, error) {
		if !exists || e.Transfer == nil || e.Reference.Direction != "upload" {
			return false, ErrDeliveryBinding
		}
		if e.Transfer.UploadID != "" {
			if e.Transfer.UploadID != id {
				return false, ErrDeliveryConflict
			}
			return false, nil
		}
		if !e.Transfer.Creating {
			return false, ErrDeliveryConflict
		}
		e.Transfer.UploadID, e.Transfer.Creating = id, false
		return true, nil
	})
}

func (s *DeliveryService) transferMutation(ctx context.Context, mutate func(*deliveryEntry, bool) (bool, error)) error {
	p, ok := VerifiedTransferFromContext(ctx)
	if !ok || !p.Valid(time.Now()) || s == nil || s.transactions == nil {
		return ErrDeliveryBinding
	}
	for range 3 {
		err := s.transactions(ctx, func(tx DeliveryTransaction) error {
			_, err := mutateDeliveryTransaction(ctx, tx, p.Identity, p.DeliveryID, p.Reference, 3, mutate)
			return err
		})
		if !errors.Is(err, ErrDeliveryRetry) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return ErrDeliveryRetry
}
