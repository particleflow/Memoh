package localfs

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"hash/fnv"
	"io"
	"os"
	"sync"

	"github.com/felinics/memoh/internal/storage"
)

const (
	rangeBlockBytes = 1 << 20
	rangeIndexLimit = 8 << 20
	rangeCacheBytes = 16 << 20
	rangeCacheItems = 128
)

var errOriginalChanged = errors.New("original changed during verified read")

// Only the Linux implementation supplies a sufficiently strong change stamp.
// The cache is process-local: no filename, timestamp-only or sidecar trust is
// imported on restart. The inode must be single-linked and regular.
type fileStamp struct {
	device, inode, mode uint64
	size                int64
	mtimeSec, mtimeNsec int64
	ctimeSec, ctimeNsec int64
}

type rangeIndex struct {
	stamp  fileStamp
	blocks [][sha256.Size]byte
}

// A scan is unfinished work, never a readable index. Ownership transfers only
// under the key's verification stripe; the cache never shares a mutable scan.
// Its hash state and digests are process-local and disappear on restart.
type rangeScan struct {
	stamp  fileStamp
	blocks [][sha256.Size]byte
	next   int
	full   hash.Hash
}

type cachedRange struct {
	key   string
	stamp fileStamp
	index *rangeIndex
	scan  *rangeScan
	bytes int
}

type rangeCache struct {
	mu     sync.Mutex
	lru    *list.List
	items  map[string]*list.Element
	bytes  int
	guards [32]chan struct{}
}

func newRangeCache() *rangeCache {
	c := &rangeCache{lru: list.New(), items: make(map[string]*list.Element)}
	for i := range c.guards {
		c.guards[i] = make(chan struct{}, 1)
	}
	return c
}

func (c *rangeCache) lock(ctx context.Context, key string) (func(), error) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	ch := c.guards[h.Sum32()%uint32(len(c.guards))]
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *rangeCache) get(key string, stamp fileStamp) *rangeIndex {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.items[key]
	if e == nil {
		return nil
	}
	item := e.Value.(cachedRange)
	if item.stamp != stamp {
		c.remove(e)
		return nil
	}
	c.lru.MoveToFront(e)
	return item.index
}

func (c *rangeCache) takeScan(key string, stamp fileStamp) *rangeScan {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.items[key]
	if e == nil {
		return nil
	}
	item := e.Value.(cachedRange)
	if item.stamp != stamp {
		c.remove(e)
		return nil
	}
	if item.scan != nil {
		c.remove(e) // Only the active verifier owns/mutates the state now.
	}
	return item.scan
}

func (c *rangeCache) remove(e *list.Element) {
	item := e.Value.(cachedRange)
	delete(c.items, item.key)
	c.bytes -= item.bytes
	c.lru.Remove(e)
}

func (c *rangeCache) put(key string, index *rangeIndex) {
	c.putEntry(cachedRange{key: key, stamp: index.stamp, index: index, bytes: len(index.blocks)*sha256.Size + len(key) + 512})
}

func (c *rangeCache) putScan(key string, scan *rangeScan) {
	c.putEntry(cachedRange{key: key, stamp: scan.stamp, scan: scan, bytes: len(scan.blocks)*sha256.Size + len(key) + 512})
}

func (c *rangeCache) putEntry(item cachedRange) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old := c.items[item.key]; old != nil {
		c.remove(old)
	}
	c.items[item.key] = c.lru.PushFront(item)
	c.bytes += item.bytes
	for c.bytes > rangeCacheBytes || len(c.items) > rangeCacheItems {
		c.remove(c.lru.Back())
	}
}

// OpenVerifiedRange leaves ordinary Open (including dedup admission) unchanged.
// A cache miss verifies the full SHA and derives block digests from those same
// bytes. A hit is valid only for the same kernel file identity/change stamp.
func (p *Originals) OpenVerifiedRange(ctx context.Context, key string) (storage.VerifiedRangeReader, error) {
	bot, name, expected, err := mediaKey(key)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := p.botRoot(bot, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("original must be a regular non-symlink file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = f.Close()
		}
	}()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errOriginalChanged
	}
	if err := rangeFilesystem(f); err != nil {
		return nil, err
	}
	stamp, err := originalStamp(opened)
	if err != nil {
		return nil, err
	}
	// Large indexes and unsupported OSes use the existing streaming full-hash
	// path. This bounds auxiliary memory; it does not lower upload size limits.
	if stamp.size < 0 || stamp.size > int64(rangeIndexLimit/sha256.Size)*rangeBlockBytes {
		return nil, storage.ErrVerifiedRangeUnsupported
	}
	unlock, err := p.ranges.lock(ctx, key)
	if err != nil {
		return nil, err
	}
	defer unlock()
	// Refresh after waiting: another request or an external writer may have run.
	stamp, err = stampFile(f)
	if err != nil {
		return nil, err
	}
	index := p.ranges.get(key, stamp)
	if index == nil {
		var paused *rangeScan
		index, paused, err = p.verifyRangeIndex(ctx, f, stamp, expected, p.ranges.takeScan(key, stamp))
		if paused != nil {
			p.ranges.putScan(key, paused)
		}
		if err != nil {
			return nil, err
		}
		p.ranges.put(key, index)
	}
	success = true
	return &verifiedRange{ctx: ctx, file: f, index: index, hash: expected, block: -1, readAt: p.readAt}, nil
}

