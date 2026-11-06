package transferclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
)

// MigrateHerdrQueue is an explicit finite maintenance action. The host must stop
// OLD writers first. Existing roots/limits are validated, not relocated. Normal
// Open never upgrades an old locator or falls back to a different directory.
func MigrateHerdrQueue(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q, err := openQueueLayout(getenv, revision, limits, false, true, nil)
	if err != nil {
		return err
	}
	defer func() { _ = q.Close() }()
	return q.ReclaimConfirmed(ctx)
}

func (q *DiskQueue) migrateLocator(f *os.File, actual, want []byte) error {
	if bytes.Equal(actual, want) {
		if f.Sync() != nil || syncQueueDir(q.root) != nil {
			return ErrStore
		}
		return nil
	}
	// The schema transition changes one version digit only. All existing locator
	// bytes (resolved root, revision and limits) must match, including their exact
	// encoding. No truncate/rename can leave an empty locator on process exit.
	const prefix = `{"Version":`
	if !bytes.HasPrefix(want, []byte(prefix+"3,")) {
		return ErrVersion
	}
	old := bytes.Clone(want)
	old[len(prefix)] = '2'
	if !bytes.Equal(actual, old) {
		return ErrVersion
	}
	// Admission is already held. Do not migrate across an active finite worker.
	// These are nonblocking locks: an old worker that needs admission can finish
	// after we return Busy. Idle old handles still require host quiescence.
	var workers []*os.File
	defer func() {
		for _, worker := range workers {
			unlockClose(worker)
		}
	}()
	for i := range q.limits.Workers {
		worker, err := q.lockName(fmt.Sprintf("worker-%d", i))
		if err != nil {
			return err
		}
		workers = append(workers, worker)
	}
	backup, err := q.file("locator-v2.backup", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(backup, 4097))
	if err != nil || len(raw) > len(old) || !bytes.Equal(raw, old[:len(raw)]) {
		_ = backup.Close()
		return ErrStore
	}
	// A partial backup can only be completed from the still exact original.
	if len(raw) < len(old) {
		_, err = backup.Write(old[len(raw):])
	}
	if err == nil {
		err = backup.Sync()
	}
	_ = backup.Close()
	if err != nil || syncQueueDir(q.root) != nil {
		return ErrStore
	}
	q.markHistory("locator_after_backup")
	n, err := f.WriteAt([]byte{'3'}, int64(len(prefix)))
	if err != nil || n != 1 {
		return ErrStore
	}
	q.markHistory("locator_after_write")
	if f.Sync() != nil || syncQueueDir(q.root) != nil {
		return ErrStore
	}
	q.markHistory("locator_after_sync")
	return nil
}
