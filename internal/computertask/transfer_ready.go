package computertask

import (
	"context"

	"github.com/felinics/memoh/internal/media"
)

// The durable ledger format is independent of the unchanged v3 HTTP outcome.
// Older ledger writers must reject this format rather than discard ready proof.
const transferLedgerVersion = 4

func validTransferReady(receipt media.Upload, ref DeliveryRequest, uploadID string) bool {
	return ref.Direction == "upload" && receipt.ID == uploadID && uploadID != "" &&
		receipt.State == "ready" && receipt.Offset == ref.SizeBytes && receipt.SizeBytes == ref.SizeBytes &&
		receipt.ContentHash == ref.ContentHash && receipt.Mime == ref.Mime && !receipt.ExpiresAt.IsZero()
}

func sameTransferReady(a, b media.Upload) bool {
	return a.ID == b.ID && a.State == b.State && a.Offset == b.Offset && a.SizeBytes == b.SizeBytes &&
		a.ContentHash == b.ContentHash && a.Mime == b.Mime && a.ExpiresAt.Equal(b.ExpiresAt)
}

// RecordTransferReady accepts only a receipt obtained from the server's own
// UploadStore by a trusted adapter. It is not a public receipt assertion API.
// Authorization, exact task/ref binding and the operation CAS share a snapshot.
// Unknown commit outcomes must be reconciled with TransferReady, not recreated.
func (s *DeliveryService) RecordTransferReady(ctx context.Context, receipt media.Upload) error {
	return s.transferMutation(ctx, func(e *deliveryEntry, exists bool) (bool, error) {
		if !exists || e.Transfer == nil || !validTransferReady(receipt, e.Reference, e.Transfer.UploadID) {
			return false, ErrDeliveryBinding
		}
		if e.Transfer.Ready != nil {
			if !sameTransferReady(*e.Transfer.Ready, receipt) {
				return false, ErrDeliveryConflict
			}
			return false, nil
		}
		if e.Outcome == "canceled" || e.Outcome == "failed" || e.Transfer.Creating {
			return false, ErrDeliveryBinding
		}
		e.Transfer.Ready = &receipt
		return true, nil
	})
}

// TransferReady returns the original proof under fresh request authorization.
// Absent proof is not proof that a file transfer or commit never happened.
func (s *DeliveryService) TransferReady(ctx context.Context) (media.Upload, bool, error) {
	var receipt media.Upload
	var found bool
	err := s.transferMutation(ctx, func(e *deliveryEntry, exists bool) (bool, error) {
		if !exists || e.Transfer == nil || e.Reference.Direction != "upload" {
			return false, ErrDeliveryBinding
		}
		if e.Transfer.Ready != nil {
			receipt, found = *e.Transfer.Ready, true
		}
		return false, nil
	})
	if err != nil {
		return media.Upload{}, false, err
	}
	return receipt, found, nil
}
