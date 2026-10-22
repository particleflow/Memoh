package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
)

// ImageTaskReceipt is the publication journal for one image request. It stores
// neither prompts nor provider credentials/response bodies. Images use UploadStore.
type ImageTaskReceipt struct {
	ID          string   `json:"id"`
	Scope       string   `json:"scope"`
	Fingerprint string   `json:"fingerprint"`
	State       string   `json:"state"`
	UploadID    string   `json:"upload_id,omitempty"`
	Asset       *Asset   `json:"asset,omitempty"`
	InputRefs   []string `json:"input_refs,omitempty"`
}

// ImageTaskJournal reuses the upload store's confined files, advisory locks and
// durability barriers. It has no scheduler, goroutine or execution credentials.
// Unsettled claims are never expired or removed automatically.
type ImageTaskJournal struct{ files *UploadStore }

func OpenImageTaskJournal(originalsRoot string) (*ImageTaskJournal, error) {
	parent, err := os.OpenRoot(originalsRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	const directory = ".image-tasks"
	if err := parent.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := parent.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUploadInvalid
	}
	root, err := parent.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = root.Close()
		return nil, ErrUploadInvalid
	}
	if err := syncRootWith(parent, (*os.File).Sync); err != nil {
		_ = root.Close()
		return nil, err
	}
	return &ImageTaskJournal{files: &UploadStore{root: root, syncFile: (*os.File).Sync}}, nil
}

func (s *ImageTaskJournal) Close() error { return s.files.root.Close() }

func imageJournalKey(scope, id string) string {
	h := sha256.Sum256([]byte(scope + "\x00" + id))
	return hex.EncodeToString(h[:])
}

func imageJournalLock(scope string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(scope)).String()
}

func (s *ImageTaskJournal) read(name string) (ImageTaskReceipt, error) {
	var r ImageTaskReceipt
	f, err := openUploadFile(s.files.root, name, os.O_RDONLY, 0)
	if err != nil {
		return r, err
	}
	defer func() { _ = f.Close() }()
	d := json.NewDecoder(io.LimitReader(f, 8192))
	if err = d.Decode(&r); err != nil {
		return r, err
	}
	if r.ID == "" || r.Scope == "" || !validSHA(r.Fingerprint) || r.State == "" {
		return r, ErrUploadInvalid
	}
	// Repair the visible-rename/directory-fsync window before acknowledging.
	if err := s.files.syncFile(f); err != nil {
		return r, err
	}
	return r, syncRootWith(s.files.root, s.files.syncFile)
}

func (s *ImageTaskJournal) save(name string, r ImageTaskReceipt) error {
	data, err := json.Marshal(r)
	if err != nil || len(data) > 8192 {
		return ErrUploadInvalid
	}
	f, err := openUploadFile(s.files.root, name+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = s.files.syncFile(f)
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := s.files.root.Rename(name+".tmp", name); err != nil {
		return err
	}
	return syncRootWith(s.files.root, s.files.syncFile)
}

// Claim commits dispatch intent before any provider call. Another unsettled
// request in the same trusted scope blocks a different task, even with a new key.
func (s *ImageTaskJournal) Claim(ctx context.Context, wanted ImageTaskReceipt) (ImageTaskReceipt, bool, error) {
	if wanted.Scope == "" || wanted.ID == "" || !validSHA(wanted.Fingerprint) {
		return ImageTaskReceipt{}, false, ErrUploadInvalid
	}
	prefix := imageJournalKey(wanted.Scope, "")
	unlock, err := s.files.lock(ctx, imageJournalLock(wanted.Scope))
	if err != nil {
		return ImageTaskReceipt{}, false, err
	}
	defer unlock()
	name := prefix + "." + imageJournalKey(wanted.Scope, wanted.ID) + ".json"
	existing, err := s.read(name)
	if err == nil {
		if existing.Scope != wanted.Scope || existing.ID != wanted.ID || existing.Fingerprint != wanted.Fingerprint {
			return ImageTaskReceipt{}, false, ErrUploadConflict
		}
		return existing, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ImageTaskReceipt{}, false, err
	}
	dir, err := s.files.root.Open(".")
	if err != nil {
		return ImageTaskReceipt{}, false, err
	}
	defer func() { _ = dir.Close() }()
	count := 0
	for {
		entries, readErr := dir.ReadDir(64)
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), prefix+".") || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			count++
			if count >= 1024 {
				return ImageTaskReceipt{}, false, ErrUploadQuota
			}
			r, err := s.read(entry.Name())
			if err != nil {
				return ImageTaskReceipt{}, false, err
			}
			if r.State != "ready" && r.State != "rejected" {
				return r, false, ErrUploadBusy
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return ImageTaskReceipt{}, false, readErr
		}
	}
	wanted.State = "dispatching"
	return wanted, true, s.save(name, wanted)
}

func (s *ImageTaskJournal) Read(ctx context.Context, scope, id string) (ImageTaskReceipt, error) {
	unlock, err := s.files.lock(ctx, imageJournalLock(scope))
	if err != nil {
		return ImageTaskReceipt{}, err
	}
	defer unlock()
	r, err := s.read(imageJournalKey(scope, "") + "." + imageJournalKey(scope, id) + ".json")
	if err != nil || r.Scope != scope || r.ID != id {
		return ImageTaskReceipt{}, ErrUploadNotFound
	}
	return r, nil
}

// Save uses the prior state as a CAS. Concurrent recovery may publish the same
// staged bytes, but cannot replace another request or regress a ready receipt.
func (s *ImageTaskJournal) Save(ctx context.Context, expected string, r ImageTaskReceipt) error {
	unlock, err := s.files.lock(ctx, imageJournalLock(r.Scope))
	if err != nil {
		return err
	}
	defer unlock()
	name := imageJournalKey(r.Scope, "") + "." + imageJournalKey(r.Scope, r.ID) + ".json"
	old, err := s.read(name)
	if err != nil {
		return err
	}
	if old.Scope != r.Scope || old.ID != r.ID || old.Fingerprint != r.Fingerprint || old.State != expected {
		return ErrUploadConflict
	}
	return s.save(name, r)
}
