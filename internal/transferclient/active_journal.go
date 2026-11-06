package transferclient

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
)

var errCheckpointIncomplete = errors.New("incomplete queue checkpoint")

const activeCheckpointBodyBytes = 16384 + 1024 // Record plus bounded object-identity envelope.

type activeCheckpoint struct {
	Version int         `json:"version"`
	FileID  string      `json:"file_id"`
	Record  QueueStatus `json:"record"`
}

// The caller holds the existing per-job process lock. A synced v4 journal
// record precedes this witness, so old readers fail closed before any truncate.
// No payload, receipt identity, retry budget or association evidence is removed.
func (q *DiskQueue) compactActiveJournal(journal *os.File, r QueueStatus) error {
	if (r.Version != 4 && r.Version != 5) || r.PayloadState != "" || !q.validRecord(r, r.Job.DeliveryID) {
		return ErrInvalid
	}
	q.markJournal("after_record")
	identity, err := queueObjectIdentity(journal)
	if err != nil {
		return err
	}
	witness := activeCheckpoint{Version: 1, FileID: identity, Record: r}
	body, err := json.Marshal(witness)
	if err != nil || len(body) > activeCheckpointBodyBytes {
		return ErrStore
	}
	if err := queueFreeSpace(q.root, int64(len(body)+36)); err != nil {
		return err
	}
	f, err := q.file(r.Job.DeliveryID+".checkpoint", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return ErrStore
	}
	err = appendBoundedFrame(f, body, activeCheckpointBodyBytes+36, activeCheckpointBodyBytes)
	_ = f.Close()
	if err != nil {
		return err
	}
	if err := syncQueueDir(q.root); err != nil {
		return err
	}
	q.markJournal("after_checkpoint")
	return q.finishActiveCheckpoint(journal, witness)
}

func (q *DiskQueue) finishActiveCheckpoint(journal *os.File, witness activeCheckpoint) error {
	body, err := json.Marshal(witness.Record)
	if err != nil || len(body) > 16384 {
		return ErrStore
	}
	identity, err := queueObjectIdentity(journal)
	if err != nil || identity != witness.FileID {
		return ErrStore
	}
	if journal.Truncate(0) != nil || journal.Sync() != nil {
		return ErrStore
	}
	q.markJournal("after_truncate")
	if err := appendJournalFrame(journal, body, q.limits.JournalBytes); err != nil {
		return err
	}
	q.markJournal("after_compact")
	if err := q.removeActiveCheckpoint(witness.Record.Job.DeliveryID); err != nil {
		return err
	}
	q.markJournal("after_finish")
	return nil
}

func (q *DiskQueue) removeActiveCheckpoint(id string) error {
	if err := removeQueueFile(q.root, queueName(id+".checkpoint")); err != nil {
		return ErrStore
	}
	return syncQueueDir(q.root)
}

// Used by bounded admission enumeration too: a valid witness keeps the same
// reservation visible while the original journal is temporarily empty. Names
// here are physical directory entries; Windows IDs are never decoded as paths.
func (q *DiskQueue) activeCheckpointHead(name string) (activeCheckpoint, error) {
	var witness activeCheckpoint
	if _, err := q.root.Lstat(name); err != nil {
		return witness, err
	}
	f, err := q.physicalFile(name, os.O_RDONLY, 0o600)
	if err != nil {
		return witness, err
	}
	defer func() { _ = f.Close() }()
	var head [36]byte
	if _, err := io.ReadFull(f, head[:]); errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return witness, errCheckpointIncomplete
	} else if err != nil {
		return witness, ErrStore
	}
	length := int64(binary.BigEndian.Uint32(head[:4]))
	if length < 1 || length > activeCheckpointBodyBytes {
		return witness, ErrStore
	}
	body := make([]byte, int(length))
	if _, err := io.ReadFull(f, body); errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return witness, errCheckpointIncomplete
	} else if err != nil {
		return witness, ErrStore
	}
	stat, err := f.Stat()
	sum := sha256.Sum256(body)
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err != nil || stat.Size() != length+36 || !bytes.Equal(sum[:], head[4:]) || decoder.Decode(&witness) != nil || decoder.Decode(new(any)) != io.EOF || witness.Version != 1 || len(witness.FileID) == 0 || len(witness.FileID) > 128 {
		return activeCheckpoint{}, ErrStore
	}
	id := witness.Record.Job.DeliveryID
	recordBody, recordErr := json.Marshal(witness.Record)
	if recordErr != nil || len(recordBody) > 16384 || (witness.Record.Version != 4 && witness.Record.Version != 5) || witness.Record.PayloadState != "" || !q.validRecord(witness.Record, id) || queueName(id+".checkpoint") != name {
		return activeCheckpoint{}, ErrStore
	}
	journal, err := q.file(id+".journal", os.O_RDONLY, 0o600)
	if err != nil {
		return activeCheckpoint{}, ErrStore
	}
	defer func() { _ = journal.Close() }()
	identity, err := queueObjectIdentity(journal)
	if err != nil || identity != witness.FileID {
		return activeCheckpoint{}, ErrStore
	}
	return witness, nil
}

func (q *DiskQueue) recoverActiveJournal(id string) (QueueStatus, *os.File, bool, error) {
	witness, err := q.activeCheckpointHead(queueName(id + ".checkpoint"))
	if errors.Is(err, os.ErrNotExist) {
		return QueueStatus{}, nil, false, nil
	}
	if errors.Is(err, errCheckpointIncomplete) {
		// A partial witness cannot authorize truncate. The entire original v4
		// journal must still validate before discarding only this partial file.
		r, f, readErr := q.loadJournal(id)
		if readErr != nil {
			return QueueStatus{}, nil, true, readErr
		}
		if r.Version != 4 && r.Version != 5 {
			_ = f.Close()
			return QueueStatus{}, nil, true, ErrStore
		}
		if err := q.removeActiveCheckpoint(id); err != nil {
			_ = f.Close()
			return QueueStatus{}, nil, true, err
		}
		return r, f, true, nil
	}
	if err != nil {
		return QueueStatus{}, nil, true, err
	}
	f, err := q.file(id+".journal", os.O_RDWR, 0o600)
	if err != nil {
		return QueueStatus{}, nil, true, err
	}
	if err := q.finishActiveCheckpoint(f, witness); err != nil {
		_ = f.Close()
		return QueueStatus{}, nil, true, err
	}
	return witness.Record, f, true, nil
}

func (q *DiskQueue) markJournal(stage string) {
	if q.journalHook != nil {
		q.journalHook(stage)
	}
}
