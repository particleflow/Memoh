package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadSeekCloser is a verified media stream usable for HTTP byte ranges.
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// OpenSeekable retains originals' full-file verification. Legacy streaming
// providers are spooled to disk, never memory, and verified before serving.
func (s *Service) OpenSeekable(ctx context.Context, botID, contentHash string) (ReadSeekCloser, Asset, error) {
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
