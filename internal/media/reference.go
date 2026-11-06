package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/felinics/memoh/internal/storage"
)

// A content hash is the identity of an asset; this file resolves the address
// half of that reference: which Bot's directory currently backs the bytes.
// Resolution follows the existing per-Bot routing — finding an owner is what
// makes the reference readable. Reads never copy: the owner keeps the only
// canonical copy (view semantics).

// resolveOwnerByHash finds the Bot directory that holds the hash's original.
// It scans every Bot root through the provider, in the provider's
// deterministic order, and returns the first owner whose key matches the hash
// exactly.
func (s *Service) resolveOwnerByHash(ctx context.Context, contentHash string) (Asset, error) {
	if !validHashReference(contentHash) {
		return Asset{}, ErrAssetNotFound
	}
	lister, ok := s.provider.(storage.CrossBotPrefixLister)
	if !ok {
		return Asset{}, ErrAssetNotFound
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	keys, err := lister.ListPrefixAcrossBots(ctx, path.Join(contentHash[:2], contentHash))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Asset{}, ErrAssetNotFound
		}
		return Asset{}, fmt.Errorf("find original owner: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	for _, key := range keys {
		owner, storageKey := splitFirst(key, '/')
		if owner == "" || !validStorageKey(storageKey) {
			continue
		}
		// Require the exact canonical directory and basename stem, not a
		// prefix collision or a provider-supplied owner claim.
		ext := path.Ext(storageKey)
		if ext == "" || storageKey != path.Join(contentHash[:2], contentHash+ext) {
			continue
		}
		asset := deriveAssetFromKey(owner, storageKey)
		if asset.ContentHash == contentHash {
			return asset, nil
		}
	}
	return Asset{}, ErrAssetNotFound
}

// validHashReference accepts exactly a lowercase SHA-256 hex digest, before
// storage is reached.
func validHashReference(contentHash string) bool {
	if contentHash != strings.ToLower(contentHash) || len(contentHash) != hex.EncodedLen(sha256.Size) {
		return false
	}
	_, err := hex.DecodeString(contentHash)
	return err == nil
}

// resolveReference resolves a hash for botID, falling back to the owner
// resolution when the Bot's own directory does not hold the bytes. The
// returned asset names the owner, so a reference points at the Bot whose
// directory backs it.
func (s *Service) resolveReference(ctx context.Context, botID, contentHash string) (Asset, error) {
	asset, err := s.resolveByContentHash(ctx, botID, contentHash)
	if err == nil {
		return asset, nil
	}
	if !errors.Is(err, ErrAssetNotFound) {
		return Asset{}, err
	}
	return s.resolveOwnerByHash(ctx, contentHash)
}

// EnsureAccessPathFor returns a path the consuming Bot can read. An asset the
// consumer owns keeps the existing materialization. An asset another Bot owns
// is refreshed from that owner's verified original into the consumer's
// addressable storage — an access copy, never a second canonical owner.
func (s *Service) EnsureAccessPathFor(ctx context.Context, consumerBotID string, asset Asset) (string, error) {
	if s.provider == nil {
		return "", ErrProviderUnavailable
	}
	if !validPathComponent(consumerBotID) {
		return "", ErrAssetNotFound
	}
	if asset.BotID == consumerBotID {
		return s.EnsureAccessPath(ctx, asset)
	}
	if !validPathComponent(asset.BotID) || !validStorageKey(asset.StorageKey) {
		return "", ErrAssetNotFound
	}
	materializer, ok := s.provider.(storage.AccessPathMaterializer)
	if !ok {
		return "", storage.ErrAccessPathUnavailable
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	accessPath, err := materializer.MaterializeAccessPath(ctx,
		path.Join(consumerBotID, asset.StorageKey),
		path.Join(asset.BotID, asset.StorageKey))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %w", ErrAssetNotFound, err)
		}
		return "", err
	}
	accessPath = strings.TrimSpace(accessPath)
	if accessPath == "" {
		return "", storage.ErrAccessPathUnavailable
	}
	return accessPath, nil
}
