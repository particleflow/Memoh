package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	UploadChunkBytes  int64 = 8 << 20
	UploadTTL               = 24 * time.Hour
	maxUploadSessions       = 128
	maxActiveUploads        = 16
	maxOwnerUploads         = 4
)

var (
	ErrUploadNotFound = errors.New("upload not found")
	ErrUploadConflict = errors.New("upload state conflict")
	ErrUploadInvalid  = errors.New("invalid upload")
	ErrUploadQuota    = errors.New("upload quota exceeded")
	ErrUploadBusy     = errors.New("upload busy")
)

// UploadInput deliberately accepts no filesystem path or workspace reference.
type UploadInput struct {
	SizeBytes int64  `json:"size_bytes" validate:"required"`
	SHA256    string `json:"sha256" validate:"required"`
	Mime      string `json:"mime" validate:"required"`
}

// Upload is a durable receipt. ContentHash is exposed only after publication.
type Upload struct {
	ID          string    `json:"id" validate:"required"`
	State       string    `json:"state" validate:"required" enums:"uploading,ready,canceled"`
	Offset      int64     `json:"offset" validate:"required"`
	SizeBytes   int64     `json:"size_bytes" validate:"required"`
	ExpiresAt   time.Time `json:"expires_at" validate:"required"`
	ContentHash string    `json:"content_hash,omitempty"`
	Mime        string    `json:"mime" validate:"required"`
}

type uploadRecord struct {
	Upload
	Owner  string `json:"owner"`
	Bot    string `json:"bot"`
	SHA256 string `json:"sha256" validate:"required"`
}

// UploadStore journals acknowledged offsets beneath the configured originals
// root. It never deletes or indexes originals. Fixed lock stripes survive GC;
// advisory locks serialize writers across processes sharing a local filesystem.
type UploadStore struct {
	root     *os.Root
	media    *Service
	now      func() time.Time
	syncFile func(*os.File) error
}

func (s *Service) Uploads() *UploadStore { return s.uploads }

// EnableUploads is only used for the independently persistent originals store.
// The operator-owned root must support atomic rename, file/directory fsync and
// advisory locks. Multiple servers must share this same root and policy.
func (s *Service) EnableUploads(originalsRoot string) error {
	if err := os.MkdirAll(originalsRoot, 0o750); err != nil {
		return err
	}
	// Persist any directories created by MkdirAll before acknowledging a journal.
	absolute, err := filepath.Abs(originalsRoot)
	if err != nil {
		return err
	}
	for directory := filepath.Dir(absolute); ; directory = filepath.Dir(directory) {
		ancestor, err := os.OpenRoot(directory)
		if err != nil {
			return err
		}
		err = syncRootWith(ancestor, (*os.File).Sync)
		_ = ancestor.Close()
		if err != nil {
			return err
		}
		if directory == filepath.Dir(directory) {
			break
		}
	}
	parent, err := os.OpenRoot(originalsRoot)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	if err := parent.Mkdir(".uploads", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := parent.Lstat(".uploads")
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUploadInvalid
	}
	root, err := parent.OpenRoot(".uploads")
	if err != nil {
		return err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = root.Close()
		return ErrUploadInvalid
	}
	if err := syncRootWith(parent, (*os.File).Sync); err != nil {
		_ = root.Close()
		return err
	}
	store := &UploadStore{root: root, media: s, now: time.Now, syncFile: (*os.File).Sync}
	unlock, err := store.lock(context.Background(), "admission")
	if err != nil && !errors.Is(err, ErrUploadBusy) {
		_ = root.Close()
		return err
	}
	if err == nil {
		unlock()
	}
	s.uploads = store
	return nil
}

func (s *UploadStore) Close() error { return s.root.Close() }

func validUploadID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

func validSHA(hash string) bool {
	if len(hash) != sha256.Size*2 {
		return false
	}
	b, err := hex.DecodeString(hash)
	return err == nil && len(b) == sha256.Size && hash == strings.ToLower(hash)
}

func syncRootWith(root *os.Root, syncFile func(*os.File) error) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return syncFile(f)
}

func (s *UploadStore) read(id string) (uploadRecord, error) {
	var r uploadRecord
	f, err := openUploadFile(s.root, id+".json", os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return r, ErrUploadNotFound
	}
	if err != nil {
		return r, err
	}
	defer func() { _ = f.Close() }()
	if err := json.NewDecoder(io.LimitReader(f, 4096)).Decode(&r); err != nil {
		return r, err
	}
	if r.ID != id || r.SizeBytes <= 0 || r.Offset < 0 || r.Offset > r.SizeBytes || !validSHA(r.SHA256) {
		return r, ErrUploadInvalid
	}
	// A prior process may have renamed this journal and failed before its
	// directory fsync. Repair that barrier before GET/ready retry can acknowledge
	// it, or Sweep can remove the payload on the strength of a terminal state.
	if err := s.syncFile(f); err != nil {
		return uploadRecord{}, err
	}
	if err := syncRootWith(s.root, s.syncFile); err != nil {
		return uploadRecord{}, err
	}
	return r, nil
}

