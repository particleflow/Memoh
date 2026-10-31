package transferclient

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var (
	ErrBusy       = errors.New("delivery is already locked")
	ErrStore      = errors.New("delivery store unavailable or corrupt")
	ErrCapacity   = errors.New("delivery capacity exhausted")
	ErrBinding    = errors.New("delivery host binding unavailable")
	ErrVersion    = errors.New("delivery queue schema migration required")
	ErrPermission = errors.New("delivery private storage permission denied")
)

// QueueLimits retain the legacy snapshot admission policy and locator binding.
// Download metadata has no cumulative byte/job quota; Workers bounds concurrent
// streaming and actual filesystem space bounds retained download bytes.
// Confirmed deliveries keep compact receipts in the existing history layout;
// metadata capacity never evicts idempotency proof.
type QueueLimits struct {
	Jobs         int
	Bytes        int64
	JournalBytes int64
	Workers      int
}

// DiskQueue owns file-transfer metadata only, under Herdr's existing plugin
// STATE_DIR. It is a finite-command adapter, not a scheduler or task database.
// The host must grant this directory only to its trusted user. Config, logs,
// execution receipts and NAS originals remain owned by their existing services.
type DiskQueue struct {
	downloadSpaceCheck func(int64) error // Test-only fault injection; nil uses actual filesystem space.
	readOnly           bool
	root               *os.Root
	guard              *os.File
	revision           string
	limits             QueueLimits
	releaseHook        func(string)
	journalHook        func(string)
	historyHook        func(string)
	budgetBase         int64 // Nonzero only for explicitly upgraded locator v4.
	openSource         func(string) (*os.File, error)
	snapshotOnly       bool // finite invocation capability, never persisted or inferred from a job
}

// QueueStatus is bounded transfer/outbox metadata. Associated means the host's
// authorized registration accepted the stable reference, not task completion.
type QueueStatus struct {
	Version             int           `json:"version"`
	Registered          bool          `json:"registered,omitempty"`
	Activated           bool          `json:"activated,omitempty"`
	Source              *QueuedSource `json:"source,omitempty"`
	LastRun             time.Time     `json:"last_run_at,omitzero"`
	PayloadState        string        `json:"payload_state,omitempty"`
	Direction           string        `json:"direction"`
	Job                 Job           `json:"job"`
	Seal                *SnapshotSeal `json:"seal,omitempty"`
	AdmissionVersion    int           `json:"admission_version,omitempty"`
	Admitted            bool          `json:"admitted"`
	Associated          bool          `json:"associated"`
	AssociationError    string        `json:"association_error,omitempty"`
	AssociationState    string        `json:"association_state,omitempty"`
	AssociationAttempts int           `json:"association_attempts"`
	AssociationDue      time.Time     `json:"association_due,omitempty"`
	AssociationBlocked  bool          `json:"association_blocked"`
}

// OpenHerdrQueue accepts Herdr's resolved locator once. Empty, relative,
// symlinked or relocated roots fail explicitly; no cwd/temp/legacy fallback.
// Native validation must succeed before any network work. Windows requires a
// private local NTFS directory; it never repairs existing ACLs or elevates.
func OpenHerdrQueue(getenv func(string) string, revision string, limits QueueLimits) (*DiskQueue, error) {
	return openHerdrQueue(getenv, revision, limits, false)
}

func openHerdrQueue(getenv func(string) string, revision string, limits QueueLimits, readOnly bool) (*DiskQueue, error) {
	return openQueueLayout(getenv, revision, limits, readOnly, false, nil)
}

func openQueueLayout(getenv func(string) string, revision string, limits QueueLimits, readOnly, migrate bool, hook func(string)) (*DiskQueue, error) {
	return openQueueLayoutMode(getenv, revision, limits, readOnly, migrate, false, hook)
}

