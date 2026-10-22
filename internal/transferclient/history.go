package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	historyDirectory     = "history-v1"
	historyBucketBytes   = 1 << 20
	historyBucketRecords = 1024
	historyReclaimBatch  = 16
)

// Terminal history is the same queue's authoritative record, not another task
// registry. There are 256 deterministic buckets, each bounded by bytes AND files.
// No payload or native publication object is stored or removed here.
func (q *DiskQueue) historyBucket(id string, create bool) (*os.Root, func(), error) {
	sum := sha256.Sum256([]byte(id))
	names := []string{historyDirectory, fmt.Sprintf("%02x", sum[0])}
	parent := q.root
	var roots []*os.Root
	var guards []*os.File
	closeAll := func() {
		for i := len(roots) - 1; i >= 0; i-- {
			closeQueueGuard(guards[i])
			_ = roots[i].Close()
		}
	}
	for _, name := range names {
		if create {
			if err := makeQueueDir(parent, name); err != nil && !errors.Is(err, os.ErrExist) {
				closeAll()
				return nil, nil, err
			}
		}
		info, err := parent.Lstat(name)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
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

func historyFrame(r QueueStatus) ([]byte, error) {
	if !confirmedDelivery(r) || r.PayloadState != PayloadReleased {
		return nil, ErrInvalid
	}
	body, err := json.Marshal(r)
	if err != nil || len(body) > 16384 {
		return nil, ErrStore
	}
	raw := make([]byte, 36+len(body))
	binary.BigEndian.PutUint32(raw[:4], uint32(len(body))) //nolint:gosec // Checked above: frame body is at most 16384 bytes.
	sum := sha256.Sum256(body)
	copy(raw[4:36], sum[:])
	copy(raw[36:], body)
	return raw, nil
}

func (q *DiskQueue) readHistory(id string) ([]byte, *os.File, bool, error) {
	root, closeRoot, err := q.historyBucket(id, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	defer closeRoot()
	flags := os.O_RDWR
	if q.readOnly {
		flags = os.O_RDONLY
	}
	f, err := openPrivateFile(root, queueName(id+".journal"), flags, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, true, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, releaseReceiptBytes+1))
	if err != nil || len(raw) > releaseReceiptBytes {
		_ = f.Close()
		return nil, nil, true, ErrStore
	}
	if !q.readOnly && (f.Sync() != nil || syncQueueDir(root) != nil) {
		_ = f.Close()
		return nil, nil, true, ErrStore
	}
	return raw, f, true, nil
}

func (q *DiskQueue) decodeHistory(id string, raw []byte) (QueueStatus, error) {
	var r QueueStatus
	if len(raw) < 36 || int(binary.BigEndian.Uint32(raw[:4])) != len(raw)-36 {
		return r, ErrStore
	}
	sum := sha256.Sum256(raw[36:])
	if !bytes.Equal(raw[4:36], sum[:]) {
		return r, ErrStore
	}
	decoder := json.NewDecoder(bytes.NewReader(raw[36:]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil || decoder.Decode(new(any)) != io.EOF || !q.validRecord(r, id) || !confirmedDelivery(r) || r.PayloadState != PayloadReleased {
		return QueueStatus{}, ErrStore
	}
	return r, nil
}

// Job-locked readers never let history hide a conflicting or corrupt active
// record. A partial archive is recoverable only while the complete original
// exists and every visible archive byte matches that exact original frame.
func (q *DiskQueue) load(id string) (QueueStatus, *os.File, error) {
	active, source, activeErr := q.loadActive(id)
	raw, history, found, err := q.readHistory(id)
	if err != nil {
		if source != nil {
			_ = source.Close()
		}
		return QueueStatus{}, nil, err
	}
	if !found {
		return active, source, activeErr
	}
	if activeErr == nil {
		defer func() { _ = history.Close() }()
		frame, err := historyFrame(active)
		if err != nil || len(raw) > len(frame) || !bytes.Equal(raw, frame[:len(raw)]) {
			_ = source.Close()
			return QueueStatus{}, nil, ErrStore
		}
		return active, source, nil
	}
	if !errors.Is(activeErr, os.ErrNotExist) {
		_ = history.Close()
		return QueueStatus{}, nil, activeErr
	}
	record, err := q.decodeHistory(id, raw)
	if err != nil {
		_ = history.Close()
		return QueueStatus{}, nil, err
	}
	return record, history, nil
}

// Caller owns admission then the permanent job stripe. Complete history is
// synced before unlinking the active journal; repeated calls reconcile both
// copies without changing the task/attempt/reference/receipt/seal.
func (q *DiskQueue) archiveConfirmedLocked(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, source, err := q.load(id)
	if err != nil {
		return err
	}
	_ = source.Close()
	frame, err := historyFrame(r)
	if err != nil {
		return err
	}
	for _, suffix := range []string{".snapshot", ".tree", ".admission", ".part", ".release", ".checkpoint"} {
		if _, err := q.root.Lstat(queueName(id + suffix)); !errors.Is(err, os.ErrNotExist) {
			return ErrStore
		}
	}
	root, closeRoot, err := q.historyBucket(id, true)
	if err != nil {
		return err
	}
	defer closeRoot()
	name := queueName(id + ".journal")
	if _, err := q.root.Lstat(name); errors.Is(err, os.ErrNotExist) {
		if syncQueueDir(root) != nil || syncQueueDir(q.root) != nil {
			return ErrStore
		}
		return nil
	} else if err != nil {
		return ErrStore
	}
	dir, err := root.Open(".")
	if err != nil {
		return ErrStore
	}
	entries, readErr := dir.ReadDir(historyBucketRecords + 1)
	_ = dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(entries) > historyBucketRecords {
		return ErrCapacity
	}
	var total int64
	count := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".journal") || !e.Type().IsRegular() {
			return ErrStore
		}
		info, err := e.Info()
		if err != nil || info.Size() > releaseReceiptBytes {
			return ErrStore
		}
		if e.Name() != name {
			total += info.Size()
			count++
		}
	}
	if count >= historyBucketRecords || int64(len(frame)) > historyBucketBytes-total {
		return ErrCapacity
	}
	if err := queueFreeSpace(root, int64(len(frame))); err != nil {
		return err
	}
	target, err := openPrivateFile(root, name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(target, releaseReceiptBytes+1))
	if err != nil || len(raw) > len(frame) || !bytes.Equal(raw, frame[:len(raw)]) {
		_ = target.Close()
		return ErrStore
	}
	// A crash during an unfinished target write keeps the authoritative source.
	// Complete malformed/conflicting records never enter this recovery path.
	if len(raw) < len(frame) {
		if _, err = target.Write(frame[len(raw):]); err == nil {
			q.markHistory("before_target_sync")
			err = target.Sync()
		}
	} else {
		err = target.Sync()
	}
	_ = target.Close()
	if err != nil || syncQueueDir(root) != nil {
		return ErrStore
	}
	q.markHistory("after_target_sync")
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := removeQueueFile(q.root, queueName(id+".journal")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrStore
	}
	q.markHistory("after_source_remove")
	if err := syncQueueDir(q.root); err != nil {
		return ErrStore
	}
	q.markHistory("after_source_sync")
	return nil
}

func (q *DiskQueue) markHistory(stage string) {
	if q.historyHook != nil {
		q.historyHook(stage)
	}
}