func (s *UploadStore) save(r uploadRecord) error {
	name := r.ID + ".tmp"
	if err := s.remove(name); err != nil {
		return err
	}
	f, err := openUploadFile(s.root, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = s.root.Remove(name) }()
	if err := json.NewEncoder(f).Encode(r); err != nil {
		return err
	}
	if err := s.syncFile(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := s.root.Rename(name, r.ID+".json"); err != nil {
		return err
	}
	return syncRootWith(s.root, s.syncFile)
}

func (s *UploadStore) remove(name string) error {
	err := s.root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *UploadStore) owned(owner, bot, id string) (uploadRecord, error) {
	r, err := s.read(id)
	if err != nil {
		return r, err
	}
	if r.Owner != owner || r.Bot != bot || !s.now().Before(r.ExpiresAt) {
		return r, ErrUploadNotFound
	}
	return r, nil
}

func (s *UploadStore) Create(ctx context.Context, owner, bot string, in UploadInput) (Upload, error) {
	if !validPathComponent(owner) || !validPathComponent(bot) || in.SizeBytes <= 0 || !validSHA(in.SHA256) {
		return Upload{}, ErrUploadInvalid
	}
	if in.SizeBytes > s.media.MaxUploadBytes() {
		return Upload{}, ErrAssetTooLarge
	}
	typ, _, err := mime.ParseMediaType(in.Mime)
	if err != nil || len(typ) > 255 || !strings.Contains(typ, "/") {
		return Upload{}, ErrUploadInvalid
	}
	unlock, err := s.lock(ctx, "admission")
	if err != nil {
		return Upload{}, err
	}
	defer unlock()
	if err := s.sweep(ctx); err != nil {
		return Upload{}, err
	}
	entries, err := s.entries()
	if err != nil {
		return Upload{}, err
	}
	var count, active, owned int
	var reserved int64
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		count++
		r, err := s.read(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return Upload{}, err
		}
		if r.State == "uploading" {
			active++
			reserved += r.SizeBytes
			if r.Owner == owner {
				owned++
			}
		}
	}
	if count >= maxUploadSessions || active >= maxActiveUploads || owned >= maxOwnerUploads || reserved+in.SizeBytes > 4*s.media.MaxUploadBytes() {
		return Upload{}, ErrUploadQuota
	}
	r := uploadRecord{Upload: Upload{ID: uuid.NewString(), State: "uploading", SizeBytes: in.SizeBytes, Mime: typ, ExpiresAt: s.now().Add(UploadTTL)}, Owner: owner, Bot: bot, SHA256: in.SHA256}
	if err := s.save(r); err != nil {
		return Upload{}, err
	}
	return r.Upload, nil
}

func (s *UploadStore) Get(ctx context.Context, owner, bot, id string) (Upload, error) {
	unlock, err := s.lock(ctx, id)
	if err != nil {
		return Upload{}, err
	}
	defer unlock()
	r, err := s.owned(owner, bot, id)
	if err != nil {
		return Upload{}, err
	}
	return r.Upload, nil
}

// Append acknowledges only exact, verified and synced bytes. On interruption,
// unacknowledged tail bytes are truncated on the next append or completion.
func (s *UploadStore) Append(ctx context.Context, owner, bot, id string, offset, size int64, hash string, body io.Reader) (Upload, error) {
	if size <= 0 || size > UploadChunkBytes || offset < 0 || !validSHA(hash) || body == nil {
		return Upload{}, ErrUploadInvalid
	}
	unlock, err := s.lock(ctx, id)
	if err != nil {
		return Upload{}, err
	}
	defer unlock()
	r, err := s.owned(owner, bot, id)
	if err != nil {
		return Upload{}, err
	}
	if r.State != "uploading" || offset != r.Offset {
		return Upload{}, ErrUploadConflict
	}
	if size > r.SizeBytes-r.Offset {
		return Upload{}, ErrAssetTooLarge
	}
	f, err := s.payload(r)
	if err != nil {
		return Upload{}, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(r.Offset, io.SeekStart); err != nil {
		return Upload{}, err
	}
	h := sha256.New()
	// Roll back a rejected/interrupted body promptly. A process death may bypass
	// this defer, so payload() also repairs the tail from the durable journal.
	acknowledged := false
	defer func() {
		if !acknowledged {
			_ = f.Truncate(r.Offset)
		}
	}()
	n, err := io.CopyBuffer(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, body}, size+1), make([]byte, 256<<10))
	if err != nil {
		return Upload{}, err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != hash {
		return Upload{}, ErrUploadInvalid
	}
	if err := s.syncFile(f); err != nil {
		return Upload{}, err
	}
	if err := ctx.Err(); err != nil {
		return Upload{}, err
	}
	// Once saving begins its outcome may be uncertain (rename then fsync).
	// Leave the bytes intact; the next operation consults the journal.
	acknowledged = true
	r.Offset += size
	if err := s.save(r); err != nil {
		return Upload{}, err
	}
	return r.Upload, nil
}

