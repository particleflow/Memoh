package computertask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

// SetResultAssociator connects verified producer results to the existing source
// conversation. The hook receives ledger facts, never model-supplied hashes.
func (s *Source) SetResultAssociator(link func(context.Context, dbstore.ComputerOperationRecord, []DeliveryRequest) error) {
	s.associate = link
}

func committedResultReferences(r dbstore.ComputerOperationRecord, d Details) []DeliveryRequest {
	if r.State != "completed" || r.CancelRequested || d.Batch == nil || d.Batch.Result == nil || d.Batch.Result.Outcome != "succeeded" {
		return nil
	}
	result := producerResult(r, d)
	if result == nil || (result.State != "delivered" && result.State != "partial") {
		return nil
	}
	refs := []DeliveryRequest{}
	if result.Reference != nil {
		refs = append(refs, *result.Reference)
	}
	for _, file := range result.Files {
		if file.State != "delivered" || file.Reference == nil {
			continue
		}
		refs = append(refs, *file.Reference)
	}
	return refs
}

func (s *Source) associateResult(ctx context.Context, b toolcontext.ComputerBinding, id string) error {
	if s.associate == nil {
		return nil
	}
	r, d, err := s.service.loadBoundHistory(ctx, b, id)
	if err != nil {
		return ErrDenied
	}
	refs := committedResultReferences(r, d)
	if len(refs) == 0 {
		return nil
	}
	if s.service.assets == nil {
		return ErrDenied
	}
	for _, ref := range refs {
		if !ref.valid() || ref.Direction != "upload" {
			return ErrDenied
		}
		reader, asset, err := s.service.assets.Open(ctx, r.BotID, ref.ContentHash)
		if err != nil {
			return ErrDenied
		}
		if reader == nil {
			return ErrDenied
		}
		hash := sha256.New()
		n, readErr := io.Copy(hash, reader)
		closeErr := reader.Close()
		if readErr != nil {
			return ErrDenied
		}
		if closeErr != nil {
			return ErrDenied
		}
		// MIME is declared ledger metadata; media lookup can only derive a MIME
		// from its storage extension. Integrity comes from the full hash and length.
		if asset.BotID != r.BotID || asset.ContentHash != ref.ContentHash || (asset.SizeBytes != 0 && asset.SizeBytes != ref.SizeBytes) || n != ref.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != ref.ContentHash {
			return ErrDenied
		}
	}
	// Revocation/cancellation/ledger mutation during a full read must stop linking.
	current, _, err := s.service.loadBoundHistory(ctx, b, id)
	if err != nil {
		return ErrDenied
	}
	if current.Revision != r.Revision || current.State != r.State || current.CancelRequested != r.CancelRequested || current.RunID != r.RunID {
		return ErrDenied
	}
	return s.associate(ctx, r, refs)
}
