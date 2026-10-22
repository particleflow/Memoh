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
	ErrBusy     = errors.New("delivery is already locked")
	ErrStore    = errors.New("delivery store unavailable or corrupt")
	ErrCapacity = errors.New("delivery capacity exhausted")
	ErrBinding  = errors.New("delivery host binding unavailable")
	ErrVersion  = errors.New("delivery queue schema migration required")
)

// QueueLimits bound retained jobs (including incomplete admissions), reserved
// bytes and each journal. Nothing is evicted to make room for a new delivery.
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
	root     *os.Root
	guard    *os.File
	revision string
	limits   QueueLimits
}

// QueueStatus is bounded transfer/outbox metadata. Associated means the host's
// authorized registration accepted the stable reference, not task completion.
type QueueStatus struct {
	Version             int           `json:"version"`
	Direction           string        `json:"direction"`
	Job                 Job           `json:"job"`
	Seal                *SnapshotSeal `json:"seal,omitempty"`
	AdmissionVersion    int           `json:"admission_version,omitempty"`
	Admitted            bool          `json:"admitted"`
	Associated          bool          `json:"associated"`
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
	if err := makeQueueDir(parent, "deliveries-v1"); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, ErrStore
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
	q := &DiskQueue{root: root, guard: guard, revision: revision, limits: limits}
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
	}{2, revision, path, limits})
	if err != nil {
		return nil, ErrInvalid
	}
	flags := os.O_RDWR
	if info, err := root.Lstat("locator"); errors.Is(err, os.ErrNotExist) {
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
	if len(actual) == 0 {
		if _, err := f.Write(want); err != nil || f.Sync() != nil {
			return nil, ErrStore
		}
	} else if !bytes.Equal(actual, want) {
		var old struct{ Version int }
		if json.Unmarshal(actual, &old) == nil && old.Version != 2 {
			return nil, ErrVersion
		}
		return nil, ErrStore
	}
	if err := syncQueueDir(root); err != nil {
		return nil, err
	}
	if err := syncQueueDir(parent); err != nil {
		return nil, err
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
	return q.physicalFile(queueName(name), flags, mode)
}

func (q *DiskQueue) physicalFile(name string, flags int, mode os.FileMode) (*os.File, error) {
	if info, err := q.root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return nil, ErrStore
	}
	f, err := openQueueFile(q.root, name, flags, mode)
	if err != nil {
		return nil, err
	}
	if err := checkQueueFile(f); err != nil {
		_ = f.Close()
		return nil, ErrStore
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
	f, err := q.file(name+".lock", os.O_RDWR|os.O_CREATE, 0o600)
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

// A journal is append-only length+SHA256+JSON, fsynced before acknowledgement.
// Only an incomplete final frame is truncated on reopen. Full corrupt frames,
// wrong versions and journal exhaustion fail closed. No rename per checkpoint.
func (q *DiskQueue) load(id string) (QueueStatus, *os.File, error) {
	var record QueueStatus
	f, err := q.file(id+".journal", os.O_RDWR, 0o600)
	if err != nil {
		return record, nil, err
	}
	validEnd := int64(0)
	for {
		var head [36]byte
		n, err := io.ReadFull(f, head[:])
		if errors.Is(err, io.EOF) && n == 0 {
			break
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			_ = f.Close()
			return record, nil, ErrStore
		}
		size := int64(binary.BigEndian.Uint32(head[:4]))
		if size < 1 || size > 16384 || validEnd+36+size > q.limits.JournalBytes {
			_ = f.Close()
			return record, nil, ErrStore
		}
		body := make([]byte, int(size))
		if _, err := io.ReadFull(f, body); errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			_ = f.Close()
			return record, nil, ErrStore
		}
		sum := sha256.Sum256(body)
		var next QueueStatus
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if !bytes.Equal(sum[:], head[4:]) || dec.Decode(&next) != nil || !q.validRecord(next, id) {
			_ = f.Close()
			return record, nil, ErrStore
		}
		record = next
		validEnd += 36 + size
	}
	if validEnd == 0 || f.Truncate(validEnd) != nil || f.Sync() != nil {
		_ = f.Close()
		return record, nil, ErrStore
	}
	if _, err := f.Seek(validEnd, io.SeekStart); err != nil {
		_ = f.Close()
		return record, nil, ErrStore
	}
	return record, f, nil
}

func (q *DiskQueue) validRecord(r QueueStatus, id string) bool {
	if r.AdmissionVersion < 0 || r.AdmissionVersion > 1 || r.Job.Spec.SizeBytes > q.limits.Bytes {
		return false
	}
	if r.Admitted && r.Direction == "upload" && !r.Seal.valid() {
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
	return r.Version == 2 && r.Job.valid() && r.Job.DeliveryID == id && r.Job.SnapshotID == id && r.Job.LocatorRevision == q.revision && (r.Direction == "upload" || r.Direction == "download") && (!r.Associated || r.Job.Phase == "ready") && r.AssociationAttempts >= 0 && r.AssociationAttempts <= 12 && associationStateValid(r.AssociationState)
}

func (q *DiskQueue) save(f *os.File, r QueueStatus) error {
	if !q.validRecord(r, r.Job.DeliveryID) {
		return ErrInvalid
	}
	body, err := json.Marshal(r)
	if err != nil || len(body) > 16384 {
		return ErrStore
	}
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil || end+36+int64(len(body)) > q.limits.JournalBytes {
		return ErrCapacity
	}
	frame := make([]byte, 36+len(body))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body))) //nolint:gosec // Body length was bounded to 16384 above.
	sum := sha256.Sum256(body)
	copy(frame[4:36], sum[:])
	copy(frame[36:], body)
	if n, err := f.Write(frame); err != nil || n != len(frame) || f.Sync() != nil {
		return ErrStore
	}
	return nil
}
