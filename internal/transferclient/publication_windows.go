//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	publicationDirectory    = "publications-v1"
	publicationReceipts     = 128
	publicationJournalBytes = 64 << 10
)

type publicationRecord struct {
	Version   int       `json:"version"`
	Reference Reference `json:"reference"`
	State     string    `json:"state"`
	FileID    string    `json:"file_id,omitempty"`
}

// NativePublication owns retained, private NTFS artifacts inside the existing
// Herdr locator. It is opened/closed per finite invocation, never a service.
// Publication bytes and bounded receipts have their own lifetime; queue Retire
// does not delete them. No arbitrary destination path or overwrite is exposed.
type NativePublication struct {
	queue     *DiskQueue
	root      *os.Root
	guard     *os.File
	authorize PublicationAuthorization
	hook      func(string) // Unexported process-crash injection, nil outside tests.
}

func (q *DiskQueue) OpenNativePublication(authorize PublicationAuthorization) (*NativePublication, error) {
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
	p := &NativePublication{queue: q, root: root, authorize: authorize}
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
	want, _ := json.Marshal(struct {
		Version  int    `json:"version"`
		Identity string `json:"identity"`
		Revision string `json:"revision"`
		Bytes    int64  `json:"bytes"`
		Receipts int    `json:"receipts"`
	}{1, identity, q.revision, q.limits.Bytes, publicationReceipts})
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
		if _, err := f.Write(want); err != nil || f.Sync() != nil {
			return nil, ErrStore
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

func publicationKey(ref Reference) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(ref.DeliveryID)))
}

func validPublicationReference(ref Reference) bool {
	return validID(ref.DeliveryID) && validID(ref.OperationID) && validID(ref.BotID) && (Spec{SizeBytes: ref.SizeBytes, SHA256: ref.ContentHash, Mime: ref.Mime}).valid()
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

func (p *NativePublication) read(name string) (publicationRecord, *os.File, error) {
	var record publicationRecord
	if _, err := p.root.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return record, nil, nil
	} else if err != nil {
		return record, nil, ErrStore
	}
	f, err := openPrivateFile(p.root, name, os.O_RDWR, 0o600)
	if err != nil {
		return record, nil, err
	}
	_, err = readJournalFrames(f, publicationJournalBytes, func(body []byte) error {
		var next publicationRecord
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if dec.Decode(&next) != nil || next.Version != 1 || !validPublicationReference(next.Reference) || publicationKey(next.Reference)+".journal" != name || len(next.FileID) > 128 {
			return ErrStore
		}
		if next.State != "prepared" && next.State != "published" && next.State != "fenced" {
			return ErrStore
		}
		if next.State != "fenced" && next.FileID == "" {
			return ErrStore
		}
		if record.Version == 0 {
			if next.State == "published" {
				return ErrStore
			}
		} else if record.State != "prepared" || next.State == "prepared" || next.Reference != record.Reference || next.FileID != record.FileID {
			return ErrStore
		}
		record = next
		return nil
	})
	if err != nil {
		_ = f.Close()
		return record, nil, err
	}
	return record, f, nil // Empty/torn first frame has never authorized rename.
}

func (p *NativePublication) save(f *os.File, record publicationRecord) error {
	body, err := json.Marshal(record)
	if err != nil {
		return ErrStore
	}
	if err := appendJournalFrame(f, body, publicationJournalBytes); err != nil {
		return err
	}
	return syncQueueDir(p.root)
}

func (p *NativePublication) capacity(ref Reference, state string) error {
	dir, err := p.root.Open(".")
	if err != nil {
		return ErrStore
	}
	entries, readErr := dir.ReadDir(publicationReceipts*2 + 3)
	_ = dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(entries) >= publicationReceipts*2+3 {
		return ErrCapacity
	}
	count, total := 0, int64(0)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".journal") {
			continue
		}
		if entry.Name() == publicationKey(ref)+".journal" {
			continue
		}
		r, f, err := p.read(entry.Name())
		if f != nil {
			_ = f.Close()
		}
		if err != nil || r.Version == 0 {
			return ErrStore
		}
		count++
		if r.State != "fenced" {
			if r.Reference.SizeBytes > p.queue.limits.Bytes-total {
				return ErrCapacity
			}
			total += r.Reference.SizeBytes
		}
	}
	if count >= publicationReceipts {
		return ErrCapacity
	}
	if state != "fenced" && ref.SizeBytes > p.queue.limits.Bytes-total {
		return ErrCapacity
	}
	return queueFreeSpace(p.root, publicationJournalBytes) // Rename reuses admitted bytes.
}

func (p *NativePublication) record(ref Reference, state, fileID string) (publicationRecord, *os.File, error) {
	key := publicationKey(ref)
	r, f, err := p.read(key + ".journal")
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
	if err := p.capacity(ref, state); err != nil {
		if f != nil {
			_ = f.Close()
		}
		return r, nil, err
	}
	if f == nil {
		f, err = openPrivateFile(p.root, key+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	}
	if err != nil {
		return r, nil, err
	}
	r = publicationRecord{Version: 1, Reference: ref, State: state, FileID: fileID}
	if err := p.save(f, r); err != nil {
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
	r, f, err := p.record(ref, "prepared", identity)
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
			if err := p.finish(source.file, f, r); err != nil {
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
	if err := p.finish(source.file, f, r); err != nil {
		return "", err
	}
	p.mark("after_commit")
	return OutcomeCommitted, nil
}

func (p *NativePublication) finish(object, journal *os.File, r publicationRecord) error {
	if object.Chmod(0o400) != nil || syncQueueDir(p.root) != nil || syncQueueDir(p.queue.root) != nil {
		return ErrStore
	}
	r.State = "published"
	return p.save(journal, r)
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
	r, f, err := p.read(publicationKey(ref) + ".journal")
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
		_, f, err := p.record(ref, "fenced", "")
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
			if err := p.finish(object, f, r); err != nil {
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
	if err := p.save(f, r); err != nil {
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
	r, f, err := p.read(publicationKey(ref) + ".journal")
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
