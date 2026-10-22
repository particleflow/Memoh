package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
	if direction == "upload" {
		if source == nil {
			return ErrSnapshot
		}
		info, err := source.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != job.Spec.SizeBytes {
			return ErrSnapshot
		}
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
	if err := q.capacity(job.Spec.SizeBytes); err != nil {
		return err
	}
	journal, err := q.file(job.DeliveryID+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = journal.Close() }()
	r := QueueStatus{Version: 1, Direction: direction, Job: job}
	if err := q.save(journal, r); err != nil {
		return err
	}
	if err := syncQueueDir(q.root); err != nil {
		return ErrStore
	}
	// Failed/interrupted admission retains its reservation. Explicit cleanup is
	// needed; never mistake a partial snapshot for an admitted job on restart.
	suffix := ".snapshot"
	if direction == "download" {
		suffix = ".part"
	}
	f, err := q.file(job.DeliveryID+suffix, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if direction == "upload" {
		if _, err := source.Seek(0, io.SeekStart); err != nil {
			return ErrSnapshot
		}
		h := sha256.New()
		remaining := job.Spec.SizeBytes
		buffer := make([]byte, copyBytes)
		for remaining > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := io.ReadFull(source, buffer[:min(int64(len(buffer)), remaining)])
			if err != nil {
				return ErrSnapshot
			}
			if written, err := f.Write(buffer[:n]); err != nil || written != n {
				return ErrStore
			}
			_, _ = h.Write(buffer[:n])
			remaining -= int64(n)
		}
		var extra [1]byte
		if n, err := source.Read(extra[:]); n != 0 || !errors.Is(err, io.EOF) || hex.EncodeToString(h.Sum(nil)) != job.Spec.SHA256 {
			return ErrSnapshot
		}
	}
	if direction == "upload" && f.Chmod(0o400) != nil {
		return ErrStore
	}
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
	entries, err := dir.ReadDir(q.limits.Jobs*3 + 136)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) >= q.limits.Jobs*3+136 {
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
		f, err := q.file(e.Name(), os.O_RDONLY, 0o600)
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
		if err != nil || !bytes.Equal(sum[:], head[4:]) || json.Unmarshal(body, &r) != nil || !q.validRecord(r, id) {
			return ErrStore
		}
		count++
		if r.Job.Spec.SizeBytes > q.limits.Bytes-total {
			return ErrCapacity
		}
		total += r.Job.Spec.SizeBytes
	}
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
	if !r.Admitted {
		return r, ErrSnapshot
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
	r.Job.CancelRequested = true
	return q.save(f, r)
}

type diskSnapshot struct {
	file *os.File
	job  Job
}

func (s diskSnapshot) ReadAt(ctx context.Context, ref, revision string, spec Spec, dst []byte, offset int64) error {
	if ctx.Err() != nil || ref != s.job.SnapshotID || revision != s.job.LocatorRevision || spec != s.job.Spec {
		return ErrSnapshot
	}
	n, err := s.file.ReadAt(dst, offset)
	if err != nil || n != len(dst) {
		return ErrSnapshot
	}
	return nil
}

// RunOne is the finite Herdr action body: one job, one exclusive OS lock and
// a two-minute context budget. Host callbacks must honor that context; OS disk
// calls can outlive it. No sleeps, detached workers or task execution states.
// Host startup/event/manual actions may call again when due; no reliable idle
// retry scheduler is implied. Production auth/association remain injected ports.
func (q *DiskQueue) RunOne(ctx context.Context, id string, client *Client, associate Associate, now time.Time) error {
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
		return ErrSnapshot
	}
	if !r.Associated && r.Direction == "download" && r.Job.CancelRequested && r.Job.Phase != "ready" {
		r.Job.Phase, r.Job.ErrorCode, r.Job.NextAttempt = "canceled", "", time.Time{}
		return q.save(journal, r)
	}
	if r.Associated || r.AssociationBlocked || r.Job.Phase == "canceled" || r.Job.Phase == "blocked" || r.Job.Phase == "create_unknown" || now.Before(r.Job.NextAttempt) || now.Before(r.AssociationDue) {
		return nil
	}
	if r.Job.CancelRequested && r.Job.Phase == "ready" {
		return nil
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
		f, err := q.file(id+".snapshot", os.O_RDONLY, 0o600)
		if err != nil {
			return err
		}
		if err := lockQueueFile(f); err != nil {
			_ = f.Close()
			return err
		}
		defer unlockClose(f)
		// Every reopened handle is hash-verified before network mutation. This
		// deliberately favors correctness; large-file cost must be measured.
		if err := Verify(ctx, f, r.Job.Spec); err != nil {
			if ctx.Err() != nil {
				return fail(&r.Job, ctx.Err(), now, save)
			}
			return stop(&r.Job, "blocked", "snapshot_unavailable", save)
		}
		return client.Advance(ctx, r.Job, save, diskSnapshot{f, r.Job}, now)
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
				return q.associationResult(journal, r, err, now)
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
	if associate == nil {
		return ErrBinding
	}
	// The registration endpoint itself MUST authorize atomically; an earlier
	// upload/download grant and a local ready bit cannot authorize association.
	err = associate(ctx, r.Direction, *r.Job.Ready, content)
	return q.associationResult(journal, r, err, now)
}

func (q *DiskQueue) associationResult(journal *os.File, r QueueStatus, err error, now time.Time) error {
	if err == nil {
		r.Associated = true
	} else {
		r.AssociationAttempts++
		if !errors.Is(err, ErrTransport) || r.AssociationAttempts >= 12 {
			r.AssociationBlocked = true
		} else {
			r.AssociationDue = now.Add(min(time.Second*time.Duration(1<<r.AssociationAttempts), 5*time.Minute))
		}
	}
	return q.save(journal, r)
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
	if err := q.root.Remove(id + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrStore
	}
	if syncQueueDir(q.root) != nil {
		return ErrStore
	}
	if q.root.Remove(id+".journal") != nil || syncQueueDir(q.root) != nil {
		return ErrStore
	}
	return nil
}
