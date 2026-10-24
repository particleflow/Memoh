//go:build windows

package transferclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

func publicationLocator(identity, revision string, version int, budget int64) []byte {
	raw, _ := json.Marshal(struct {
		Version  int    `json:"version"`
		Identity string `json:"identity"`
		Revision string `json:"revision"`
		Bytes    int64  `json:"bytes"`
		Receipts int    `json:"receipts"`
	}{version, identity, revision, budget, publicationReceipts})
	return raw
}

// Only the locator gate changes. Existing objects, per-bucket usage, history
// and cancel tombstones stay in place. The queue budget journal becomes the
// authoritative byte ceiling; publication accounting remains independent.
func (q *DiskQueue) migratePublicationBudget(commit bool) error {
	if _, err := q.root.Lstat(publicationDirectory); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return ErrStore
	}
	root, err := q.root.OpenRoot(publicationDirectory)
	if err != nil {
		return ErrStore
	}
	p := &NativePublication{queue: q, root: root}
	defer func() { _ = p.Close() }()
	p.guard, err = guardQueueRoot(root)
	if err != nil {
		return err
	}
	lock, err := p.lock()
	if err != nil {
		return err
	}
	defer unlockClose(lock)
	identity, err := queueObjectIdentity(p.guard)
	if err != nil {
		return err
	}
	want := publicationLocator(identity, q.revision, 3, q.budgetBase)
	old := publicationLocator(identity, q.revision, 2, q.budgetBase)
	f, err := openPrivateFile(root, "locator", os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	actual, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || !bytes.Equal(actual, old) && !bytes.Equal(actual, want) {
		return ErrVersion // Legacy v1 must finish its existing metadata migration.
	}
	if !commit {
		return nil
	}
	if _, err := f.WriteAt([]byte{'3'}, int64(len(`{"version":`))); err != nil || f.Sync() != nil || syncQueueDir(root) != nil {
		return ErrStore
	}
	return nil
}
