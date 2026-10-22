package transferclient

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
)

const snapshotBlockBytes int64 = 1 << 20

// SnapshotSeal binds the tree created during the admitted full-hash copy to
// stable opened-file identities. It is integrity metadata, never authority.
type SnapshotSeal struct {
	Version      int    `json:"version"`
	Root         string `json:"root"`
	DataIdentity string `json:"data_identity"`
	TreeIdentity string `json:"tree_identity"`
}

func snapshotBlocks(size int64) int64 { return (size + snapshotBlockBytes - 1) / snapshotBlockBytes }

func snapshotTreeBytes(size int64) int64 {
	count, total := snapshotBlocks(size), int64(0)
	for {
		total += count * sha256.Size
		if count <= 1 {
			return total
		}
		count = (count + 1) / 2
	}
}

func (s *SnapshotSeal) valid() bool {
	return s != nil && s.Version == 1 && validHash(s.Root) && len(s.DataIdentity) > 0 && len(s.DataIdentity) <= 128 && len(s.TreeIdentity) > 0 && len(s.TreeIdentity) <= 128
}

func blockDigest(index int64, data []byte) [sha256.Size]byte {
	var prefix [17]byte
	binary.BigEndian.PutUint64(prefix[1:9], uint64(index)) //nolint:gosec // Caller derives a nonnegative block index from bounded size.
	binary.BigEndian.PutUint64(prefix[9:], uint64(len(data)))
	h := sha256.New()
	_, _ = h.Write(prefix[:])
	_, _ = h.Write(data)
	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	return sum
}

func parentDigest(left, right [sha256.Size]byte) [sha256.Size]byte {
	var data [1 + 2*sha256.Size]byte
	data[0] = 1
	copy(data[1:], left[:])
	copy(data[1+sha256.Size:], right[:])
	return sha256.Sum256(data[:])
}

// sealedSnapshot validates only touched blocks plus logarithmic-size proofs.
// Its host owns the protected handles for the whole finite invocation.
type sealedSnapshot struct {
	data io.ReaderAt
	tree io.ReaderAt
	job  Job
	seal SnapshotSeal
}

func (s sealedSnapshot) ReadAt(ctx context.Context, ref, revision string, spec Spec, dst []byte, offset int64) error {
	if ref != s.job.SnapshotID || revision != s.job.LocatorRevision || spec != s.job.Spec || !spec.valid() || !s.seal.valid() || offset < 0 || int64(len(dst)) > spec.SizeBytes-offset || spec.SizeBytes > 1<<40 {
		return ErrSnapshot
	}
	root, err := hex.DecodeString(s.seal.Root)
	if err != nil {
		return ErrSnapshot
	}
	var scratch []byte
	for len(dst) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		index := offset / snapshotBlockBytes
		start := index * snapshotBlockBytes
		length := min(snapshotBlockBytes, spec.SizeBytes-start)
		var block []byte
		if offset == start && int64(len(dst)) >= length {
			block = dst[:length]
		} else {
			if scratch == nil {
				scratch = make([]byte, snapshotBlockBytes)
			}
			block = scratch[:length]
		}
		if _, err := s.data.ReadAt(block, start); err != nil {
			return ErrSnapshot
		}
		sum := blockDigest(index, block)
		node, count, base := index, snapshotBlocks(spec.SizeBytes), int64(0)
		for count > 1 {
			sibling := node ^ 1
			other := sum
			if sibling < count {
				if _, err := s.tree.ReadAt(other[:], base+sibling*sha256.Size); err != nil {
					return ErrSnapshot
				}
			}
			if node&1 == 0 {
				sum = parentDigest(sum, other)
			} else {
				sum = parentDigest(other, sum)
			}
			base += count * sha256.Size
			count = (count + 1) / 2
			node /= 2
		}
		if string(sum[:]) != string(root) {
			return ErrSnapshot
		}
		n := copy(dst, block[offset-start:length])
		dst = dst[n:]
		offset += int64(n)
	}
	return nil
}

func (q *DiskQueue) openSnapshot(r QueueStatus) (Snapshot, func(), error) {
	if !r.Seal.valid() {
		return nil, nil, ErrSnapshot
	}
	data, err := q.file(r.Job.DeliveryID+".snapshot", os.O_RDONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	if err := lockQueueFile(data); err != nil {
		_ = data.Close()
		return nil, nil, err
	}
	closeData := func() { unlockClose(data) }
	tree, err := q.file(r.Job.DeliveryID+".tree", os.O_RDONLY, 0o600)
	if err != nil {
		closeData()
		return nil, nil, err
	}
	if err := lockQueueFile(tree); err != nil {
		_ = tree.Close()
		closeData()
		return nil, nil, err
	}
	closeBoth := func() { unlockClose(tree); closeData() }
	dataID, err := queueFileIdentity(data)
	if err != nil || dataID != r.Seal.DataIdentity {
		closeBoth()
		return nil, nil, ErrSnapshot
	}
	treeID, err := queueFileIdentity(tree)
	if err != nil || treeID != r.Seal.TreeIdentity {
		closeBoth()
		return nil, nil, ErrSnapshot
	}
	dataInfo, err := data.Stat()
	if err != nil || dataInfo.Size() != r.Job.Spec.SizeBytes || dataInfo.Mode().Perm()&0o222 != 0 {
		closeBoth()
		return nil, nil, ErrSnapshot
	}
	treeInfo, err := tree.Stat()
	if err != nil || treeInfo.Size() != snapshotTreeBytes(r.Job.Spec.SizeBytes) || treeInfo.Mode().Perm()&0o222 != 0 {
		closeBoth()
		return nil, nil, ErrSnapshot
	}
	var root [sha256.Size]byte
	if _, err := tree.ReadAt(root[:], treeInfo.Size()-sha256.Size); err != nil || hex.EncodeToString(root[:]) != r.Seal.Root {
		closeBoth()
		return nil, nil, ErrSnapshot
	}
	return sealedSnapshot{data: data, tree: tree, job: r.Job, seal: *r.Seal}, closeBoth, nil
}
