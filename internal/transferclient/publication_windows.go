//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// NativePublication owns retained, private NTFS artifacts inside the existing
// Herdr locator. It is opened/closed per finite invocation, never a service.
// Publication bytes and bounded receipts have their own lifetime; queue Retire
// does not delete them. No arbitrary destination path or overwrite is exposed.
type NativePublication struct {
	queue     *DiskQueue
	root      *os.Root
	guard     *os.File
	metadata  *publicationMetadata
	authorize PublicationAuthorization
	hook      func(string) // Unexported process-crash injection, nil outside tests.
}

func (q *DiskQueue) OpenNativePublication(authorize PublicationAuthorization) (*NativePublication, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return q.openNativePublication(ctx, authorize, false, nil)
}

func (q *DiskQueue) openNativePublication(ctx context.Context, authorize PublicationAuthorization, maintenance bool, hook func(string)) (*NativePublication, error) {
	if q == nil || q.root == nil || authorize == nil {
		return nil, ErrBinding
	}
	if err := makeQueueDir(q.root, publicationDirectory); err != nil {
		return nil, err
	}
	root, err := q.root.OpenRoot(publicationDirectory)
	if err != nil {
		return nil, ErrStore
	}
	p := &NativePublication{queue: q, root: root, authorize: authorize, hook: hook}
	p.metadata = &publicationMetadata{root: root, objectBytes: 0, hook: p.mark}
	good := false
	defer func() {
		if !good {
			_ = p.Close()
		}
	}()
	p.guard, err = guardQueueRoot(root)
	if err != nil {
		return nil, err
	}
	lock, err := p.lock()
	if err != nil {
		return nil, err
	}
	defer unlockClose(lock)
	identity, err := queueObjectIdentity(p.guard)
	if err != nil {
		return nil, err
	}
	version, budget := 2, q.limits.Bytes
	if q.budgetBase != 0 {
		version, budget = 3, q.budgetBase
	}
	want := publicationLocator(identity, q.revision, version, budget)
	flags := os.O_RDWR
	if _, err := root.Lstat("locator"); errors.Is(err, os.ErrNotExist) {
		dir, err := root.Open(".")
		if err != nil {
			return nil, ErrStore
		}
		entries, err := dir.ReadDir(2)
		_ = dir.Close()
		if err != nil && !errors.Is(err, io.EOF) || len(entries) != 1 || entries[0].Name() != "publication.lock" {
			return nil, ErrStore
		}
		flags |= os.O_CREATE | os.O_EXCL
	} else if err != nil {
		return nil, ErrStore
	}
	f, err := openPrivateFile(root, "locator", flags, 0o600)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	actual, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, ErrStore
	}
	if len(actual) == 0 && flags&os.O_EXCL != 0 {
		// Persist the complete empty catalog before accepting locator v2. A
		// later missing bucket/cache must never look like an unused namespace.
		if err := p.metadata.initialize(ctx, true); err != nil {
			return nil, err
		}
		if _, err := f.Write(want); err != nil || f.Sync() != nil {
			return nil, ErrStore
		}
	} else if maintenance {
		if err := p.prepareMigration(f, actual, want); err != nil {
			return nil, err
		}
	} else if !bytes.Equal(actual, want) {
		return nil, ErrVersion
	}
	if syncQueueDir(root) != nil || syncQueueDir(q.root) != nil {
		return nil, ErrStore
	}
	good = true
	return p, nil
}

func (p *NativePublication) Close() error {
	closeQueueGuard(p.guard)
	if p.root == nil {
		return nil
	}
	return p.root.Close()
}