func openQueueLayoutMode(getenv func(string) string, revision string, limits QueueLimits, readOnly, migrate, budgetMigration bool, hook func(string)) (*DiskQueue, error) {
	if getenv == nil || !validID(revision) || limits.Jobs < 1 || limits.Jobs > 128 || limits.Bytes < 1 || limits.Bytes > 1<<40 || limits.JournalBytes < 65536 || limits.JournalBytes > 8<<20 || limits.Workers < 1 || limits.Workers > 4 {
		return nil, ErrInvalid
	}
	if err := queuePlatformReady(); err != nil {
		return nil, err
	}
	path := getenv("HERDR_PLUGIN_STATE_DIR")
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrInvalid
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, ErrStore
	}
	parent, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrStore
	}
	defer func() { _ = parent.Close() }()
	parentGuard, err := guardQueueRoot(parent)
	if err != nil {
		return nil, err
	}
	defer closeQueueGuard(parentGuard)
	if !readOnly {
		if err := makeQueueDir(parent, "deliveries-v1"); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	info, err := parent.Lstat("deliveries-v1")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrStore
	}
	root, err := parent.OpenRoot("deliveries-v1")
	if err != nil {
		return nil, ErrStore
	}
	if err := checkQueueRoot(root); err != nil {
		_ = root.Close()
		return nil, err
	}
	guard, err := guardQueueRoot(root)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	q := &DiskQueue{root: root, guard: guard, revision: revision, limits: limits, readOnly: readOnly, historyHook: hook}
	ok := false
	defer func() {
		if !ok {
			closeQueueGuard(guard)
			_ = root.Close()
		}
	}()
	lock, err := q.lock("admission")
	if err != nil {
		return nil, err
	}
	defer unlockClose(lock)
	// The locator version and resolved location are one immutable header. A
	// directory move is a migration, never an implicit new path resolution.
	want, err := json.Marshal(struct {
		Version        int
		Revision, Path string
		Limits         QueueLimits
	}{3, revision, path, limits})
	if err != nil {
		return nil, ErrInvalid
	}
	flags := os.O_RDWR
	if readOnly {
		flags = os.O_RDONLY
	}
	if info, err := root.Lstat("locator"); errors.Is(err, os.ErrNotExist) {
		if readOnly {
			return nil, ErrBinding
		}
		dir, err := root.Open(".")
		if err != nil {
			return nil, ErrStore
		}
		entries, err := dir.ReadDir(2)
		_ = dir.Close()
		if err != nil && !errors.Is(err, io.EOF) || len(entries) != 1 || entries[0].Name() != "admission.lock" {
			return nil, ErrStore
		}
		flags |= os.O_CREATE | os.O_EXCL
	} else if err != nil || info.Size() == 0 {
		return nil, ErrStore
	}
	f, err := q.file("locator", flags, 0o600)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	actual, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(want) > 4096 {
		return nil, ErrStore
	}
	budgetHandled, err := q.openBudgetLocator(f, actual, want, budgetMigration)
	if err != nil {
		return nil, err
	}
	if budgetHandled {
		if migrate {
			return nil, ErrVersion // History v2->v3 must precede budget v4.
		}
		// Only the explicit budget journal can explain this locator version.
		actual = want
	}
	if migrate && len(actual) != 0 {
		if err := q.migrateLocator(f, actual, want); err != nil {
			return nil, err
		}
		actual = want
	}
	if readOnly && !bytes.Equal(actual, want) {
		return nil, ErrVersion
	}
	if len(actual) == 0 {
		if _, err := f.Write(want); err != nil || f.Sync() != nil {
			return nil, ErrStore
		}
	} else if !bytes.Equal(actual, want) {
		var old struct{ Version int }
		if json.Unmarshal(actual, &old) == nil && old.Version != 3 {
			return nil, ErrVersion
		}
		return nil, ErrStore
	}
	if !readOnly {
		// Repair a lost migration-file sync before any new layout mutation.
		if err := f.Sync(); err != nil {
			return nil, ErrStore
		}
		if err := syncQueueDir(root); err != nil {
			return nil, err
		}
		if err := syncQueueDir(parent); err != nil {
			return nil, err
		}
	}
	ok = true
	return q, nil
}

func (q *DiskQueue) Close() error {
	closeQueueGuard(q.guard)
	return q.root.Close()
}

func closeQueueGuard(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}

