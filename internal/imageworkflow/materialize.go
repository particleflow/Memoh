package imageworkflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/felinics/memoh/internal/media"
)

// MaterializeRequest asks for one input directory entry.
type MaterializeRequest struct {
	// OriginalsRoot is the canonical original root (config files.originals_root).
	OriginalsRoot string
	// InputDir is the workflow input directory (<workflows>/<pair_key>).
	InputDir string
	// Asset is the value Ingest returned. Its BotID and StorageKey name the
	// canonical original and may belong to another Bot on a dedup hit, so the
	// link source always comes from this value and never from the request's bot.
	Asset media.Asset
}

// MaterializedFile is the directory entry the trigger batch names afterwards.
type MaterializedFile struct {
	Path        string
	ContentHash string
}

// MaterializeInput hardlinks the canonical original into the workflow input
// directory as <content_hash><ext>. One original, one extra directory entry,
// no second copy; an existing entry for the same hash is left alone.
func MaterializeInput(_ context.Context, req MaterializeRequest) (MaterializedFile, error) {
	if req.OriginalsRoot == "" || req.InputDir == "" {
		return MaterializedFile{}, ErrInvalid
	}
	hash := req.Asset.ContentHash
	if hash == "" {
		return MaterializedFile{}, ErrInvalid
	}
	// The link source is the resolved Asset's own BotID and StorageKey: on a
	// cross-Bot dedup hit the bytes live under another Bot's directory.
	source := filepath.Join(req.OriginalsRoot, req.Asset.BotID, filepath.FromSlash(req.Asset.StorageKey))
	target := InputFilePath(req.InputDir, hash, path.Ext(req.Asset.StorageKey))

	if _, err := os.Stat(target); err == nil {
		// An entry for this hash already exists; leaving it alone is the
		// idempotent answer and keeps one inode.
		return MaterializedFile{Path: target, ContentHash: hash}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return MaterializedFile{}, err
	}
	if _, err := os.Stat(source); err != nil {
		// A missing original fails the trigger; the server never silently copies
		// bytes to stand in for it.
		return MaterializedFile{}, fmt.Errorf("image workflow: original %s unavailable: %w", source, err)
	}
	if err := os.MkdirAll(req.InputDir, 0o750); err != nil {
		return MaterializedFile{}, fmt.Errorf("image workflow: create input directory: %w", err)
	}
	if err := os.Link(source, target); err != nil {
		return MaterializedFile{}, fmt.Errorf("image workflow: link %s: %w", target, err)
	}
	return MaterializedFile{Path: target, ContentHash: hash}, nil
}

// InputFilePath is the directory entry for one content hash: the hash plus the
// canonical extension, so the entry opens as the same media type.
func InputFilePath(dir, contentHash, ext string) string {
	return filepath.Join(dir, contentHash+ext)
}