func (s *UploadStore) payload(r uploadRecord) (*os.File, error) {
	f, err := openUploadFile(s.root, r.ID+".data", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.Size() < r.Offset {
		_ = f.Close()
		return nil, ErrUploadConflict
	}
	if err := f.Truncate(r.Offset); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func (s *UploadStore) Complete(ctx context.Context, owner, bot, id string) (Upload, error) {
	unlock, err := s.lock(ctx, id)
	if err != nil {
		return Upload{}, err
	}
	defer unlock()
	r, err := s.owned(owner, bot, id)
	if err != nil {
		return Upload{}, err
	}
	if r.State == "ready" {
		return r.Upload, nil
	}
	if r.State != "uploading" || r.Offset != r.SizeBytes {
		return Upload{}, ErrUploadConflict
	}
	f, err := s.payload(r)
	if err != nil {
		return Upload{}, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.CopyBuffer(h, contextReader{ctx, f}, make([]byte, 256<<10))
	if err != nil {
		return Upload{}, err
	}
	if n != r.SizeBytes || hex.EncodeToString(h.Sum(nil)) != r.SHA256 {
		return Upload{}, ErrUploadInvalid
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Upload{}, err
	}
	// Reuse the canonical media publication and deduplication path, without a
	// second whole-file spool. The durable provider also verifies before rename.
	_, err = s.media.persist(ctx, IngestInput{BotID: bot, Mime: r.Mime}, r.SHA256, r.SizeBytes, f)
	if err != nil {
		return Upload{}, err
	}
	r.State = "ready"
	r.ContentHash = r.SHA256
	if err := s.save(r); err != nil {
		return Upload{}, err
	}
	if err := s.remove(id + ".data"); err != nil {
		return Upload{}, err
	}
	if err := syncRootWith(s.root, s.syncFile); err != nil {
		return Upload{}, err
	}
	return r.Upload, nil
}

func (s *UploadStore) Cancel(ctx context.Context, owner, bot, id string) (Upload, error) {
	unlock, err := s.lock(ctx, id)
	if err != nil {
		return Upload{}, err
	}
	defer unlock()
	r, err := s.owned(owner, bot, id)
	if err != nil {
		return Upload{}, err
	}
	if r.State == "ready" {
		return Upload{}, ErrUploadConflict
	}
	r.State = "canceled"
	if err := s.save(r); err != nil {
		return Upload{}, err
	}
	if err := s.remove(id + ".data"); err != nil {
		return Upload{}, err
	}
	if err := syncRootWith(s.root, s.syncFile); err != nil {
		return Upload{}, err
	}
	return r.Upload, nil
}

func (s *UploadStore) entries() ([]os.DirEntry, error) {
	f, err := s.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return f.ReadDir(-1) // bounded by admission: 128 receipts plus payloads/lock stripes
}

func (s *UploadStore) Sweep(ctx context.Context) error {
	unlock, err := s.lock(ctx, "admission")
	if err != nil {
		return err
	}
	defer unlock()
	return s.sweep(ctx)
}

func (s *UploadStore) sweep(ctx context.Context) error {
	entries, err := s.entries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			id := strings.TrimSuffix(entry.Name(), ".tmp")
			if !validUploadID(id) {
				continue
			}
			unlock, err := s.lock(ctx, id)
			if errors.Is(err, ErrUploadBusy) {
				continue
			}
			if err != nil {
				return err
			}
			err = s.remove(entry.Name())
			unlock()
			if err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		unlock, err := s.lock(ctx, id)
		if errors.Is(err, ErrUploadBusy) {
			continue
		}
		if err != nil {
			return err
		}
		err = func() error {
			defer unlock()
			r, err := s.read(id)
			if err != nil {
				return err
			}
			if s.now().Before(r.ExpiresAt) {
				if r.State == "ready" || r.State == "canceled" {
					return s.remove(id + ".data")
				}
				return nil
			}
			for _, suffix := range []string{".data", ".tmp", ".json"} {
				if err := s.remove(id + suffix); err != nil {
					return err
				}
			}
			return syncRootWith(s.root, s.syncFile)
		}()
		if err != nil {
			return err
		}
	}
	return syncRootWith(s.root, s.syncFile)
}
