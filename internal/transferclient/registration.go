package transferclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"
)

// QueuedSource is private transfer metadata under the existing locator. Ref is
// a validated logical name, never a new root or an arbitrary filesystem path.
// Identity and Modified detect replacement/change before resumed indexing. The
// complete hash and per-block proofs remain authoritative for transferred bytes.
type QueuedSource struct {
	Ref      string `json:"ref"`
	Identity string `json:"identity"`
	Modified int64  `json:"modified"`
}

func (s *QueuedSource) valid() bool {
	return s != nil && validSourceRef(s.Ref) && len(s.Identity) > 0 && len(s.Identity) <= 128
}

func recordVersion(r QueueStatus) int {
	if r.Recovery != nil {
		return 6 // Fence writers that cannot retain/revalidate the original mapping.
	}
	if r.Registered {
		return 5
	}
	return 4
}

func describeSource(ref string, f *os.File, size int64) (*QueuedSource, error) {
	if !validSourceRef(ref) || f == nil {
		return nil, ErrSnapshot
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return nil, ErrSnapshot
	}
	identity, err := queueFileIdentity(f)
	if err != nil {
		return nil, err
	}
	return &QueuedSource{Ref: ref, Identity: identity, Modified: info.ModTime().UnixNano()}, nil
}

// RegisterUpload acknowledges only durable metadata. It does not read/hash/copy
// content, reserve the declared file size, or consume an active transfer slot.
// Existing snapshot journals are replayed unchanged, including after source loss.
func (q *DiskQueue) RegisterUpload(ctx context.Context, job Job, ref string, open func(string) (*os.File, error)) (QueueStatus, error) {
	return q.registerUpload(ctx, job, ref, open, nil)
}

func (q *DiskQueue) registerUpload(ctx context.Context, job Job, ref string, open func(string) (*os.File, error), recovery *ResultSourceMapping) (QueueStatus, error) {
	if ctx.Err() != nil {
		return QueueStatus{}, ctx.Err()
	}
	if !job.valid() || job.Phase != "queued" || job.Offset != 0 || job.ReceiptID != "" || job.CancelRequested || job.Attempts != 0 || job.SnapshotID != job.DeliveryID || job.LocatorRevision != q.revision || job.DeliveryID == "admission" || !validSourceRef(ref) || open == nil {
		return QueueStatus{}, ErrInvalid
	}
	global, err := q.lock("admission")
	if err != nil {
		return QueueStatus{}, err
	}
	defer unlockClose(global)
	lock, err := q.lock(job.DeliveryID)
	if err != nil {
		return QueueStatus{}, err
	}
	defer unlockClose(lock)
	r, journal, err := q.load(job.DeliveryID)
	if err == nil {
		_ = journal.Close()
		if r.Direction != "upload" || !sameAdmission(r.Job, job) || r.Registered && r.Source.Ref != ref || !sameResultMapping(r.Recovery, recovery) {
			return QueueStatus{}, ErrInvalid
		}
		return r, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return QueueStatus{}, err
	}
	f, err := open(ref)
	if err != nil {
		return QueueStatus{}, err
	}
	source, err := describeSource(ref, f, job.Spec.SizeBytes)
	_ = f.Close()
	if err != nil {
		return QueueStatus{}, err
	}
	if recovery != nil && !recovery.validFor(job, source) {
		return QueueStatus{}, ErrBinding
	}
	r = QueueStatus{Version: 5, Recovery: recovery, Registered: true, Direction: "upload", Job: job, Source: source, AdmissionVersion: 1, AssociationState: AssociationIdle}
	r.Version = recordVersion(r)
	body, err := json.Marshal(r)
	if err != nil {
		return QueueStatus{}, ErrStore
	}
	if err := queueFreeSpace(q.root, int64(len(body)+36)); err != nil {
		return QueueStatus{}, err
	}
	journal, err = q.file(job.DeliveryID+".journal", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return QueueStatus{}, err
	}
	defer func() { _ = journal.Close() }()
	if err := q.save(journal, r); err != nil {
		return QueueStatus{}, err
	}
	if err := syncQueueDir(q.root); err != nil {
		return QueueStatus{}, err
	}
	q.markJournal("registration_synced")
	return r, nil
}

func (q *DiskQueue) registeredSource(r QueueStatus) (*os.File, func(), error) {
	if !r.Registered || !r.Source.valid() || q.openSource == nil {
		return nil, nil, ErrBinding
	}
	var f *os.File
	var err error
	if r.Recovery != nil {
		if q.openRecoverySource == nil {
			return nil, nil, ErrBinding
		}
		f, err = q.openRecoverySource(*r.Recovery)
	} else {
		f, err = q.openSource(r.Source.Ref)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, errSourceMissing
		}
		return nil, nil, errors.Join(ErrSnapshot, err)
	}
	stable, release, err := guardQueueSource(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	closeAll := func() { release(); _ = f.Close() }
	actual, err := describeSource(r.Source.Ref, stable, r.Job.Spec.SizeBytes)
	if err != nil || *actual != *r.Source {
		closeAll()
		return nil, nil, ErrSnapshot
	}
	return stable, closeAll, nil
}

var errSourceMissing = errors.New("registered source missing")

// Called under the normal worker and job locks. Only active index scratch is
// allocated; the original remains in place. The established resumable hash/tree
// protocol handles finite invocation limits without rescanning confirmed prefixes.
func (q *DiskQueue) prepareRegistered(ctx context.Context, r QueueStatus, journal *os.File, now time.Time) (QueueStatus, error) {
	if !r.Registered || r.Admitted {
		return r, nil
	}
	r.LastRun = now.UTC()
	if r.Job.CancelRequested || r.Job.Phase == "blocked" || r.Job.Phase == "canceled" {
		return r, nil
	}
	f, closeSource, err := q.registeredSource(r)
	if err != nil {
		if errors.Is(err, errSourceMissing) || errors.Is(err, ErrSnapshot) {
			r.Job.Phase, r.Job.ErrorCode = "blocked", "source_changed"
			if errors.Is(err, errSourceMissing) {
				r.Job.ErrorCode = "source_missing"
			}
			return r, q.save(journal, r)
		}
		return r, err
	}
	defer closeSource()
	if !r.Activated {
		if err := queueFreeSpace(q.root, snapshotTreeBytes(r.Job.Spec.SizeBytes)+admissionFileBytes); err != nil {
			return r, err
		}
		r.Activated = true
		if err := q.save(journal, r); err != nil {
			return r, err
		}
	}
	err = q.resumeAdmission(ctx, r, journal, f)
	if errors.Is(err, ErrSnapshot) {
		r.Job.Phase, r.Job.ErrorCode = "blocked", "source_changed"
		return r, q.save(journal, r)
	}
	if err != nil {
		return r, err
	}
	// resumeAdmission persists the seal before returning success.
	next, fstate, err := q.load(r.Job.DeliveryID)
	if fstate != nil {
		_ = fstate.Close()
	}
	return next, err
}

// Retain the existing wire hash requirement. This first increment does not add
// a new NAS protocol or claim that indexing can avoid its full source read.
func sameSourceAfterRead(r QueueStatus, f *os.File) error {
	if !r.Registered {
		return nil
	}
	actual, err := describeSource(r.Source.Ref, f, r.Job.Spec.SizeBytes)
	if err != nil || *actual != *r.Source {
		return ErrSnapshot
	}
	return nil
}
