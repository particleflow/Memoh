package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/felinics/memoh/internal/storage"
)

// ReadSeekCloser is a verified media stream usable for HTTP byte ranges.
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// OpenSeekable uses a provider's explicit verified-range contract when present.
// Legacy providers retain full-file verification and disk-only spooling.
func (s *Service) OpenSeekable(ctx context.Context, botID, contentHash string) (ReadSeekCloser, Asset, error) {
	if opener, ok := s.provider.(storage.VerifiedRangeOpener); ok {
		asset, err := s.Resolve(ctx, botID, contentHash)
		if err != nil {
			return nil, Asset{}, err
		}
		r, err := opener.OpenVerifiedRange(ctx, path.Join(botID, asset.StorageKey))
		if err == nil {
			if r.VerifiedContentHash() != asset.ContentHash {
				_ = r.Close()
				return nil, Asset{}, errors.New("verified range content identity mismatch")
			}
			return r, asset, nil
		}
		if !errors.Is(err, storage.ErrVerifiedRangeUnsupported) {
			return nil, Asset{}, fmt.Errorf("open verified media range: %w", err)
		}
	}
	reader, asset, err := s.Open(ctx, botID, contentHash)
	if err != nil {
		return nil, Asset{}, err
	}
	if seekable, ok := reader.(ReadSeekCloser); ok {
		// Detect seek failures before HTTP headers are committed.
		if _, err := seekable.Seek(0, io.SeekEnd); err != nil {
			_ = reader.Close()
			return nil, Asset{}, fmt.Errorf("size media: %w", err)
		}
		if _, err := seekable.Seek(0, io.SeekStart); err != nil {
			_ = reader.Close()
			return nil, Asset{}, fmt.Errorf("rewind media: %w", err)
		}
		// Legacy filesystem providers may not verify on Open. Hash the full
		// stream too, so a range never hides corruption outside that range.
		hasher := sha256.New()
		if _, err := io.Copy(hasher, contextReader{ctx, seekable}); err != nil {
			_ = reader.Close()
			return nil, Asset{}, fmt.Errorf("verify media: %w", err)
		}
		if hex.EncodeToString(hasher.Sum(nil)) != asset.ContentHash {
			_ = reader.Close()
			return nil, Asset{}, errors.New("media integrity check failed")
		}
		if _, err := seekable.Seek(0, io.SeekStart); err != nil {
			_ = reader.Close()
			return nil, Asset{}, fmt.Errorf("rewind verified media: %w", err)
		}
		return seekable, asset, nil
	}
	defer func() { _ = reader.Close() }()
	f, err := os.CreateTemp("", "memoh-media-download-*")
	if err != nil {
		return nil, Asset{}, fmt.Errorf("create media spool: %w", err)
	}
	spool := &temporaryMedia{File: f}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), contextReader{ctx, reader}); err != nil {
		_ = spool.Close()
		return nil, Asset{}, fmt.Errorf("spool media: %w", err)
	}
	if hex.EncodeToString(hasher.Sum(nil)) != asset.ContentHash {
		_ = spool.Close()
		return nil, Asset{}, errors.New("media integrity check failed")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = spool.Close()
		return nil, Asset{}, fmt.Errorf("rewind media spool: %w", err)
	}
	return spool, asset, nil
}

type temporaryMedia struct{ *os.File }

func (f *temporaryMedia) Close() error {
	err := f.File.Close()
	_ = os.Remove(f.Name()) //nolint:gosec // path comes from os.CreateTemp
	return err
}
