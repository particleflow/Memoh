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

// ErrLocatorUnsupported means the provider cannot locate assets without opening
// their contents. Locator callers must not fall back to a content read.
var ErrLocatorUnsupported = errors.New("provider does not support read-only media lookup")

// Locate returns a Bot-scoped locator derived only from directory metadata.
// It never opens payload bytes, migrates legacy objects, repairs durability, or
// acknowledges committed storage. A found locator is not proof of readability
// or integrity; consumers must still authorize and verify their actual reads.
func (s *Service) Locate(ctx context.Context, botID, contentHash string) (Asset, error) {
	if !validPathComponent(botID) || len(contentHash) != hex.EncodedLen(sha256.Size) || contentHash != strings.ToLower(contentHash) {
		return Asset{}, ErrAssetNotFound
	}
	if _, err := hex.DecodeString(contentHash); err != nil {
		return Asset{}, ErrAssetNotFound
	}
	if s.provider == nil {
		return Asset{}, ErrProviderUnavailable
	}
	lister, ok := s.provider.(storage.PrefixLister)
	if !ok {
		return Asset{}, ErrLocatorUnsupported
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	keys, err := lister.ListPrefix(ctx, path.Join(botID, contentHash[:2], contentHash))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Asset{}, ErrAssetNotFound
		}
		return Asset{}, fmt.Errorf("locate media: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	for _, key := range keys {
		owner, storageKey := splitFirst(key, '/')
		if owner != botID || !validStorageKey(storageKey) {
			continue
		}
		// Require the exact canonical directory and basename stem, not a prefix
		// collision, traversal component or a provider-supplied owner claim.
		ext := path.Ext(storageKey)
		if ext == "" || storageKey != path.Join(contentHash[:2], contentHash+ext) {
			continue
		}
		asset := deriveAssetFromKey(botID, storageKey)
		if asset.ContentHash == contentHash {
			return asset, nil
		}
	}
	return Asset{}, ErrAssetNotFound
}
