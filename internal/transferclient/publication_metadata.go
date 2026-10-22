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
	"strings"
)

// Versioned defaults in the existing publication locator, not a second config
// root. Objects retain the queue's separately accounted, immutable byte budget.
const (
	publicationDirectory         = "publications-v1"
	publicationReceipts          = 128 // Per bucket in locator v2; global only in legacy v1.
	publicationBuckets           = 64
	publicationJournalBytes      = 64 << 10
	publicationBucketBytes       = publicationReceipts * publicationJournalBytes
	publicationMetadataDirectory = "records-v2"
	publicationMigrationBatch    = 16
)

type publicationRecord struct {
	Version   int       `json:"version"`
	Reference Reference `json:"reference"`
	State     string    `json:"state"`
	FileID    string    `json:"file_id,omitempty"`
}

func publicationKey(ref Reference) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(ref.DeliveryID)))
}

func validPublicationReference(ref Reference) bool {
	return validID(ref.DeliveryID) && validID(ref.OperationID) && validID(ref.BotID) && (Spec{SizeBytes: ref.SizeBytes, SHA256: ref.ContentHash, Mime: ref.Mime}).valid()
}

func publicationBucket(name string) (int, error) {
	key, ok := strings.CutSuffix(name, ".journal")
	if !ok || len(key) != 64 {
		return 0, ErrInvalid
	}
	raw, err := hex.DecodeString(key)
	if err != nil || hex.EncodeToString(raw) != key {
		return 0, ErrInvalid
	}
	return int(raw[0]) & (publicationBuckets - 1), nil
}

// This is private implementation of NativePublication, not a task registry.
// Journals remain authoritative. Usage is a bounded, rebuildable per-bucket
// cache; a durable dirty marker ALWAYS precedes mutations of that bucket.
// Every method is called while holding the original publication.lock.
type publicationMetadata struct {
	root        *os.Root
	objectBytes int64
	hook        func(string)
}

type publicationUsage struct {
	Version  int    `json:"version"`
	Identity string `json:"identity"`
	Count    int    `json:"count"`
	Bytes    int64  `json:"bytes"`
	Metadata int64  `json:"metadata"`
}

func (m *publicationMetadata) mark(stage string) {
	if m.hook != nil {
		m.hook(stage)
	}
}

func (m *publicationMetadata) bucket(index int, create bool) (*os.Root, func(), error) {
	if index < 0 || index >= publicationBuckets {
		return nil, nil, ErrInvalid
	}
	parent := m.root
	var roots []*os.Root
	var guards []*os.File
	closeAll := func() {
		for i := len(roots) - 1; i >= 0; i-- {
			closeQueueGuard(guards[i])
			_ = roots[i].Close()
		}
	}
	for _, name := range []string{publicationMetadataDirectory, fmt.Sprintf("%02x", index)} {
		if create {
			if err := makeQueueDir(parent, name); err != nil && !errors.Is(err, os.ErrExist) {
				closeAll()
				return nil, nil, err
			}
		}
		st, err := parent.Lstat(name)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			closeAll()
			return nil, nil, ErrStore
		}
		root, err := parent.OpenRoot(name)
		if err != nil {
			closeAll()
			return nil, nil, ErrStore
		}
		if err := checkQueueRoot(root); err != nil {
			_ = root.Close()
			closeAll()
			return nil, nil, err
		}
		guard, err := guardQueueRoot(root)
		if err != nil {
			_ = root.Close()
			closeAll()
			return nil, nil, err
		}
		roots = append(roots, root)
		guards = append(guards, guard)
		if create && syncQueueDir(parent) != nil {
			closeAll()
			return nil, nil, ErrStore
		}
		parent = root
	}
	return parent, closeAll, nil
}

func publicationRootIdentity(root *os.Root) (string, error) {
	f, err := root.Open(".")
	if err != nil {
		return "", ErrStore
	}
	defer func() { _ = f.Close() }()
	return queueObjectIdentity(f)
}

