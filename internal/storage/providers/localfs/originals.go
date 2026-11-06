package localfs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/felinics/memoh/internal/storage"
)

// Originals stores verified originals in independent, per-bot directory roots.
// The configured root is operator-owned and must never be mounted in workspaces.
type Originals struct {
	root     string
	syncFile func(*os.File) error
	ranges   *rangeCache
	readAt   func(*os.File, []byte, int64) (int, error)

	prefixMu sync.Mutex
	synced   map[string]bool
}

func NewOriginals(root string) *Originals {
	return &Originals{root: root, syncFile: (*os.File).Sync, ranges: newRangeCache(), readAt: (*os.File).ReadAt, synced: make(map[string]bool)}
}

func (p *Originals) prefixSynced(bot, dir string) bool {
	p.prefixMu.Lock()
	defer p.prefixMu.Unlock()
	return p.synced[bot+"/"+dir]
}

func (p *Originals) markPrefixSynced(bot, dir string) {
	p.prefixMu.Lock()
	defer p.prefixMu.Unlock()
	p.synced[bot+"/"+dir] = true
}

func mediaKey(key string) (bot, name, hash string, err error) {
	parts := strings.Split(key, "/")
	if len(parts) != 3 || !validComponent(parts[0]) {
		return "", "", "", errors.New("invalid media key")
	}
	hash = strings.TrimSuffix(parts[2], path.Ext(parts[2]))
	decoded, decodeErr := hex.DecodeString(hash)
	if decodeErr != nil || len(decoded) != sha256.Size || hash != strings.ToLower(hash) ||
		parts[1] != hash[:2] || !validComponent(parts[2]) {
		return "", "", "", errors.New("invalid media hash key")
	}
	return parts[0], parts[1] + "/" + parts[2], hash, nil
}

func validComponent(value string) bool {
	return value != "" && value != "." && value != ".." &&
		!strings.ContainsAny(value, "/\\\x00")
}

func (p *Originals) syncDirectory(root *os.Root, name string) error {
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return p.syncFile(dir)
}