// All paths below are private layout constants or validated logical IDs.
func (q *DiskQueue) file(name string, flags int, mode os.FileMode) (*os.File, error) {
	if q.readOnly && flags != os.O_RDONLY {
		return nil, ErrInvalid
	}
	return q.physicalFile(queueName(name), flags, mode)
}

func (q *DiskQueue) physicalFile(name string, flags int, mode os.FileMode) (*os.File, error) {
	return openPrivateFile(q.root, name, flags, mode)
}

func openPrivateFile(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return nil, ErrStore
	}
	f, err := openQueueFile(root, name, flags, mode)
	if err != nil {
		return nil, err
	}
	if err := checkQueueFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func (q *DiskQueue) lock(id string) (*os.File, error) {
	if id != "admission" {
		sum := sha256.Sum256([]byte(id))
		id = fmt.Sprintf("job-%02x", sum[0]&127)
	}
	return q.lockName(id)
}

func (q *DiskQueue) lockName(name string) (*os.File, error) {
	flags := os.O_RDWR | os.O_CREATE
	if q.readOnly {
		flags = os.O_RDONLY
	}
	f, err := q.file(name+".lock", flags, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockQueueFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func (q *DiskQueue) worker() (*os.File, error) {
	for i := 0; i < q.limits.Workers; i++ {
		f, err := q.lockName(fmt.Sprintf("worker-%d", i))
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, ErrBusy) {
			return nil, err
		}
	}
	return nil, ErrBusy
}

// An active journal is append-only length+SHA256+JSON, synced before acknowledgement.
// Confirmed delivery journals may be compacted with a synced release witness.
// Only an incomplete final frame is truncated on reopen. Full corrupt frames,
// wrong versions and journal exhaustion fail closed. No rename per checkpoint.
func (q *DiskQueue) loadActive(id string) (QueueStatus, *os.File, error) {
	if q.readOnly {
		return q.readOnlyRecord(id)
	}
	if r, f, found, err := q.recoverActiveJournal(id); found || err != nil {
		return r, f, err
	}
	r, f, err := q.loadJournal(id)
	if err == nil {
		return r, f, nil
	}
	// A durable released receipt is authoritative only after payload deletion
	// and directory sync. It recovers an interrupted journal compaction.
	saved, backup, recoverErr := q.loadReleaseReceipt(id)
	if recoverErr == nil {
		return saved, backup, nil
	}
	return r, nil, err
}

func (q *DiskQueue) loadJournal(id string) (QueueStatus, *os.File, error) {
	var record QueueStatus
	flags := os.O_RDWR
	if q.readOnly {
		flags = os.O_RDONLY
	}
	f, err := q.file(id+".journal", flags, 0o600)
	if err != nil {
		return record, nil, err
	}
	end, err := readJournalFramesMode(f, q.limits.JournalBytes, !q.readOnly, func(body []byte) error {
		var next QueueStatus
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if dec.Decode(&next) != nil {
			return ErrStore
		}
		if next.Version != 2 && next.Version != 3 && next.Version != 4 && next.Version != 5 {
			return ErrVersion
		}
		if !q.validRecord(next, id) {
			return ErrStore
		}
		record = next
		return nil
	})
	if err != nil || end == 0 {
		_ = f.Close()
		return record, nil, errors.Join(ErrStore, err)
	}
	return record, f, nil
}

// readJournalFrames shares the queue's bounded checksum/fsync recovery format
// with the same component's publication receipts. Only a torn final frame is
// truncated. Callers decide whether an entirely uninitialized file is valid.
func readJournalFrames(f *os.File, limit int64, consume func([]byte) error) (int64, error) {
	return readJournalFramesMode(f, limit, true, consume)
}

func readJournalFramesMode(f *os.File, limit int64, repair bool, consume func([]byte) error) (int64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, ErrStore
	}
	validEnd := int64(0)
	for {
		var head [36]byte
		n, err := io.ReadFull(f, head[:])
		if errors.Is(err, io.EOF) && n == 0 || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return 0, ErrStore
		}
		size := int64(binary.BigEndian.Uint32(head[:4]))
		if size < 1 || size > 16384 || validEnd+36+size > limit {
			return 0, ErrStore
		}
		body := make([]byte, int(size))
		if _, err := io.ReadFull(f, body); errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return 0, ErrStore
		}
		sum := sha256.Sum256(body)
		if !bytes.Equal(sum[:], head[4:]) {
			return 0, ErrStore
		}
		if err := consume(body); err != nil {
			return 0, errors.Join(ErrStore, err)
		}
		validEnd += 36 + size
	}
	if repair && (f.Truncate(validEnd) != nil || f.Sync() != nil) {
		return 0, ErrStore
	}
	if _, err := f.Seek(validEnd, io.SeekStart); err != nil {
		return 0, ErrStore
	}
	return validEnd, nil
}

