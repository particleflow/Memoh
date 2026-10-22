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
	if _, err := q.root.Lstat(queueName(job.DeliveryID + ".journal")); err == nil {
		var existing QueueStatus
		existing, journal, err = q.load(job.DeliveryID)
		if err != nil {
			return err
		}
		defer func() { _ = journal.Close() }()
		if existing.Direction != direction || !sameAdmission(existing.Job, job) || existing.Job.CancelRequested {
			return ErrInvalid
		}
		if existing.Admitted {
			return nil
		}
		if existing.AdmissionVersion != 1 {
			return ErrVersion
		}
		r = existing
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrStore
	} else {
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

func (q *DiskQueue) capacity(add int64) error {
	dir, err := q.root.Open(".")
	if err != nil {
		return ErrStore
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(q.limits.Jobs*5 + 136)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) >= q.limits.Jobs*5+136 {
		return ErrCapacity
	}
	count, total := 0, int64(0)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".journal") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".journal")
		if !validID(id) {
			return ErrStore
		}
		f, err := q.physicalFile(e.Name(), os.O_RDONLY, 0o600)
		if err != nil {
			return err
		}
		var head [36]byte
		_, err = io.ReadFull(f, head[:])
		size := int64(binary.BigEndian.Uint32(head[:4]))
		if err != nil || size < 1 || size > 16384 {
			_ = f.Close()
			return ErrStore
		}
		body := make([]byte, int(size))
		_, err = io.ReadFull(f, body)
		_ = f.Close()
		sum := sha256.Sum256(body)
		var r QueueStatus
		if err != nil || !bytes.Equal(sum[:], head[4:]) || json.Unmarshal(body, &r) != nil || !q.validRecord(r, r.Job.DeliveryID) || queueName(r.Job.DeliveryID+".journal") != e.Name() {
			return ErrStore
		}
		count++
		reserved := r.Job.Spec.SizeBytes + snapshotTreeBytes(r.Job.Spec.SizeBytes) + admissionFileBytes
		if reserved > q.limits.Bytes-total {
			return ErrCapacity
		}
		total += reserved
	}
	add += snapshotTreeBytes(add) + admissionFileBytes
	if count >= q.limits.Jobs || add > q.limits.Bytes-total {
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
		content = io.LimitReader(f, r.Job.Spec.SizeBytes)
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
// The producer must never reuse a delivery ID for different content; the host's
// registration endpoint retains the durable idempotency binding after retirement.
func (q *DiskQueue) Retire(id string) error {
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
