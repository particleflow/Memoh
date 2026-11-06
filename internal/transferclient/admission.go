package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"os"
)

var ErrAdmissionPending = errors.New("delivery admission has durable resumable work")

const (
	admissionSlotBytes       = 4096
	admissionFileBytes       = 2 * admissionSlotBytes
	admissionCopyBudget      = 64 << 20
	admissionCheckpointBytes = 8 << 20
	admissionTreeBudget      = 4 << 20
)

// Two fixed slots bound admission metadata regardless of file size/retry count.
// The journal binds this protocol before any source bytes are copied.
type admissionProgress struct {
	Version      int        `json:"version"`
	Indexed      bool       `json:"indexed,omitempty"`
	Sequence     uint64     `json:"sequence"`
	Binding      string     `json:"binding"`
	Stage        string     `json:"stage"`
	Offset       int64      `json:"offset"`
	TreeBytes    int64      `json:"tree_bytes"`
	HashState    []byte     `json:"hash_state"`
	Frontier     [21][]byte `json:"frontier"`
	DataIdentity string     `json:"data_identity"`
	TreeIdentity string     `json:"tree_identity"`
}

func sameAdmission(a, b Job) bool {
	return a.HostBinding == b.HostBinding && a.Task == b.Task && a.DeliveryID == b.DeliveryID && a.BotID == b.BotID && a.OperationID == b.OperationID && a.SnapshotID == b.SnapshotID && a.LocatorRevision == b.LocatorRevision && a.Spec == b.Spec
}

func admissionBinding(job Job) string {
	body, _ := json.Marshal(struct {
		Delivery, Bot, Operation, Snapshot, Revision string
		Spec                                         Spec
	}{job.DeliveryID, job.BotID, job.OperationID, job.SnapshotID, job.LocatorRevision, job.Spec})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (p *admissionProgress) valid(job Job) bool {
	if p.Version != 1 || p.Sequence == 0 || p.Binding != admissionBinding(job) || p.Offset < 0 || p.Offset > job.Spec.SizeBytes || p.Offset != job.Spec.SizeBytes && p.Offset%snapshotBlockBytes != 0 || len(p.HashState) > 256 || len(p.DataIdentity) == 0 || len(p.DataIdentity) > 128 || len(p.TreeIdentity) == 0 || len(p.TreeIdentity) > 128 {
		return false
	}
	leaves := snapshotBlocks(p.Offset)
	for i, node := range p.Frontier {
		if leaves&(1<<i) != 0 && len(node) != sha256.Size || leaves&(1<<i) == 0 && len(node) != 0 {
			return false
		}
	}
	switch p.Stage {
	case "copy":
		return p.TreeBytes == leaves*sha256.Size
	case "tree", "sealed":
		return p.Offset == job.Spec.SizeBytes && p.TreeBytes >= leaves*sha256.Size && p.TreeBytes <= snapshotTreeBytes(p.Offset) && p.TreeBytes%sha256.Size == 0 && (p.Stage != "sealed" || p.TreeBytes == snapshotTreeBytes(p.Offset))
	default:
		return false
	}
}

func readAdmission(f *os.File, job Job) (admissionProgress, error) {
	var best admissionProgress
	for slot := int64(0); slot < 2; slot++ {
		var frame [admissionSlotBytes]byte
		if _, err := f.ReadAt(frame[:], slot*admissionSlotBytes); err != nil {
			continue
		}
		length := int(binary.BigEndian.Uint32(frame[:4]))
		if length < 1 || length > admissionSlotBytes-36 {
			continue
		}
		body := frame[36 : 36+length]
		sum := sha256.Sum256(body)
		var p admissionProgress
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if !bytes.Equal(sum[:], frame[4:36]) || dec.Decode(&p) != nil || !p.valid(job) {
			continue
		}
		if p.Sequence > best.Sequence {
			best = p
		}
	}
	if best.Sequence == 0 {
		return best, ErrStore
	}
	return best, nil
}

func saveAdmission(f, data, tree *os.File, p *admissionProgress, h hash.Hash) error {
	if !p.Indexed && data.Sync() != nil || tree.Sync() != nil {
		return ErrStore
	}
	var err error
	if p.HashState, err = h.(encoding.BinaryMarshaler).MarshalBinary(); err != nil {
		return ErrStore
	}
	if p.DataIdentity, err = queueFileIdentity(data); err != nil {
		return err
	}
	if p.TreeIdentity, err = queueFileIdentity(tree); err != nil {
		return err
	}
	if p.Sequence == ^uint64(0) {
		return ErrCapacity
	}
	p.Sequence++
	body, err := json.Marshal(p)
	if err != nil || len(body) > admissionSlotBytes-36 {
		return ErrStore
	}
	var frame [admissionSlotBytes]byte
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body))) //nolint:gosec // Bounded above by slot size.
	sum := sha256.Sum256(body)
	copy(frame[4:36], sum[:])
	copy(frame[36:], body)
	if n, err := f.WriteAt(frame[:], int64(p.Sequence%2)*admissionSlotBytes); err != nil || n != len(frame) || f.Sync() != nil {
		return ErrStore
	}
	return nil
}