func stampFile(f *os.File) (fileStamp, error) {
	info, err := f.Stat()
	if err != nil {
		return fileStamp{}, err
	}
	return originalStamp(info)
}

func (p *Originals) verifyRangeIndex(ctx context.Context, f *os.File, stamp fileStamp, expected string, scan *rangeScan) (*rangeIndex, *rangeScan, error) {
	if stamp.size < 0 || stamp.size > int64(rangeIndexLimit/sha256.Size)*rangeBlockBytes {
		return nil, nil, storage.ErrVerifiedRangeUnsupported
	}
	if scan == nil {
		count := (stamp.size + rangeBlockBytes - 1) / rangeBlockBytes
		scan = &rangeScan{stamp: stamp, blocks: make([][sha256.Size]byte, count), full: sha256.New()}
	}
	buffer := make([]byte, rangeBlockBytes)
	for scan.next < len(scan.blocks) {
		if err := ctx.Err(); err != nil {
			return scan.pause(f, err)
		}
		i := scan.next
		offset := int64(i) * rangeBlockBytes
		chunk := buffer[:min(int64(len(buffer)), stamp.size-offset)]
		if n, err := p.readAt(f, chunk, offset); err != nil || n != len(chunk) {
			return nil, nil, errors.Join(io.ErrUnexpectedEOF, err)
		}
		_, _ = scan.full.Write(chunk)
		scan.blocks[i] = sha256.Sum256(chunk)
		scan.next++
	}
	if hex.EncodeToString(scan.full.Sum(nil)) != expected {
		return nil, nil, errors.New("original content hash mismatch")
	}
	after, err := stampFile(f)
	if err != nil {
		return nil, nil, err
	}
	if after != stamp {
		return nil, nil, errOriginalChanged
	}
	if err := ctx.Err(); err != nil {
		return scan.pause(f, err)
	}
	return &rangeIndex{stamp: stamp, blocks: scan.blocks}, nil, nil
}

func (s *rangeScan) pause(f *os.File, cause error) (*rangeIndex, *rangeScan, error) {
	// Retain only complete reads from an unchanged descriptor. A read error or
	// changed identity discards progress, rather than salvaging uncertain bytes.
	after, err := stampFile(f)
	if err != nil {
		return nil, nil, err
	}
	if after != s.stamp {
		return nil, nil, errOriginalChanged
	}
	if s.next == 0 {
		return nil, nil, cause
	}
	return nil, s, cause
}

type verifiedRange struct {
	ctx    context.Context
	file   *os.File
	index  *rangeIndex
	hash   string
	pos    int64
	block  int64
	buffer []byte
	readAt func(*os.File, []byte, int64) (int, error)
}

// ServeContent can print Seek errors directly for a single range. Keep the
// filesystem cause available to Go callers without exposing a physical path.
type rangeReadError struct{ cause error }

func (rangeReadError) Error() string   { return "original is unavailable" }
func (e rangeReadError) Unwrap() error { return e.cause }

func (r *verifiedRange) VerifiedContentHash() string { return r.hash }

func (r *verifiedRange) unchanged() error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	stamp, err := stampFile(r.file)
	if err != nil {
		return rangeReadError{cause: err}
	}
	if stamp != r.index.stamp {
		return errOriginalChanged
	}
	return nil
}

func (r *verifiedRange) Read(b []byte) (int, error) {
	if err := r.unchanged(); err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, nil
	}
	if r.pos >= r.index.stamp.size {
		return 0, io.EOF
	}
	block := r.pos / rangeBlockBytes
	if block != r.block {
		// A failed fill overwrites the old buffer too. Never let a later seek
		// treat those bytes as the previously verified block.
		r.block = -1
		if r.buffer == nil {
			r.buffer = make([]byte, rangeBlockBytes)
		}
		offset := block * rangeBlockBytes
		chunk := r.buffer[:min(int64(len(r.buffer)), r.index.stamp.size-offset)]
		if n, err := r.readAt(r.file, chunk, offset); err != nil || n != len(chunk) {
			return 0, errors.Join(io.ErrUnexpectedEOF, err)
		}
		if sha256.Sum256(chunk) != r.index.blocks[block] {
			return 0, errors.New("original block integrity check failed")
		}
		if err := r.unchanged(); err != nil {
			return 0, err
		}
		r.block = block
	}
	end := min((block+1)*rangeBlockBytes, r.index.stamp.size)
	n := copy(b, r.buffer[r.pos%rangeBlockBytes:r.pos%rangeBlockBytes+min(int64(len(b)), end-r.pos)])
	r.pos += int64(n)
	return n, nil
}

func (r *verifiedRange) Seek(offset int64, whence int) (int64, error) {
	if err := r.unchanged(); err != nil {
		return 0, err
	}
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = r.pos
	case io.SeekEnd:
		base = r.index.stamp.size
	default:
		return 0, errors.New("invalid seek origin")
	}
	position := base + offset
	if position < 0 || (offset > 0 && position < base) {
		return 0, errors.New("invalid seek offset")
	}
	r.pos = position
	return position, nil
}

func (r *verifiedRange) Close() error {
	r.buffer = nil
	return r.file.Close()
}