// mkdirOriginalsRoot persists the configured root's ancestry before adding data.
// Resync existing ancestors too: a failed attempt may have left unsynced entries.
func (p *Originals) mkdirOriginalsRoot(dir string) error {
	if dir == "" {
		return errors.New("media root is required")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	var parents []string
	for parent := filepath.Dir(dir); ; parent = filepath.Dir(parent) {
		parents = append(parents, parent)
		if parent == filepath.Dir(parent) {
			break
		}
	}
	for i := len(parents) - 1; i >= 0; i-- {
		parent, err := os.OpenRoot(parents[i])
		if err != nil {
			return err
		}
		err = p.syncDirectory(parent, ".")
		_ = parent.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Originals) botRoot(bot string, create bool) (*os.Root, error) {
	if create {
		if err := p.mkdirOriginalsRoot(p.root); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(p.root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	if create {
		if err := root.Mkdir(bot, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := p.syncDirectory(root, "."); err != nil {
			return nil, err
		}
	}
	info, err := root.Lstat(bot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("media bot directory is not a directory")
	}
	botRoot, err := root.OpenRoot(bot)
	if err != nil {
		return nil, err
	}
	// Confirm we anchored the directory inspected above, even if renamed during
	// open. Subsequent operations cannot follow links out of this bot's root.
	opened, err := botRoot.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = botRoot.Close()
		return nil, errors.New("media bot directory changed during open")
	}
	return botRoot, nil
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(b)
}

func (p *Originals) Put(ctx context.Context, key string, reader io.Reader) error {
	bot, name, expected, err := mediaKey(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := p.botRoot(bot, true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	dir := path.Dir(name)
	// Persist the prefix directory entry when this call creates it, and once
	// per process for a pre-existing entry: a prior publication may have died
	// between its Mkdir and this sync. Later puts into the same prefix skip
	// the repeat fsync.
	mkdirErr := root.Mkdir(dir, 0o750)
	if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
		return mkdirErr
	}
	if mkdirErr == nil || !p.prefixSynced(bot, dir) {
		if err := p.syncDirectory(root, "."); err != nil {
			return err
		}
		p.markPrefixSynced(bot, dir)
	}
	temp := dir + "/.pending-" + rand.Text()
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = root.Remove(temp) }()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), contextReader{ctx, reader}); err != nil {
		return fmt.Errorf("write media: %w", err)
	}
	if hex.EncodeToString(h.Sum(nil)) != expected {
		return storage.ErrContentMismatch
	}
	if err := p.syncFile(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Same-directory rename publishes only complete, verified bytes. It replaces
	// a final symlink itself rather than writing through that symlink.
	if err := root.Rename(temp, name); err != nil {
		return err
	}
	return p.syncDirectory(root, dir)
}

// EnsureDurable completes file and directory barriers after an interrupted
// publication. Reopening/verifying a hash alone cannot establish this guarantee.
func (p *Originals) EnsureDurable(ctx context.Context, key string) error {
	bot, name, _, err := mediaKey(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// This also syncs the configured root's ancestry and the Bot entry.
	root, err := p.botRoot(bot, true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("media object is not a regular file")
	}
	if err := p.syncFile(f); err != nil {
		return err
	}
	if err := p.syncDirectory(root, path.Dir(name)); err != nil {
		return err
	}
	if err := p.syncDirectory(root, "."); err != nil {
		return err
	}
	return ctx.Err()
}

func (p *Originals) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	bot, name, expected, err := mediaKey(key)
	if err != nil {
		return nil, err
	}
	root, err := p.botRoot(bot, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (io.ReadCloser, error) { _ = f.Close(); return nil, err }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(errors.New("media object is not a regular file"))
	}
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx, f}); err != nil {
		return fail(err)
	}
	if hex.EncodeToString(h.Sum(nil)) != expected {
		return fail(storage.ErrContentMismatch)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	return f, nil
}

func (p *Originals) Delete(ctx context.Context, key string) error {
	bot, name, _, err := mediaKey(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := p.botRoot(bot, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	err = root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Host paths are never exposed to workspace consumers.
func (*Originals) AccessPath(context.Context, string) string { return "" }

func (p *Originals) ListPrefix(ctx context.Context, prefix string) ([]string, error) {
	bot, name, _, err := mediaKey(prefix + ".bin")
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
	dir, err := root.Open(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), path.Base(prefix)+".") {
			continue
		}
		key := path.Join(bot, path.Dir(name), e.Name())
		if _, _, _, err := mediaKey(key); err == nil {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// ListPrefixAcrossBots answers which Bot directories hold a storage-key
// prefix. Bot roots are walked in name order, so the first listed owner is
// deterministic. Like ListPrefix this is metadata-only: no payload bytes are
// read and nothing moves between Bot directories.
func (p *Originals) ListPrefixAcrossBots(ctx context.Context, storageKeyPrefix string) ([]string, error) {
	if !validStorageKeyPrefix(storageKeyPrefix) {
		return nil, errors.New("invalid storage key prefix")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(p.root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	top, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { _ = top.Close() }()
	entries, err := top.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	bots := make([]string, 0, len(entries))
	for _, e := range entries {
		// Only real Bot directories are candidates; the per-Bot listing's own
		// key validation still applies to each.
		if !e.IsDir() || !validComponent(e.Name()) {
			continue
		}
		bots = append(bots, e.Name())
	}
	slices.Sort(bots)
	var keys []string
	for _, bot := range bots {
		botKeys, err := p.ListPrefix(ctx, path.Join(bot, storageKeyPrefix))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		keys = append(keys, botKeys...)
	}
	return keys, nil
}

// validStorageKeyPrefix accepts the Bot-independent half of a routing key:
// "<hash prefix>/<hash>", without an extension — the same ext-less prefix the
// per-Bot ListPrefix consumes.
func validStorageKeyPrefix(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if !validComponent(part) || strings.Contains(part, ".") {
			return false
		}
	}
	return true
}
