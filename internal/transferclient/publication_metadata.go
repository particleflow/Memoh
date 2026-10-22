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
	"slices"
	"strings"
)

// Versioned defaults in the existing publication locator, not a second config
// root. Objects retain the queue's separately accounted, immutable byte budget.
const (
	publicationDirectory         = "publications-v1"
	publicationReceipts          = 128 // Per bucket in locator v2; global only in legacy v1.
	publicationBuckets           = 64
	publicationJournalBytes      = 64 << 10
	publicationUsageBytes        = 16 << 10
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
	Version  int      `json:"version"`
	Identity string   `json:"identity"`
	Count    int      `json:"count"`
	Bytes    int64    `json:"bytes"`
	Metadata int64    `json:"metadata"`
	Keys     []string `json:"keys"`
}

func (m *publicationMetadata) mark(stage string) {
	if m.hook != nil {
		m.hook(stage)
	}
}

// Only creation of a new store, or closed locator state 0 migration, may seed
// empty buckets. Once locator v2 is visible every bucket and usage file MUST
// exist: absence is damaged evidence, never proof that an ID was unused.
func (m *publicationMetadata) initialize(ctx context.Context, fresh bool) error {
	if fresh {
		// This path is exclusively before the very first locator commit. Never
		// use it to repair an existing store or a closed migration's evidence.
		if _, err := m.root.Lstat(publicationMetadataDirectory); !errors.Is(err, os.ErrNotExist) {
			return ErrStore
		}
	}
	catalog, closeCatalog, err := openPublicationDirectory(m.root, publicationMetadataDirectory, true)
	if err != nil {
		return errors.Join(ErrStore, err)
	}
	defer closeCatalog()
	for i := range publicationBuckets {
		if err := ctx.Err(); err != nil {
			return err
		}
		root, closeRoot, err := openPublicationDirectory(catalog, fmt.Sprintf("%02x", i), true)
		if err != nil {
			return err
		}
		if fresh {
			err = seedEmptyPublicationUsage(root)
			closeRoot()
			if err != nil {
				return err
			}
			continue
		}
		if _, err := root.Lstat("usage"); errors.Is(err, os.ErrNotExist) {
			dir, openErr := root.Open(".")
			if openErr != nil {
				closeRoot()
				return ErrStore
			}
			entries, readErr := dir.ReadDir(1)
			_ = dir.Close()
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				closeRoot()
				return ErrStore
			}
			if len(entries) == 0 {
				err = m.dirty(root, i, true)
				if err != nil {
					closeRoot()
					return err
				}
			}
		} else if err != nil {
			closeRoot()
			return ErrStore
		}
		_, err = m.usage(ctx, root, i)
		closeRoot()
		if err != nil {
			return err
		}
	}
	return nil
}

