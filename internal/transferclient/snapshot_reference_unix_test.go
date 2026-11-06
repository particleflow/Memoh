//go:build linux || darwin

package transferclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

// Single-pass reference for equivalence tests and the frozen baseline benchmark.
// Production admission uses the resumable progress protocol.
// sealSnapshot performs one full source pass, bounded scratch memory and a
// linear-size tree construction. Only a matching full SHA may be admitted.
func sealSnapshot(ctx context.Context, source io.Reader, data, tree *os.File, spec Spec) (*SnapshotSeal, error) {
	if !spec.valid() || spec.SizeBytes > 1<<40 {
		return nil, ErrInvalid
	}
	h := sha256.New()
	buffer := make([]byte, snapshotBlockBytes)
	remaining := spec.SizeBytes
	for index := int64(0); remaining > 0; index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := io.ReadFull(source, buffer[:min(snapshotBlockBytes, remaining)])
		if err != nil {
			return nil, ErrSnapshot
		}
		if written, err := data.Write(buffer[:n]); err != nil || written != n {
			return nil, ErrStore
		}
		_, _ = h.Write(buffer[:n])
		leaf := blockDigest(index, buffer[:n])
		if n, err := tree.Write(leaf[:]); err != nil || n != sha256.Size {
			return nil, ErrStore
		}
		remaining -= int64(n)
	}
	var extra [1]byte
	if n, err := source.Read(extra[:]); n != 0 || !errors.Is(err, io.EOF) || hex.EncodeToString(h.Sum(nil)) != spec.SHA256 {
		return nil, ErrSnapshot
	}
	count, base := snapshotBlocks(spec.SizeBytes), int64(0)
	for count > 1 {
		for index := int64(0); index < count; index += 2 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var left, right [sha256.Size]byte
			if _, err := tree.ReadAt(left[:], base+index*sha256.Size); err != nil {
				return nil, ErrStore
			}
			right = left
			if index+1 < count {
				if _, err := tree.ReadAt(right[:], base+(index+1)*sha256.Size); err != nil {
					return nil, ErrStore
				}
			}
			parent := parentDigest(left, right)
			if n, err := tree.Write(parent[:]); err != nil || n != sha256.Size {
				return nil, ErrStore
			}
		}
		base += count * sha256.Size
		count = (count + 1) / 2
	}
	var root [sha256.Size]byte
	if _, err := tree.ReadAt(root[:], base); err != nil {
		return nil, ErrStore
	}
	if data.Sync() != nil || tree.Sync() != nil {
		return nil, ErrStore
	}
	dataID, err := queueFileIdentity(data)
	if err != nil {
		return nil, err
	}
	treeID, err := queueFileIdentity(tree)
	if err != nil {
		return nil, err
	}
	return &SnapshotSeal{Version: 1, Root: hex.EncodeToString(root[:]), DataIdentity: dataID, TreeIdentity: treeID}, nil
}