func readPublicationJournal(root *os.Root, name string, repair bool) (publicationRecord, *os.File, error) {
	var record publicationRecord
	f, err := openPrivateFile(root, name, os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return record, nil, nil
	}
	if err != nil {
		return record, nil, err
	}
	_, err = readJournalFramesMode(f, publicationJournalBytes, repair, func(body []byte) error {
		var next publicationRecord
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if dec.Decode(&next) != nil || dec.Decode(new(any)) != io.EOF || next.Version != 1 || !validPublicationReference(next.Reference) || publicationKey(next.Reference)+".journal" != name || len(next.FileID) > 128 {
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
	if err != nil || record.Version == 0 {
		_ = f.Close()
		return record, nil, ErrStore
	}
	return record, f, nil
}

func (m *publicationMetadata) dirty(root *os.Root) error {
	f, err := openPrivateFile(root, "usage.dirty", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err == nil && st.Size() != 0 {
		err = ErrStore
	}
	if err == nil {
		err = f.Sync()
	}
	_ = f.Close()
	if err != nil || syncQueueDir(root) != nil {
		return ErrStore
	}
	m.mark("metadata_after_dirty")
	return nil
}

func (m *publicationMetadata) census(ctx context.Context, root *os.Root, index int) (publicationUsage, error) {
	identity, err := publicationRootIdentity(root)
	if err != nil {
		return publicationUsage{}, err
	}
	usage := publicationUsage{Version: 1, Identity: identity}
	dir, err := root.Open(".")
	if err != nil {
		return usage, ErrStore
	}
	entries, readErr := dir.ReadDir(publicationReceipts + 3)
	_ = dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(entries) > publicationReceipts+2 {
		return usage, ErrCapacity
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return usage, err
		}
		if e.Name() == "usage" || e.Name() == "usage.dirty" {
			continue
		}
		bucket, err := publicationBucket(e.Name())
		if err != nil || bucket != index || !e.Type().IsRegular() {
			return usage, ErrStore
		}
		r, f, err := readPublicationJournal(root, e.Name(), true)
		if err != nil {
			return usage, err
		}
		if f == nil {
			return usage, ErrStore
		}
		st, err := f.Stat()
		_ = f.Close()
		if err != nil {
			return usage, ErrStore
		}
		usage.Count++
		usage.Metadata += st.Size()
		if r.State != "fenced" {
			if r.Reference.SizeBytes > m.objectBytes-usage.Bytes {
				return usage, ErrCapacity
			}
			usage.Bytes += r.Reference.SizeBytes
		}
	}
	if usage.Count > publicationReceipts || usage.Metadata > publicationBucketBytes {
		return usage, ErrCapacity
	}
	return usage, nil
}

func (m *publicationMetadata) rebuild(ctx context.Context, root *os.Root, index int) (publicationUsage, error) {
	usage, err := m.census(ctx, root, index)
	if err != nil {
		return usage, err
	}
	body, err := json.Marshal(usage)
	if err != nil {
		return usage, ErrStore
	}
	f, err := openPrivateFile(root, "usage", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return usage, err
	}
	if err = f.Truncate(0); err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err == nil {
		err = appendJournalFrame(f, body, 4096)
	}
	_ = f.Close()
	if err != nil || syncQueueDir(root) != nil {
		return usage, ErrStore
	}
	m.mark("metadata_after_usage_sync")
	if err := removeQueueFile(root, "usage.dirty"); err != nil {
		return usage, ErrStore
	}
	if syncQueueDir(root) != nil {
		return usage, ErrStore
	}
	m.mark("metadata_after_clean")
	return usage, nil
}

func (m *publicationMetadata) usage(ctx context.Context, root *os.Root, index int) (publicationUsage, error) {
	var u publicationUsage
	dirty, err := openPrivateFile(root, "usage.dirty", os.O_RDWR, 0o600)
	if err == nil {
		st, err := dirty.Stat()
		_ = dirty.Close()
		if err != nil || st.Size() != 0 {
			return u, ErrStore
		}
		return m.rebuild(ctx, root, index)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return u, err
	}
	f, err := openPrivateFile(root, "usage", os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		dir, err := root.Open(".")
		if err != nil {
			return u, ErrStore
		}
		entries, readErr := dir.ReadDir(1)
		_ = dir.Close()
		if len(entries) != 0 || readErr != nil && !errors.Is(readErr, io.EOF) {
			return u, ErrStore
		}
		return u, nil
	}
	if err != nil {
		return u, err
	}
	defer func() { _ = f.Close() }()
	count := 0
	_, err = readJournalFramesMode(f, 4096, false, func(body []byte) error {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		count++
		if dec.Decode(&u) != nil || dec.Decode(new(any)) != io.EOF || count != 1 {
			return ErrStore
		}
		return nil
	})
	identity, identityErr := publicationRootIdentity(root)
	if err != nil || identityErr != nil || count != 1 || u.Version != 1 || u.Identity != identity || u.Count < 0 || u.Count > publicationReceipts || u.Bytes < 0 || u.Bytes > m.objectBytes || u.Metadata < 0 || u.Metadata > publicationBucketBytes {
		return u, ErrStore
	}
	return u, nil
}

func (m *publicationMetadata) read(ctx context.Context, name string) (publicationRecord, *os.File, error) {
	index, err := publicationBucket(name)
	if err != nil {
		return publicationRecord{}, nil, err
	}
	root, closeRoot, err := m.bucket(index, false)
	if errors.Is(err, os.ErrNotExist) {
		return publicationRecord{}, nil, nil
	}
	if err != nil {
		return publicationRecord{}, nil, err
	}
	defer closeRoot()
	if _, err := m.usage(ctx, root, index); err != nil {
		return publicationRecord{}, nil, err
	}
	return readPublicationJournal(root, name, true)
}

// Reserve metadata before creating a first frame. All 64 bounded summaries are
// checked, not all historic journals. Repair scans at most one dirty bucket at a
// time. Existing publication.lock serializes reservations and state transitions.
func (m *publicationMetadata) reserve(ctx context.Context, ref Reference, state string) error {
	name := publicationKey(ref) + ".journal"
	index, _ := publicationBucket(name)
	total := int64(0)
	for i := range publicationBuckets {
		if err := ctx.Err(); err != nil {
			return err
		}
		root, closeRoot, err := m.bucket(i, false)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		u, err := m.usage(ctx, root, i)
		closeRoot()
		if err != nil {
			return err
		}
		if u.Bytes > m.objectBytes-total {
			return ErrCapacity
		}
		total += u.Bytes
		if i == index && (u.Count >= publicationReceipts || u.Metadata > publicationBucketBytes-publicationJournalBytes) {
			return ErrCapacity
		}
	}
	if state != "fenced" && ref.SizeBytes > m.objectBytes-total {
		return ErrCapacity
	}
	root, closeRoot, err := m.bucket(index, true)
	if err != nil {
		return err
	}
	defer closeRoot()
	if err := queueFreeSpace(root, publicationJournalBytes); err != nil {
		return err
	}
	return m.dirty(root)
}

func (m *publicationMetadata) create(ref Reference) (*os.File, error) {
	index, _ := publicationBucket(publicationKey(ref) + ".journal")
	root, closeRoot, err := m.bucket(index, false)
	if err != nil {
		return nil, err
	}
	defer closeRoot()
	return openPrivateFile(root, publicationKey(ref)+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
}

func (m *publicationMetadata) save(ctx context.Context, f *os.File, r publicationRecord) error {
	index, _ := publicationBucket(publicationKey(r.Reference) + ".journal")
	root, closeRoot, err := m.bucket(index, false)
	if err != nil {
		return err
	}
	defer closeRoot()
	if err := m.dirty(root); err != nil {
		return err
	}
	body, err := json.Marshal(r)
	if err != nil {
		return ErrStore
	}
	if err := appendJournalFrame(f, body, publicationJournalBytes); err != nil {
		return err
	}
	if syncQueueDir(root) != nil {
		return ErrStore
	}
	m.mark("metadata_after_journal_sync")
	_, err = m.rebuild(ctx, root, index)
	return err
}