func (p *NativePublication) lock() (*os.File, error) {
	f, err := openPrivateFile(p.root, "publication.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockQueueFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func (p *NativePublication) check(ctx context.Context, ref Reference) error {
	if p == nil || p.authorize == nil || ctx == nil || !validPublicationReference(ref) {
		return ErrBinding
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.authorize(ctx, ref)
}

func (p *NativePublication) read(ctx context.Context, name string) (publicationRecord, *os.File, error) {
	return p.metadata.read(ctx, name)
}

func (p *NativePublication) save(ctx context.Context, f *os.File, r publicationRecord) error {
	return p.metadata.save(ctx, f, r)
}

func (p *NativePublication) record(ctx context.Context, ref Reference, state, fileID string) (publicationRecord, *os.File, error) {
	key := publicationKey(ref)
	r, f, err := p.read(ctx, key+".journal")
	if err != nil {
		return r, nil, err
	}
	if r.Version != 0 {
		if r.Reference != ref {
			_ = f.Close()
			return r, nil, ErrPublicationConflict
		}
		return r, f, nil
	}
	if _, err := p.root.Lstat(key + ".object"); !errors.Is(err, os.ErrNotExist) {
		if f != nil {
			_ = f.Close()
		}
		return r, nil, ErrPublicationConflict
	}
	if err := p.metadata.reserve(ctx, ref, state); err != nil {
		if f != nil {
			_ = f.Close()
		}
		return r, nil, err
	}
	if f == nil {
		f, err = p.metadata.create(ref)
	}
	if err != nil {
		return r, nil, err
	}
	r = publicationRecord{Version: 1, Reference: ref, State: state, FileID: fileID}
	if err := p.save(ctx, f, r); err != nil {
		_ = f.Close()
		return r, nil, err
	}
	return r, f, nil
}

func (p *NativePublication) mark(stage string) {
	if p.hook != nil {
		p.hook(stage)
	}
}

func (p *NativePublication) Publish(ctx context.Context, ref Reference, reader io.Reader) (AssociationOutcome, error) {
	if err := p.check(ctx, ref); err != nil {
		return "", err
	}
	source, ok := reader.(*publicationSource)
	if !ok || !source.active || source.queue != p.queue || source.reference != ref {
		return "", ErrBinding
	}
	lock, err := p.lock()
	if err != nil {
		return "", err
	}
	defer unlockClose(lock)
	identity, err := queueFileIdentity(source.file)
	if err != nil || checkQueueFile(source.file) != nil {
		return "", ErrStore
	}
	actual, err := handlePath(windows.Handle(source.file.Fd()))
	atSource := strings.EqualFold(actual, filepath.Join(p.queue.root.Name(), queueName(ref.DeliveryID+".part")))
	atDestination := strings.EqualFold(actual, filepath.Join(p.root.Name(), publicationKey(ref)+".object"))
	if err != nil || !atSource && !atDestination {
		return "", ErrPublicationConflict
	}
	if err := source.file.Sync(); err != nil {
		return "", ErrStore
	}
	r, f, err := p.record(ctx, ref, "prepared", identity)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if r.State == "fenced" {
		if !atSource {
			return "", ErrPublicationConflict
		}
		return OutcomeCanceled, nil
	}
	if r.FileID != identity {
		return "", ErrPublicationConflict
	}
	if atDestination {
		if r.State == "prepared" {
			if err := p.finish(ctx, source.file, f, r); err != nil {
				return "", err
			}
		}
		return OutcomeCommitted, nil
	}
	if r.State == "published" {
		return "", ErrPublicationConflict
	}
	p.mark("after_prepare")
	if err := p.check(ctx, ref); err != nil {
		return "", err
	}
	if err := renamePublication(source.file, p.root, publicationKey(ref)+".object"); err != nil {
		return "", err
	}
	p.mark("after_rename")
	if err := p.finish(ctx, source.file, f, r); err != nil {
		return "", err
	}
	p.mark("after_commit")
	return OutcomeCommitted, nil
}

func (p *NativePublication) finish(ctx context.Context, object, journal *os.File, r publicationRecord) error {
	if object.Chmod(0o400) != nil || syncQueueDir(p.root) != nil || syncQueueDir(p.queue.root) != nil {
		return ErrStore
	}
	r.State = "published"
	return p.save(ctx, journal, r)
}

func (p *NativePublication) object(ctx context.Context, r publicationRecord) (*os.File, error) {
	f, err := openPrivateFile(p.root, publicationKey(r.Reference)+".object", os.O_RDONLY, 0o400)
	if err != nil {
		return nil, err
	}
	identity, err := queueFileIdentity(f)
	if err != nil || identity != r.FileID {
		_ = f.Close()
		return nil, ErrPublicationConflict
	}
	hash := sha256.New()
	buffer := make([]byte, copyBytes)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		n, err := f.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
			total += int64(n)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = f.Close()
			return nil, ErrStore
		}
	}
	if total != r.Reference.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != r.Reference.ContentHash {
		_ = f.Close()
		return nil, ErrIntegrity
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, ErrStore
	}
	return f, nil
}

