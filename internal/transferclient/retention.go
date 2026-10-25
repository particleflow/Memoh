package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
)

const (
	PayloadReleasePending = "release_pending"
	PayloadReleased       = "released"
	// Active or not-yet-archived records keep the original bounded catalog.
	// Confirmed released history has a separate finite budget in history.go.
	maxQueueRecords     = 1024
	releaseReceiptBytes = 16384 + 36
)

func confirmedDelivery(r QueueStatus) bool {
	if !r.Admitted || !r.Associated || r.AssociationBlocked || r.AssociationState != AssociationCommitted || r.Job.Phase != "ready" || r.Job.Ready == nil {
		return false
	}
	want := Reference{DeliveryID: r.Job.DeliveryID, OperationID: r.Job.OperationID, BotID: r.Job.BotID, ContentHash: r.Job.Spec.SHA256, SizeBytes: r.Job.Spec.SizeBytes, Mime: r.Job.Spec.Mime}
	return *r.Job.Ready == want
}

// ReleaseConfirmed deletes ONLY this queue's cache files after durable NAS
// association. It retains the exact terminal job/receipt in this queue's history.
// User sources and native published destination objects are never addressed.
func (q *DiskQueue) ReleaseConfirmed(ctx context.Context, id string) error {
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
	return q.releaseConfirmedLocked(ctx, id)
}

// ReclaimConfirmed recovers interrupted release and frees acknowledged jobs
// within the existing finite host invocation. Active/unknown/canceled jobs stay.
func (q *DiskQueue) ReclaimConfirmed(ctx context.Context) error {
	global, err := q.lock("admission")
	if err != nil {
		return err
	}
	defer unlockClose(global)
	var candidates []string
	finished := errors.New("reclaim batch complete")
	err = q.visitAdmissions(func(head QueueStatus) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(candidates) >= historyReclaimBatch {
			return finished
		}
		// Registered upload completion already compacts its head before release.
		if head.Registered && !head.Associated {
			return nil
		}
		id := head.Job.DeliveryID
		lock, err := q.lock(id)
		if errors.Is(err, ErrBusy) {
			return nil
		}
		if err != nil {
			return err
		}
		r, f, err := q.load(id)
		if err == nil {
			_ = f.Close()
			if confirmedDelivery(r) {
				candidates = append(candidates, id)
			}
		}
		unlockClose(lock)
		return err
	})
	if err != nil && !errors.Is(err, finished) {
		return err
	}
	// Finish bounded directory enumeration before removing any entry. A page
	// may contain both a journal and its release witness after process death;
	// removing the witness mid-page would make its later visit look corrupt.
	// Recheck confirmation under the same admission -> job lock order.
	for _, id := range candidates {
		lock, err := q.lock(id)
		if errors.Is(err, ErrBusy) {
			continue
		}
		if err != nil {
			return err
		}
		err = q.releaseConfirmedLocked(ctx, id)
		unlockClose(lock)
		if err != nil {
			return err
		}
	}
	return nil
}

func (q *DiskQueue) releaseConfirmedLocked(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, journal, err := q.load(id)
	if err != nil {
		return err
	}
	if !confirmedDelivery(r) {
		_ = journal.Close()
		return ErrInvalid
	}
	if r.PayloadState == PayloadReleased {
		if _, err := q.root.Lstat(queueName(id + ".release")); errors.Is(err, os.ErrNotExist) {
			if err := journal.Close(); err != nil {
				return err
			}
			return q.archiveConfirmedLocked(ctx, id)
		} else if err != nil {
			_ = journal.Close()
			return ErrStore
		}
	}
	if r.PayloadState == "" {
		r.Version, r.PayloadState = 3, PayloadReleasePending
		if err = q.save(journal, r); err != nil {
			_ = journal.Close()
			return err
		}
	}
	_ = journal.Close()
	q.markRelease("after_intent")
	if err := ctx.Err(); err != nil {
		return err
	}
	// These are queue-owned layout names. Never use SourceRef or publication paths.
	for _, suffix := range []string{".snapshot", ".tree", ".admission", ".part"} {
		if err = removeQueueFile(q.root, queueName(id+suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrStore
		}
	}
	if err = syncQueueDir(q.root); err != nil {
		return err
	}
	q.markRelease("after_payload")
	if err := ctx.Err(); err != nil {
		return err
	}
	r.Version, r.PayloadState = 3, PayloadReleased
	// A bounded sibling is a crash witness for in-place journal compaction. This
	// reuses existing private files, checksummed frames, file sync and directory
	// sync; it needs no Windows replacement rename or separate state service.
	receipt, err := q.file(id+".release", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	n := 0
	_, err = readJournalFrames(receipt, releaseReceiptBytes, func(raw []byte) error {
		var saved QueueStatus
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if dec.Decode(&saved) != nil || !q.validRecord(saved, id) || saved.PayloadState != PayloadReleased || !sameAdmission(saved.Job, r.Job) || saved.Job.ReceiptID != r.Job.ReceiptID {
			return ErrStore
		}
		n++
		if n != 1 {
			return ErrStore
		}
		return nil
	})
	if err == nil && n == 0 {
		err = q.saveReceipt(receipt, r)
	}
	if err == nil {
		err = syncQueueDir(q.root)
	}
	_ = receipt.Close()
	if err != nil {
		return err
	}
	q.markRelease("after_receipt")
	if err := ctx.Err(); err != nil {
		return err
	}
	journal, err = q.file(id+".journal", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err = journal.Truncate(0); err == nil {
		err = journal.Sync()
	}
	if err != nil {
		_ = journal.Close()
		return ErrStore
	}
	q.markRelease("after_truncate")
	err = q.saveReceipt(journal, r)
	_ = journal.Close()
	if err != nil {
		return err
	}
	q.markRelease("after_compact")
	if err = removeQueueFile(q.root, queueName(id+".release")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrStore
	}
	if err = syncQueueDir(q.root); err != nil {
		return err
	}
	q.markRelease("after_finish")
	return q.archiveConfirmedLocked(ctx, id)
}

func (q *DiskQueue) saveReceipt(f *os.File, r QueueStatus) error {
	if !q.validRecord(r, r.Job.DeliveryID) || r.PayloadState != PayloadReleased {
		return ErrInvalid
	}
	r.Version = recordVersion(r)
	raw, err := json.Marshal(r)
	if err != nil {
		return ErrStore
	}
	return appendJournalFrame(f, raw, releaseReceiptBytes)
}

func (q *DiskQueue) loadReleaseReceipt(id string) (QueueStatus, *os.File, error) {
	var r QueueStatus
	flags := os.O_RDWR
	if q.readOnly {
		flags = os.O_RDONLY
	}
	f, err := q.file(id+".release", flags, 0o600)
	if err != nil {
		return r, nil, err
	}
	count := 0
	_, err = readJournalFramesMode(f, releaseReceiptBytes, !q.readOnly, func(raw []byte) error {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if dec.Decode(&r) != nil || !q.validRecord(r, id) || r.PayloadState != PayloadReleased {
			return ErrStore
		}
		count++
		return nil
	})
	if err != nil || count != 1 {
		_ = f.Close()
		return QueueStatus{}, nil, ErrStore
	}
	return r, f, nil
}

func (q *DiskQueue) markRelease(stage string) {
	if q.releaseHook != nil {
		q.releaseHook(stage)
	}
}
