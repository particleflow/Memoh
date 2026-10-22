package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

var errAdmissionFrame = errors.Join(ErrStore, errors.New("incomplete or changed admission frame"))

// Associate must atomically reauthorize the current task owner, Bot, session,
// target fingerprint and direction, resolve ready media, and associate by
// DeliveryID with idempotent replay. Reuse ComputerOperationStore/CAS; never
// interpret OperationID as a credential. A lost response is retried with the
// same reference. No production implementation is supplied by this adapter.
// The reader is nil for uploads. For downloads it exposes the verified private
// staging handle only during the call; the host must authorize before publishing
// it through its existing destination storage service, idempotently by delivery.
type Associate func(context.Context, string, Reference, io.Reader) error

// Admit reserves disk budget before reading. For uploads the producer supplies
// a closed-output hash/size and an already authorized regular file handle.
// A bounded copy verified against that hash becomes the private snapshot;
// subsequent source mutation cannot change it. Paths never enter the journal.
// Download admission reserves the entire declared final size as well.
func (q *DiskQueue) Admit(ctx context.Context, direction string, job Job, source *os.File) error {
	if !job.valid() || job.Phase != "queued" || job.Offset != 0 || job.ReceiptID != "" || job.CancelRequested || job.Attempts != 0 || job.SnapshotID != job.DeliveryID || job.LocatorRevision != q.revision || job.DeliveryID == "admission" || direction != "upload" && direction != "download" {
		return ErrInvalid
	}
	if job.Spec.SizeBytes > q.limits.Bytes {
		return ErrCapacity
	}

	global, err := q.lock("admission")
	if err != nil {
		return err
	}
	defer unlockClose(global)
	lock, err := q.lock(job.DeliveryID)
	if err != nil {
		return err
	}
	defer unlockClose(lock)
	r := QueueStatus{Version: 2, Direction: direction, Job: job, AssociationState: AssociationIdle, AdmissionVersion: 1}
	var journal *os.File
	_, recordErr := q.root.Lstat(queueName(job.DeliveryID + ".journal"))
	if errors.Is(recordErr, os.ErrNotExist) {
		_, recordErr = q.root.Lstat(queueName(job.DeliveryID + ".release"))
	}
	switch {
	case recordErr == nil:
		var existing QueueStatus
		existing, journal, err = q.load(job.DeliveryID)
		if err != nil {
			return err
		}
		defer func() { _ = journal.Close() }()
		if existing.Direction != direction || !sameAdmission(existing.Job, job) || existing.Job.CancelRequested && !existing.Associated {
			return ErrInvalid
		}
		if existing.Admitted {
			return nil
		}
		if existing.AdmissionVersion != 1 {
			return ErrVersion
		}
		r = existing
	case !errors.Is(recordErr, os.ErrNotExist):
		return ErrStore
	default:
		if err := q.capacity(job.Spec.SizeBytes); err != nil {
			return err
		}
		journal, err = q.file(job.DeliveryID+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		defer func() { _ = journal.Close() }()
		if err := q.save(journal, r); err != nil {
			return err
		}
		if err := syncQueueDir(q.root); err != nil {
			return ErrStore
		}
	}
	if direction == "upload" {
		return q.resumeAdmission(ctx, r, journal, source)
	}
	f, err := q.file(job.DeliveryID+".part", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if f.Sync() != nil || syncQueueDir(q.root) != nil {
		return ErrStore
	}
	r.Admitted = true
	return q.save(journal, r)
}

func (q *DiskQueue) admissions() ([]QueueStatus, error) {
	dir, err := q.root.Open(".")
	if err != nil {
		return nil, ErrStore
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(q.limits.Jobs*6 + maxQueueRecords*2 + 136)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) >= q.limits.Jobs*6+maxQueueRecords*2+136 {
		return nil, ErrCapacity
	}
	var records []QueueStatus
	seen := make(map[string]bool)
	for _, e := range entries {
		stem, ok := strings.CutSuffix(e.Name(), ".journal")
		if !ok {
			stem, ok = strings.CutSuffix(e.Name(), ".release")
		}
		if !ok {
			stem, ok = strings.CutSuffix(e.Name(), ".checkpoint")
		}
		if !ok || seen[stem] {
			continue
		}
		if !validID(stem) {
			return nil, ErrStore
		}
		seen[stem] = true
		r, err := q.admissionHead(stem + ".journal")
		if err != nil {
			q.markJournal("admission_after_head_error")
			if checkpoint, checkpointErr := q.activeCheckpointHead(stem + ".checkpoint"); checkpointErr == nil {
				r, err = checkpoint.Record, nil
			} else if errors.Is(err, errAdmissionFrame) {
				// A job-locked compactor can finish between the short/changed
				// first-frame read and witness lookup. If the witness is now
				// absent, reread the journal ONCE; only a fully validated frame
				// restores the reservation. Never reinterpret a persistent
				// corrupt frame, wrong version or unsafe path as empty capacity.
				if _, witnessErr := q.root.Lstat(stem + ".checkpoint"); errors.Is(witnessErr, os.ErrNotExist) {
					q.markJournal("admission_retry_head")
					r, err = q.admissionHead(stem + ".journal")
				}
			}
		}
		if err != nil {
			// Compaction can leave the original journal empty/torn. Only the
			// synced, fully confirmed sibling can recover its original identity.
			r, err = q.admissionHead(stem + ".release")
		}
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// Caller owns the admission lock, excluding creation and terminal release.
// Active compaction holds only the job lock and can replace the first frame;
// admissions reconciles that race without acquiring locks in reverse order.
// Read one bounded frame instead of every journal; this read never repairs it.
func (q *DiskQueue) admissionHead(name string) (QueueStatus, error) {
	var r QueueStatus
	f, err := q.physicalFile(name, os.O_RDONLY, 0o600)
	if err != nil {
		return r, err
	}
	defer func() { _ = f.Close() }()
	var head [36]byte
	_, err = io.ReadFull(f, head[:])
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return r, errAdmissionFrame
	}
	size := int64(binary.BigEndian.Uint32(head[:4]))
	if err != nil || size < 1 || size > 16384 {
		return r, ErrStore
	}
	body := make([]byte, int(size))
	_, err = io.ReadFull(f, body)
	sum := sha256.Sum256(body)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || err == nil && !bytes.Equal(sum[:], head[4:]) {
		return QueueStatus{}, errAdmissionFrame
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err != nil || dec.Decode(&r) != nil || !q.validRecord(r, r.Job.DeliveryID) {
		return QueueStatus{}, ErrStore
	}
	suffix := ".journal"
	if strings.HasSuffix(name, ".release") {
		suffix = ".release"
		st, err := f.Stat()
		if err != nil || st.Size() != size+36 || r.PayloadState != PayloadReleased {
			return QueueStatus{}, ErrStore
		}
	}
	if queueName(r.Job.DeliveryID+suffix) != name {
		return QueueStatus{}, ErrStore
	}
	return r, nil
}

func (q *DiskQueue) capacity(add int64) error {
	records, err := q.admissions()
	if err != nil {
		return err
	}
	total := int64(0)
	active := 0
	for _, r := range records {
		if r.PayloadState == PayloadReleased {
			continue
		}
		active++
		reserved := r.Job.Spec.SizeBytes + snapshotTreeBytes(r.Job.Spec.SizeBytes) + admissionFileBytes
		if reserved > q.limits.Bytes-total {
			return ErrCapacity
		}
		total += reserved
	}
	add += snapshotTreeBytes(add) + admissionFileBytes
	if len(records) >= maxQueueRecords || active >= q.limits.Jobs || add > q.limits.Bytes-total {
		return ErrCapacity
	}
	return queueFreeSpace(q.root, add+total)
}

// Inspect returns bounded metadata only and shares the same process lock.
func (q *DiskQueue) Inspect(id string) (QueueStatus, error) {
	if !validID(id) || id == "admission" {
		return QueueStatus{}, ErrInvalid
	}
	lock, err := q.lock(id)
	if err != nil {
		return QueueStatus{}, err
	}
	defer unlockClose(lock)
	r, f, err := q.load(id)
	if err != nil {
		return QueueStatus{}, err
	}
	defer func() { _ = f.Close() }()
	if !r.Admitted && r.Job.Phase != "canceled" {
		return r, ErrAdmissionPending
	}
	return r, nil
}

// RequestCancel persists intent. An in-flight invocation returns ErrBusy; its
// owner can cancel its context, then persist intent. A ready original is never
// deleted: cancellation suppresses an unassociated outbox reference instead.
func (q *DiskQueue) RequestCancel(id string) error {
	if !validID(id) || id == "admission" {
		return ErrInvalid
	}
	lock, err := q.lock(id)
	if err != nil {
		return err
	}
	defer unlockClose(lock)
	r, f, err := q.load(id)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if r.Associated {
		return ErrInvalid
	}
	if r.Direction == "upload" && (r.Job.Phase == "create_unknown" || r.Job.Phase == "blocked") {
		return ErrBinding
	}
	if r.AssociationState == AssociationInFlight || r.AssociationState == "" && r.AssociationAttempts > 0 {
		r.AssociationState = AssociationUnknown
	}
	if !r.Job.CancelRequested {
		r.AssociationAttempts = 0
		r.AssociationDue = time.Time{}
		r.AssociationBlocked = false
	}
	r.Job.CancelRequested = true
	if !r.Admitted {
		r.Job.Phase = "canceled"
		r.AssociationState = AssociationCanceled
	}
	return q.save(f, r)
}

// RunOne is the finite Herdr action body: one job, one exclusive OS lock and
// a two-minute context budget. Host callbacks must honor that context; OS disk
// calls can outlive it. No sleeps, detached workers or task execution states.
// Host startup/event/manual actions may call again when due; no reliable idle
// retry scheduler is implied. Production auth/association remain injected ports.
func (q *DiskQueue) RunOne(ctx context.Context, id string, client *Client, binding *AssociationBinding, now time.Time) error {
	if !validID(id) || id == "admission" || client == nil {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	worker, err := q.worker()
	if err != nil {
		return err
	}
	defer unlockClose(worker)
	lock, err := q.lock(id)
	if err != nil {
		return err
	}
	defer unlockClose(lock)
	r, journal, err := q.load(id)
	if err != nil {
		return err
	}
	defer func() { _ = journal.Close() }()
	if !r.Admitted {
		return ErrAdmissionPending
	}
	if !r.Associated && r.Direction == "download" && r.Job.CancelRequested && r.Job.Phase != "ready" {
		r.Job.Phase, r.Job.ErrorCode, r.Job.NextAttempt = "canceled", "", time.Time{}
		return q.save(journal, r)
	}
	if r.AssociationState == AssociationInFlight || r.AssociationState == "" && r.AssociationAttempts > 0 && !r.Associated {
		r.AssociationState = AssociationUnknown
		if err := q.save(journal, r); err != nil {
			return err
		}
	}
	if r.Associated || r.Job.Phase == "canceled" || r.Job.Phase == "blocked" || r.Job.Phase == "create_unknown" || now.Before(r.Job.NextAttempt) {
		return nil
	}
	if r.Job.Phase == "ready" {
		if r.Job.CancelRequested && (r.AssociationState == AssociationIdle || r.AssociationState == "") {
			return q.finishAssociation(journal, r, OutcomeCanceled)
		}
		if r.AssociationBlocked || now.Before(r.AssociationDue) {
			return nil
		}
		if binding == nil || binding.Commit == nil || binding.Resolve == nil {
			return ErrBinding
		}
		if r.AssociationState == AssociationUnknown || r.Job.CancelRequested {
			return q.resolveAssociation(ctx, journal, r, binding, now)
		}
	}
	save := func(j Job) error {
		next := r
		next.Job = j
		if err := q.save(journal, next); err != nil {
			return err
		}
		r = next
		return nil
	}
	if r.Job.Phase != "ready" && r.Direction == "upload" {
		if r.Job.CancelRequested {
			return client.Advance(ctx, r.Job, save, nil, now)
		}
		source, closeSource, err := q.openSnapshot(r)
		if err != nil {
			if errors.Is(err, ErrSnapshot) {
				return stop(&r.Job, "blocked", "snapshot_unavailable", save)
			}
			return err
		}
		defer closeSource()
		return client.Advance(ctx, r.Job, save, source, now)
	}
	var content io.Reader
	if r.Direction == "download" {
		if r.Job.CancelRequested {
			return stop(&r.Job, "canceled", "", save)
		}
		f, err := q.file(id+".part", os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		err = client.Download(ctx, r.Job.BotID, r.Job.Spec.SHA256, r.Job.Spec.SizeBytes, r.Job.Offset, f, func(offset int64) error { next := r.Job; next.Offset = offset; return save(next) })
		if err != nil {
			if r.Job.Ready != nil {
				r.AssociationAttempts++
				return q.waitAssociation(journal, r, err, now)
			}
			return fail(&r.Job, err, now, save)
		}
		if r.Job.Phase != "ready" {
			next := r.Job
			next.Phase = "ready"
			next.Attempts = 0
			next.NextAttempt = time.Time{}
			next.ErrorCode = ""
			next.Ready = &Reference{DeliveryID: next.DeliveryID, OperationID: next.OperationID, BotID: next.BotID, ContentHash: next.Spec.SHA256, SizeBytes: next.Spec.SizeBytes, Mime: next.Spec.Mime}
			// Staging stays private. Ready is verified bytes, not publication.
			return save(next)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return ErrStore
		}
		source := &publicationSource{SectionReader: io.NewSectionReader(f, 0, r.Job.Spec.SizeBytes), file: f, queue: q, reference: *r.Job.Ready, active: true}
		defer func() { source.active = false }()
		content = source
	}
	if binding == nil || binding.Commit == nil || binding.Resolve == nil {
		return ErrBinding
	}
	// The registration endpoint itself MUST authorize atomically; an earlier
	// upload/download grant and a local ready bit cannot authorize association.
	return q.commitAssociation(ctx, journal, r, binding, content, now)
}

// Retire releases only acknowledged delivery bytes or confirmed cancellation.
// It never touches NAS originals. Stable striped lock files remain to prevent
// unlink/recreate lock races; failed/unknown jobs require explicit reconciliation.
// Confirmed deliveries keep the compact local receipt as well as the NAS binding.
// The producer must never reuse a delivery ID for different content.
func (q *DiskQueue) Retire(id string) error {
	return q.retire(context.Background(), id)
}

func (q *DiskQueue) retire(ctx context.Context, id string) error {
	if !validID(id) || id == "admission" {
		return ErrInvalid
	}
	global, err := q.lock("admission")
	if err != nil {
		return err
	}
	defer unlockClose(global)
	lock, err := q.lock(id)
	if err != nil {
		return err
	}
	defer unlockClose(lock)
	r, journal, err := q.load(id)
	if err != nil {
		return err
	}
	_ = journal.Close()
	if confirmedDelivery(r) {
		return q.releaseConfirmedLocked(ctx, id)
	}
	if !r.Associated && r.Job.Phase != "canceled" {
		return ErrInvalid
	}
	suffix := ".snapshot"
	if r.Direction == "download" {
		suffix = ".part"
	}
	if err := removeQueueFile(q.root, queueName(id+suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrStore
	}
	if r.Direction == "upload" {
		if err := removeQueueFile(q.root, queueName(id+".tree")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrStore
		}
	}
	if err := removeQueueFile(q.root, queueName(id+".admission")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrStore
	}
	if syncQueueDir(q.root) != nil {
		return ErrStore
	}
	if removeQueueFile(q.root, queueName(id+".journal")) != nil || syncQueueDir(q.root) != nil {
		return ErrStore
	}
	return nil
}