// A brand-new, still uncommitted store has no old state to witness. Persist
// each initial empty usage exactly once; a partial initial creation remains
// unavailable, just like an incomplete initial locator, rather than resetting it.
func seedEmptyPublicationUsage(root *os.Root) error {
	identity, err := publicationRootIdentity(root)
	if err != nil {
		return err
	}
	body, err := json.Marshal(publicationUsage{Version: 1, Identity: identity})
	if err != nil {
		return ErrStore
	}
	f, err := openPrivateFile(root, "usage", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	err = appendJournalFrame(f, body, publicationUsageBytes)
	_ = f.Close()
	if err != nil || syncQueueDir(root) != nil {
		return ErrStore
	}
	return nil
}

func (m *publicationMetadata) bucket(index int, create bool) (*os.Root, func(), error) {
	if index < 0 || index >= publicationBuckets {
		return nil, nil, ErrInvalid
	}
	catalog, closeCatalog, err := openPublicationDirectory(m.root, publicationMetadataDirectory, create)
	if err != nil {
		return nil, nil, err
	}
	root, closeRoot, err := openPublicationDirectory(catalog, fmt.Sprintf("%02x", index), create)
	if err != nil {
		closeCatalog()
		return nil, nil, err
	}
	return root, func() { closeRoot(); closeCatalog() }, nil
}

// A caller may retain this verified, pinned directory only for one locked,
// finite operation. No usage or authorization is cached across invocations.
func openPublicationDirectory(parent *os.Root, name string, create bool) (*os.Root, func(), error) {
	st, err := parent.Lstat(name)
	if create && errors.Is(err, os.ErrNotExist) {
		if err := makeQueueDir(parent, name); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, nil, err
		}
		st, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrStore
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, nil, ErrStore
	}
	if err := checkQueueRoot(root); err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	guard, err := guardQueueRoot(root)
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	closeRoot := func() { closeQueueGuard(guard); _ = root.Close() }
	// Also flush a pre-existing entry during maintenance: an earlier create
	// may have returned a sync error before this resumed invocation.
	if create && syncQueueDir(parent) != nil {
		closeRoot()
		return nil, nil, ErrStore
	}
	return root, closeRoot, nil
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

// The dirty witness retains the last acknowledged membership before any journal
// or usage mutation. Recovery may add records but must never shrink this set.
// Missing usage may only be seeded during new-store creation or closed migration.
func (m *publicationMetadata) dirty(root *os.Root, index int, seed bool) error {
	if _, err := m.loadUsage(root, "usage.dirty", index); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	before, err := m.loadUsage(root, "usage", index)
	if err == nil {
		err = checkPublicationMembership(root, before.Keys)
	}
	if errors.Is(err, os.ErrNotExist) && seed {
		before.Identity, err = publicationRootIdentity(root)
		before.Version = 1
	}
	if err != nil {
		return ErrStore
	}
	body, err := json.Marshal(before)
	if err != nil {
		return ErrStore
	}
	f, err := openPrivateFile(root, "usage.dirty", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	err = appendJournalFrame(f, body, publicationUsageBytes)
	_ = f.Close()
	if err != nil || syncQueueDir(root) != nil {
		return ErrStore
	}
	m.mark("metadata_after_dirty")
	return nil
}

// Check only bounded directory entries, not journal payloads. Before mutation,
// this prevents accepting an unexpected journal through dirty-cache recovery.
func checkPublicationMembership(root *os.Root, keys []string) error {
	dir, err := root.Open(".")
	if err != nil {
		return ErrStore
	}
	entries, readErr := dir.ReadDir(publicationReceipts + 3)
	_ = dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(entries) > publicationReceipts+2 {
		return ErrStore
	}
	var actual []string
	for _, entry := range entries {
		if entry.Name() == "usage" || entry.Name() == "usage.dirty" {
			continue
		}
		if !entry.Type().IsRegular() {
			return ErrStore
		}
		actual = append(actual, entry.Name())
	}
	slices.Sort(actual)
	if !slices.Equal(actual, keys) {
		return ErrStore
	}
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
		usage.Keys = append(usage.Keys, e.Name())
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
	slices.Sort(usage.Keys)
	return usage, nil
}

func (m *publicationMetadata) rebuild(ctx context.Context, root *os.Root, index int) (publicationUsage, error) {
	before, err := m.loadUsage(root, "usage.dirty", index)
	if err != nil {
		return publicationUsage{}, ErrStore
	}
	usage, err := m.census(ctx, root, index)
	if err != nil {
		return usage, err
	}
	for _, key := range before.Keys {
		if _, present := slices.BinarySearch(usage.Keys, key); !present {
			return usage, ErrStore
		}
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
		err = appendJournalFrame(f, body, publicationUsageBytes)
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
	if _, err := root.Lstat("usage.dirty"); err == nil {
		return m.rebuild(ctx, root, index)
	} else if !errors.Is(err, os.ErrNotExist) {
		return publicationUsage{}, ErrStore
	}
	u, err := m.loadUsage(root, "usage", index)
	if err != nil {
		return u, ErrStore
	}
	return u, nil
}

func (m *publicationMetadata) loadUsage(root *os.Root, name string, index int) (publicationUsage, error) {
	var u publicationUsage
	f, err := openPrivateFile(root, name, os.O_RDWR, 0o600)
	if err != nil {
		return u, err
	}
	defer func() { _ = f.Close() }()
	count := 0
	_, err = readJournalFramesMode(f, publicationUsageBytes, false, func(body []byte) error {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		count++
		if dec.Decode(&u) != nil || dec.Decode(new(any)) != io.EOF || count != 1 {
			return ErrStore
		}
		return nil
	})
	identity, identityErr := publicationRootIdentity(root)
	if err != nil || identityErr != nil || count != 1 || u.Version != 1 || u.Identity != identity || u.Count < 0 || u.Count > publicationReceipts || u.Count != len(u.Keys) || u.Bytes < 0 || u.Bytes > m.objectBytes || u.Metadata < 0 || u.Metadata > publicationBucketBytes {
		return u, ErrStore
	}
	for i, key := range u.Keys {
		bucket, err := publicationBucket(key)
		if err != nil || bucket != index || i > 0 && key <= u.Keys[i-1] {
			return u, ErrStore
		}
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
		return publicationRecord{}, nil, ErrStore
	}
	if err != nil {
		return publicationRecord{}, nil, err
	}
	defer closeRoot()
	u, err := m.usage(ctx, root, index)
	if err != nil {
		return publicationRecord{}, nil, err
	}
	r, f, err := readPublicationJournal(root, name, true)
	_, present := slices.BinarySearch(u.Keys, name)
	if err == nil && present != (f != nil) {
		if f != nil {
			_ = f.Close()
		}
		return publicationRecord{}, nil, ErrStore
	}
	return r, f, err
}

// Reserve metadata before creating a first frame. All 64 bounded summaries are
// checked, not all historic journals. Repair scans at most one dirty bucket at a
// time. Existing publication.lock serializes reservations and state transitions.
func (m *publicationMetadata) reserve(ctx context.Context, ref Reference, state string) error {
	name := publicationKey(ref) + ".journal"
	index, _ := publicationBucket(name)
	total := int64(0)
	catalog, closeCatalog, err := openPublicationDirectory(m.root, publicationMetadataDirectory, false)
	if err != nil {
		return errors.Join(ErrStore, err)
	}
	defer closeCatalog()
	for i := range publicationBuckets {
		if err := ctx.Err(); err != nil {
			return err
		}
		root, closeRoot, err := openPublicationDirectory(catalog, fmt.Sprintf("%02x", i), false)
		if errors.Is(err, os.ErrNotExist) {
			return ErrStore
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
	root, closeRoot, err := m.bucket(index, false)
	if err != nil {
		return err
	}
	defer closeRoot()
	if err := queueFreeSpace(root, publicationJournalBytes); err != nil {
		return err
	}
	return m.dirty(root, index, false)
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
	if err := m.dirty(root, index, false); err != nil {
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

func (m *publicationMetadata) validate(ctx context.Context) error {
	var total int64
	catalog, closeCatalog, err := openPublicationDirectory(m.root, publicationMetadataDirectory, false)
	if err != nil {
		return errors.Join(ErrStore, err)
	}
	defer closeCatalog()
	for i := range publicationBuckets {
		if err := ctx.Err(); err != nil {
			return err
		}
		root, closeRoot, err := openPublicationDirectory(catalog, fmt.Sprintf("%02x", i), false)
		if err != nil {
			return ErrStore
		}
		u, err := m.usage(ctx, root, i)
		if err == nil {
			err = checkPublicationMembership(root, u.Keys)
		}
		closeRoot()
		if err != nil {
			return err
		}
		if u.Bytes > m.objectBytes-total {
			return ErrCapacity
		}
		total += u.Bytes
	}
	return nil
}