func (p *NativePublication) Resolve(ctx context.Context, ref Reference, cancel bool) (AssociationOutcome, error) {
	if err := p.check(ctx, ref); err != nil {
		return "", err
	}
	lock, err := p.lock()
	if err != nil {
		return "", err
	}
	defer unlockClose(lock)
	r, f, err := p.read(ctx, publicationKey(ref)+".journal")
	if err != nil {
		return "", err
	}
	if f != nil {
		defer func() { _ = f.Close() }()
	}
	if r.Version == 0 {
		if _, err := p.root.Lstat(publicationKey(ref) + ".object"); !errors.Is(err, os.ErrNotExist) {
			return "", ErrPublicationConflict
		}
		if !cancel {
			return OutcomeAbsent, nil
		}
		if f != nil {
			_ = f.Close()
		}
		_, f, err := p.record(ctx, ref, "fenced", "")
		if f != nil {
			_ = f.Close()
		}
		if err != nil {
			return "", err
		}
		return OutcomeCanceled, nil
	}
	if r.Reference != ref {
		return "", ErrPublicationConflict
	}
	if r.State == "fenced" {
		if _, err := p.root.Lstat(publicationKey(ref) + ".object"); !errors.Is(err, os.ErrNotExist) {
			return "", ErrPublicationConflict
		}
		return OutcomeCanceled, nil
	}
	if _, err := p.root.Lstat(publicationKey(ref) + ".object"); err == nil {
		object, err := p.object(ctx, r)
		if err != nil {
			return "", err
		}
		defer func() { _ = object.Close() }()
		if err := p.check(ctx, ref); err != nil {
			return "", err
		}
		if r.State == "prepared" {
			if err := p.finish(ctx, object, f, r); err != nil {
				return "", err
			}
		}
		return OutcomeCommitted, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", ErrStore
	}
	if r.State == "published" {
		return "", ErrStore
	} // Missing bytes do not undo an acknowledged publication.
	source, err := p.queue.file(ref.DeliveryID+".part", os.O_RDONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer func() { _ = source.Close() }()
	identity, err := queueFileIdentity(source)
	if err != nil || identity != r.FileID {
		return "", ErrPublicationConflict
	}
	if !cancel {
		return OutcomeAbsent, nil
	} // Prepared but not renamed; same source can retry.
	if err := p.check(ctx, ref); err != nil {
		return "", err
	}
	r.State = "fenced"
	if err := p.save(ctx, f, r); err != nil {
		return "", err
	}
	return OutcomeCanceled, nil
}

// OpenPublished returns a checked, no-write/no-delete-share handle to the host's
// consumer. Only a logical reference enters; no physical destination is returned.
func (p *NativePublication) OpenPublished(ctx context.Context, ref Reference) (io.ReadCloser, error) {
	if err := p.check(ctx, ref); err != nil {
		return nil, err
	}
	lock, err := p.lock()
	if err != nil {
		return nil, err
	}
	defer unlockClose(lock)
	r, f, err := p.read(ctx, publicationKey(ref)+".journal")
	if f != nil {
		_ = f.Close()
	}
	if err != nil {
		return nil, err
	}
	if r.State != "published" || r.Reference != ref {
		return nil, ErrPublicationConflict
	}
	object, err := p.object(ctx, r)
	if err != nil {
		return nil, err
	}
	if err := p.check(ctx, ref); err != nil {
		_ = object.Close()
		return nil, err
	}
	return &publicationReader{file: object}, nil
}

type publicationReader struct{ file *os.File }

func (r *publicationReader) Read(b []byte) (int, error) { return r.file.Read(b) }
func (r *publicationReader) Close() error               { return r.file.Close() }
