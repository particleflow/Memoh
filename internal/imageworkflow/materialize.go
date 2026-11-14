package imageworkflow

import (
	"context"
	"path"

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
func MaterializeInput(_ context.Context, _ MaterializeRequest) (MaterializedFile, error) {
	// TODO(F5): link OriginalsRoot/<Asset.BotID>/<Asset.StorageKey> to
	// InputDir/<hash><ext>, leave an existing entry alone, and never fall back
	// to a copy.
	return MaterializedFile{}, ErrNotImplemented
}

// InputFilePath is the directory entry for one content hash: the hash plus the
// canonical extension, so the entry opens as the same media type.
func InputFilePath(dir, contentHash, ext string) string {
	return path.Join(dir, contentHash+ext)
}
