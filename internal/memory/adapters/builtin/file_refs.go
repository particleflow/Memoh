package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path"
	"strings"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/memory/adapters"
)

// FileResolver is the existing Bot-scoped original-file reader. Opening, rather
// than merely inspecting a claimed hash, checks current readability. The caller
// still uses the regular authenticated media endpoint to retrieve the bytes.
type FileResolver interface {
	Open(context.Context, string, string) (io.ReadCloser, media.Asset, error)
}

func (p *BuiltinProvider) SetFileResolver(files FileResolver) {
	p.files = files
}

func (p *BuiltinProvider) projectFileRefs(ctx context.Context, botID string, items []adapters.MemoryItem) {
	// A page may contain many memories referring to the same immutable asset.
	// Cache only within this response, so later reads recheck availability.
	checked := make(map[string]adapters.MemoryFileRef)
	for i := range items {
		item := &items[i]
		item.FileRefs = nil // Never trust runtime/metadata-provided projections.
		raw, present := item.Metadata["content_hash"]
		if !present {
			continue
		}
		ref := adapters.MemoryFileRef{Status: "unavailable"}
		hash, ok := raw.(string)
		if !ok || !canonicalFileHash(hash) || botID == "" || (item.BotID != "" && item.BotID != botID) {
			item.FileRefs = []adapters.MemoryFileRef{ref}
			continue
		}
		if cached, ok := checked[hash]; ok {
			item.FileRefs = []adapters.MemoryFileRef{cached}
			continue
		}
		if p.files != nil {
			// Ignore every metadata owner, path, URL, name, MIME and status. Only
			// the trusted caller Bot and canonical hash reach the existing store.
			reader, asset, err := p.files.Open(ctx, botID, hash)
			if reader != nil {
				closeErr := reader.Close()
				if err == nil && closeErr == nil && asset.BotID == botID && asset.ContentHash == hash {
					ref = adapters.MemoryFileRef{Status: "available", ContentHash: hash, Name: path.Base(asset.StorageKey), Mime: asset.Mime}
				}
			}
		}
		checked[hash] = ref
		item.FileRefs = []adapters.MemoryFileRef{ref}
	}
}

func canonicalFileHash(value string) bool {
	if len(value) != hex.EncodedLen(sha256.Size) || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
