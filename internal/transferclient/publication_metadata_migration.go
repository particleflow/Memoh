package transferclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
)

// Only locator state 0 (explicit migration) may call this. All old journals
// remain until every destination is durable and its usage can be validated.
// A finite call copies or removes at most 16 journals; no object is moved.
func (m *publicationMetadata) migrateLegacy(ctx context.Context) (bool, error) {
	dir, err := m.root.Open(".")
	if err != nil {
		return false, ErrStore
	}
	entries, readErr := dir.ReadDir(publicationReceipts*2 + 8)
	_ = dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(entries) >= publicationReceipts*2+8 {
		return false, ErrCapacity
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".journal") {
			if !e.Type().IsRegular() {
				return false, ErrStore
			}
			names = append(names, e.Name())
		}
	}
	if len(names) > publicationReceipts {
		return false, ErrCapacity
	}
	sort.Strings(names)
	copied := 0
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		changed, err := m.copyLegacy(ctx, name)
		if err != nil {
			return false, err
		}
		if changed {
			copied++
			if copied >= publicationMigrationBatch {
				return false, nil
			}
		}
	}
	// Cache recovery is safe only after ALL partial targets have been completed
	// from their original journals. This avoids one partial target blocking a
	// different valid duplicate in the same bucket on a resumed invocation.
	if err := m.initialize(ctx, false); err != nil {
		return false, err
	}
	var count int
	var total int64
	for i := range publicationBuckets {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		root, closeRoot, err := m.bucket(i, false)
		if errors.Is(err, os.ErrNotExist) {
			return false, ErrStore
		}
		if err != nil {
			return false, err
		}
		u, err := m.usage(ctx, root, i)
		if err == nil {
			err = checkPublicationMembership(root, u.Keys)
		}
		closeRoot()
		if err != nil {
			return false, err
		}
		count += u.Count
		if !m.fitsObjectBytes(total, u.Bytes) {
			return false, ErrCapacity
		}
		total += u.Bytes
	}
	if count > publicationReceipts {
		return false, ErrCapacity
	} // The legacy store could not own more.
	for i, name := range names {
		if i >= publicationMigrationBatch {
			return false, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		m.mark("publication_before_legacy_remove")
		if err := removeQueueFile(m.root, name); err != nil {
			return false, ErrStore
		}
		m.mark("publication_after_legacy_remove")
		if syncQueueDir(m.root) != nil {
			return false, ErrStore
		}
	}
	return true, nil
}

func (m *publicationMetadata) copyLegacy(ctx context.Context, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	index, err := publicationBucket(name)
	if err != nil {
		return false, err
	}
	_, source, err := readPublicationJournal(m.root, name, false)
	if err != nil || source == nil {
		return false, ErrStore
	}
	_, err = source.Seek(0, io.SeekStart)
	var raw []byte
	if err == nil {
		raw, err = io.ReadAll(io.LimitReader(source, publicationJournalBytes+1))
	}
	_ = source.Close()
	if err != nil || len(raw) > publicationJournalBytes {
		return false, ErrStore
	}
	root, closeRoot, err := m.bucket(index, true)
	if err != nil {
		return false, err
	}
	defer closeRoot()
	target, err := openPrivateFile(root, name, os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		if err := m.dirty(root, index, true); err != nil {
			return false, err
		}
		target, err = openPrivateFile(root, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = target.Close() }()
	saved, err := io.ReadAll(io.LimitReader(target, publicationJournalBytes+1))
	if err != nil || len(saved) > len(raw) || !bytes.Equal(saved, raw[:len(saved)]) {
		return false, ErrPublicationConflict
	}
	changed := len(saved) < len(raw)
	if changed {
		if err := m.dirty(root, index, true); err != nil {
			return false, err
		}
		if _, err := target.Write(raw[len(saved):]); err != nil {
			return false, ErrStore
		}
		m.mark("publication_before_target_sync")
	}
	if target.Sync() != nil || syncQueueDir(root) != nil {
		return false, ErrStore
	}
	if changed {
		m.mark("publication_after_target_sync")
	}
	return changed, nil
}