func (q *DiskQueue) validRecord(r QueueStatus, id string) bool {
	if r.Version != 2 && r.Version != 3 && r.Version != 4 && r.Version != 5 {
		return false
	}
	if r.PayloadState != "" || r.Version == 3 {
		if r.Version == 2 || (r.PayloadState != PayloadReleasePending && r.PayloadState != PayloadReleased) || !confirmedDelivery(r) {
			return false
		}
	}

	if r.AdmissionVersion < 0 || r.AdmissionVersion > 1 || !r.Registered && r.Direction == "upload" && r.Job.Spec.SizeBytes > q.limits.Bytes {
		return false
	}
	if r.Registered && (r.Source == nil || !r.Source.valid() || r.Direction != "upload" || r.Admitted && !r.Activated) {
		return false
	}
	if !r.Registered && (r.Source != nil || r.Activated) {
		return false
	}
	if r.Admitted && r.Direction == "upload" && !r.Seal.valid() {
		return false
	}
	switch r.AssociationError {
	case "", "authentication", "permission", "configuration", "capacity", "transient", "busy", "unknown":
	default:
		return false
	}
	switch r.AssociationState {
	case AssociationCommitted:
		if !r.Associated || r.Job.Phase != "ready" {
			return false
		}
	case AssociationCanceled:
		if r.Associated || r.Job.Phase != "canceled" {
			return false
		}
	case AssociationInFlight, AssociationUnknown, AssociationRetryCommit:
		if r.Associated || !r.Admitted || r.Job.Phase != "ready" {
			return false
		}
	}
	return r.Job.valid() && r.Job.DeliveryID == id && r.Job.SnapshotID == id && r.Job.LocatorRevision == q.revision && (r.Direction == "upload" || r.Direction == "download") && (!r.Associated || r.Job.Phase == "ready") && r.AssociationAttempts >= 0 && associationStateValid(r.AssociationState)
}

func (q *DiskQueue) save(f *os.File, r QueueStatus) error {
	if q.readOnly || !q.validRecord(r, r.Job.DeliveryID) {
		return ErrInvalid
	}
	// Fence old queue writers before creating a compaction witness. Every later
	// write retains this version, including terminal receipt compaction.
	r.Version = recordVersion(r)
	body, err := json.Marshal(r)
	if err != nil || len(body) > 16384 {
		return ErrStore
	}
	if err := appendJournalFrame(f, body, q.limits.JournalBytes); err != nil {
		return err
	}
	stat, err := f.Stat()
	if err != nil {
		return ErrStore
	}
	if r.PayloadState == "" && (r.Registered && r.Associated || stat.Size() >= min(int64(65536), q.limits.JournalBytes/2)) {
		return q.compactActiveJournal(f, r)
	}
	return nil
}

func appendJournalFrame(f *os.File, body []byte, limit int64) error {
	return appendBoundedFrame(f, body, limit, 16384)
}

func appendBoundedFrame(f *os.File, body []byte, limit int64, maxBody int) error {
	if maxBody < 1 || maxBody > 17408 || len(body) < 1 || len(body) > maxBody {
		return ErrStore
	}
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil || end+36+int64(len(body)) > limit {
		return ErrCapacity
	}
	frame := make([]byte, 36+len(body))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body))) //nolint:gosec // Body length was bounded to at most 17408 above.
	sum := sha256.Sum256(body)
	copy(frame[4:36], sum[:])
	copy(frame[36:], body)
	if n, err := f.Write(frame); err != nil || n != len(frame) || f.Sync() != nil {
		return ErrStore
	}
	return nil
}
