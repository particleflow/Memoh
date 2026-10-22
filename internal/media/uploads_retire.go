package media

import (
	"context"
	"errors"
	"os"
)

// RetireTaskReceipt is an internal adapter port, not an HTTP proof assertion.
// The caller must obtain this exact ready receipt from a freshly authorized,
// durably committed operation ledger. Ready alone never authorizes retirement.
// All mutation locks have the same order as Create/Sweep. Fixed lock stripes
// are never removed, so old handles cannot form a second lock after retirement.
func (s *UploadStore) RetireTaskReceipt(ctx context.Context, owner, bot string, proof Upload) error {
	if !validPathComponent(owner) || !validPathComponent(bot) || !validUploadID(proof.ID) ||
		proof.State != "ready" || proof.SizeBytes <= 0 || proof.Offset != proof.SizeBytes ||
		!validSHA(proof.ContentHash) || proof.ExpiresAt.IsZero() {
		return ErrUploadInvalid
	}
	admission, err := s.lock(ctx, "admission")
	if err != nil {
		return err
	}
	defer admission()
	unlock, err := s.lock(ctx, proof.ID)
	if err != nil {
		return err
	}
	defer unlock()
	r, err := s.read(proof.ID)
	if errors.Is(err, ErrUploadNotFound) {
		// A previous unlink may have lost its directory-sync acknowledgment.
		// Never sweep an unexplained orphan on the strength of missing metadata.
		for _, suffix := range []string{".data", ".tmp"} {
			if _, err := s.root.Lstat(proof.ID + suffix); !errors.Is(err, os.ErrNotExist) {
				return ErrUploadInvalid
			}
		}
		return syncRootWith(s.root, s.syncFile)
	}
	if err != nil {
		return err
	}
	if r.Owner != owner || r.Bot != bot {
		return ErrUploadNotFound
	}
	if r.State != "ready" || r.ID != proof.ID || r.Offset != proof.Offset || r.SizeBytes != proof.SizeBytes ||
		r.SHA256 != proof.ContentHash || r.ContentHash != proof.ContentHash || r.Mime != proof.Mime || !r.ExpiresAt.Equal(proof.ExpiresAt) {
		return ErrUploadConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Metadata is last: a crash before its unlink can safely repeat the exact
	// terminal check. Neither the canonical original nor the DB ledger is touched.
	for _, suffix := range []string{".data", ".tmp", ".json"} {
		if err := s.remove(proof.ID + suffix); err != nil {
			return err
		}
	}
	return syncRootWith(s.root, s.syncFile)
}