func addFrontier(p *admissionProgress, index int64, digest [sha256.Size]byte) {
	for level := 0; level < len(p.Frontier); level++ {
		if index&(1<<level) == 0 {
			p.Frontier[level] = append([]byte(nil), digest[:]...)
			return
		}
		digest = parentDigest(*(*[sha256.Size]byte)(p.Frontier[level]), digest)
		p.Frontier[level] = nil
	}
}

func frontierRoot(p admissionProgress) [sha256.Size]byte {
	var root [sha256.Size]byte
	height := -1
	for level, node := range p.Frontier {
		if len(node) == 0 {
			continue
		}
		if height == -1 {
			copy(root[:], node)
			height = level
			continue
		}
		for height < level {
			root = parentDigest(root, root)
			height++
		}
		root = parentDigest(*(*[sha256.Size]byte)(node), root)
		height = level + 1
	}
	return root
}

func (q *DiskQueue) resumeAdmission(ctx context.Context, r QueueStatus, journal, source *os.File) error {
	id := r.Job.DeliveryID
	progress, err := q.file(id+".admission", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = progress.Close() }()
	info, err := progress.Stat()
	if err != nil {
		return ErrStore
	}
	h := sha256.New()
	p := admissionProgress{Version: 1, Indexed: r.Registered, Binding: admissionBinding(r.Job), Stage: "copy"}
	fresh := info.Size() == 0
	if !fresh {
		if info.Size() != admissionFileBytes {
			return ErrStore
		}
		p, err = readAdmission(progress, r.Job)
		if err != nil {
			return err
		}
		if p.Indexed != r.Registered {
			return ErrStore
		}
		if h.(encoding.BinaryUnmarshaler).UnmarshalBinary(p.HashState) != nil {
			return ErrStore
		}
	}
	// A sealed checkpoint precedes readonly metadata. Reopening may finish the
	// same transition after interruption; it never alters an admitted snapshot.
	if p.Stage == "sealed" {
		suffixes := []string{".tree"}
		if !r.Registered {
			suffixes = append(suffixes, ".snapshot")
		}
		for _, suffix := range suffixes {
			f, err := q.file(id+suffix, os.O_RDONLY, 0o600)
			if err != nil {
				return err
			}
			err = f.Chmod(0o600)
			_ = f.Close()
			if err != nil {
				return ErrStore
			}
		}
	}
	data := source
	if !r.Registered {
		data, err = q.file(id+".snapshot", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		defer func() { _ = data.Close() }()
	} else if source == nil {
		return ErrSnapshot
	}
	tree, err := q.file(id+".tree", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = tree.Close() }()
	if fresh {
		if !r.Registered && data.Truncate(0) != nil || tree.Truncate(0) != nil || progress.Truncate(admissionFileBytes) != nil || syncQueueDir(q.root) != nil {
			return ErrStore
		}
		if err := saveAdmission(progress, data, tree, &p, h); err != nil {
			return err
		}
	} else {
		for _, v := range []struct {
			f        *os.File
			size     int64
			identity string
		}{{data, p.Offset, p.DataIdentity}, {tree, p.TreeBytes, p.TreeIdentity}} {
			info, err := v.f.Stat()
			if err != nil || info.Size() < v.size || (!r.Registered || v.f != data) && v.f.Truncate(v.size) != nil {
				return ErrStore
			}
			identity, err := queueFileIdentity(v.f)
			if err != nil || identity != v.identity {
				return ErrSnapshot
			}
		}
	}
	if p.Stage == "copy" {
		if source == nil {
			return ErrSnapshot
		}
		stable, release, err := guardQueueSource(source)
		if err != nil {
			return err
		}
		defer release()
		source = stable
		copyErr := copyAdmission(ctx, source, data, tree, progress, r.Job, &p, h)
		if err := sameSourceAfterRead(r, source); err != nil {
			return err
		}
		if copyErr != nil {
			return copyErr
		}
	}
	if p.Stage == "tree" {
		if err := buildAdmissionTree(ctx, data, tree, progress, r.Job, &p, h); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root := frontierRoot(p)
	var actual [sha256.Size]byte
	if _, err := tree.ReadAt(actual[:], p.TreeBytes-sha256.Size); err != nil || actual != root {
		return ErrSnapshot
	}
	r.Seal = &SnapshotSeal{Version: 1, Root: hex.EncodeToString(root[:]), DataIdentity: p.DataIdentity, TreeIdentity: p.TreeIdentity}
	if !r.Registered && (data.Chmod(0o400) != nil || data.Sync() != nil) || tree.Chmod(0o400) != nil || tree.Sync() != nil || syncQueueDir(q.root) != nil {
		return ErrStore
	}
	r.Admitted = true
	return q.save(journal, r)
}

func copyAdmission(ctx context.Context, source, data, tree, progress *os.File, job Job, p *admissionProgress, h hash.Hash) error {
	if source == nil {
		return ErrSnapshot
	}
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != job.Spec.SizeBytes {
		return ErrSnapshot
	}
	start, saved := p.Offset, p.Offset
	buffer := make([]byte, snapshotBlockBytes)
	checkpoint := func() error { return saveAdmission(progress, data, tree, p, h) }
	for p.Offset < job.Spec.SizeBytes {
		stop := ctx.Err()
		if stop == nil && p.Offset-start >= admissionCopyBudget {
			stop = ErrAdmissionPending
		}
		if stop != nil {
			if p.Offset != saved {
				if err := checkpoint(); err != nil {
					return err
				}
			}
			return stop
		}
		length := min(snapshotBlockBytes, job.Spec.SizeBytes-p.Offset)
		if _, err := source.ReadAt(buffer[:length], p.Offset); err != nil {
			return ErrSnapshot
		}
		if !p.Indexed {
			if n, err := data.WriteAt(buffer[:length], p.Offset); err != nil || int64(n) != length {
				return ErrStore
			}
		}
		leaf := blockDigest(p.Offset/snapshotBlockBytes, buffer[:length])
		if n, err := tree.WriteAt(leaf[:], p.TreeBytes); err != nil || n != sha256.Size {
			return ErrStore
		}
		_, _ = h.Write(buffer[:length])
		addFrontier(p, p.Offset/snapshotBlockBytes, leaf)
		p.Offset += length
		p.TreeBytes += sha256.Size
		if p.Offset-saved >= admissionCheckpointBytes {
			if err := checkpoint(); err != nil {
				return err
			}
			saved = p.Offset
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != job.Spec.SHA256 {
		return ErrSnapshot
	}
	info, err = source.Stat()
	if err != nil || info.Size() != job.Spec.SizeBytes {
		return ErrSnapshot
	}
	p.Stage = "tree"
	return checkpoint()
}

func buildAdmissionTree(ctx context.Context, data, tree, progress *os.File, job Job, p *admissionProgress, h hash.Hash) error {
	start, saved := p.TreeBytes, p.TreeBytes
	checkpoint := func() error { return saveAdmission(progress, data, tree, p, h) }
	for p.TreeBytes < snapshotTreeBytes(job.Spec.SizeBytes) {
		stop := ctx.Err()
		if stop == nil && p.TreeBytes-start >= admissionTreeBudget {
			stop = ErrAdmissionPending
		}
		if stop != nil {
			if p.TreeBytes != saved {
				if err := checkpoint(); err != nil {
					return err
				}
			}
			return stop
		}
		base, count := int64(0), snapshotBlocks(job.Spec.SizeBytes)
		for p.TreeBytes >= base+(count+(count+1)/2)*sha256.Size {
			base += count * sha256.Size
			count = (count + 1) / 2
		}
		index := (p.TreeBytes - (base + count*sha256.Size)) / sha256.Size * 2
		var left, right [sha256.Size]byte
		if _, err := tree.ReadAt(left[:], base+index*sha256.Size); err != nil {
			return ErrStore
		}
		right = left
		if index+1 < count {
			if _, err := tree.ReadAt(right[:], base+(index+1)*sha256.Size); err != nil {
				return ErrStore
			}
		}
		parent := parentDigest(left, right)
		if n, err := tree.WriteAt(parent[:], p.TreeBytes); err != nil || n != sha256.Size {
			return ErrStore
		}
		p.TreeBytes += sha256.Size
		if p.TreeBytes-saved >= 256<<10 {
			if err := checkpoint(); err != nil {
				return err
			}
			saved = p.TreeBytes
		}
	}
	var actual [sha256.Size]byte
	if _, err := tree.ReadAt(actual[:], p.TreeBytes-sha256.Size); err != nil || actual != frontierRoot(*p) {
		return ErrSnapshot
	}
	p.Stage = "sealed"
	return checkpoint()
}
