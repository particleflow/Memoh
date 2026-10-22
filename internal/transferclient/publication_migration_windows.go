//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Publication locator version 0 is a closed maintenance gate, never a normal
// readable/writable format. The record schema and final objects do not change.
func (p *NativePublication) prepareMigration(f *os.File, actual, want []byte) error {
	if bytes.Equal(actual, want) {
		return nil
	}
	const prefix = `{"version":`
	if !bytes.HasPrefix(want, []byte(prefix+"2,")) {
		return ErrVersion
	}
	old, pending := bytes.Clone(want), bytes.Clone(want)
	old[len(prefix)] = '1'
	pending[len(prefix)] = '0'
	if !bytes.Equal(actual, old) && !bytes.Equal(actual, pending) {
		return ErrVersion
	}
	if bytes.Equal(actual, old) {
		if _, err := p.root.Lstat(publicationMetadataDirectory); !errors.Is(err, os.ErrNotExist) {
			return ErrVersion
		}
	}
	flags := os.O_RDWR
	if bytes.Equal(actual, old) {
		flags |= os.O_CREATE
	}
	backup, err := openPrivateFile(p.root, "locator-v1.backup", flags, 0o600)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(backup, 4097))
	if err != nil || len(raw) > len(old) || !bytes.Equal(raw, old[:len(raw)]) || bytes.Equal(actual, pending) && len(raw) != len(old) {
		_ = backup.Close()
		return ErrStore
	}
	if len(raw) < len(old) {
		_, err = backup.Write(old[len(raw):])
	}
	if err == nil {
		err = backup.Sync()
	}
	_ = backup.Close()
	if err != nil || syncQueueDir(p.root) != nil {
		return ErrStore
	}
	p.mark("publication_after_locator_backup")
	if bytes.Equal(actual, old) {
		if _, err = f.WriteAt([]byte{'0'}, int64(len(prefix))); err != nil {
			return ErrStore
		}
	}
	if f.Sync() != nil || syncQueueDir(p.root) != nil {
		return ErrStore
	}
	p.mark("publication_after_migration_gate")
	return nil
}

// MigrateNativePublication is local finite maintenance, not a publication or
// authorization bypass. It never consumes a source or calls the NAS. The host
// must stop ALL old writers and retain a complete raw backup before invocation.
func (q *DiskQueue) MigrateNativePublication(ctx context.Context) (bool, error) {
	if ctx == nil || q == nil || q.root == nil || q.readOnly {
		return false, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// Match existing queue maintenance lock order, without waiting: an active
	// worker may need admission while finishing. Idle old handles still require
	// explicit host quiescence; these locks cannot revoke another process.
	admission, err := q.lock("admission")
	if err != nil {
		return false, err
	}
	defer unlockClose(admission)
	var workers []*os.File
	defer func() {
		for _, worker := range workers {
			unlockClose(worker)
		}
	}()
	for i := range q.limits.Workers {
		worker, err := q.lockName(fmt.Sprintf("worker-%d", i))
		if err != nil {
			return false, err
		}
		workers = append(workers, worker)
	}
	p, err := q.openNativePublication(func(ctx context.Context, _ Reference) error { return ctx.Err() }, true, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = p.Close() }()
	return p.migratePublication(ctx)
}

func (p *NativePublication) migratePublication(ctx context.Context) (bool, error) {
	lock, err := p.lock()
	if err != nil {
		return false, err
	}
	defer unlockClose(lock)
	f, err := openPrivateFile(p.root, "locator", os.O_RDWR, 0o600)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return false, ErrStore
	}
	var locator struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(raw, &locator) != nil {
		return false, ErrStore
	}
	if locator.Version == 2 {
		return true, nil
	}
	if locator.Version != 0 {
		return false, ErrVersion
	}
	done, err := p.metadata.migrateLegacy(ctx)
	if err != nil || !done {
		return done, err
	}
	p.mark("publication_before_locator_commit")
	if _, err := f.WriteAt([]byte{'2'}, int64(len(`{"version":`))); err != nil {
		return false, ErrStore
	}
	if f.Sync() != nil || syncQueueDir(p.root) != nil {
		return false, ErrStore
	}
	p.mark("publication_after_locator_commit")
	return true, nil
}
