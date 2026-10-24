package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
)

const budgetJournalBytes = 64 << 10

type budgetRecord struct {
	Version int      `json:"version"`
	Base    [32]byte `json:"base"`
	Bytes   int64    `json:"bytes"`
}

// MigrateHerdrBudget raises only the byte budget, preserving the original
// locator, job IDs, sources, receipts and publication objects. Stop ALL old
// writers and back up the complete layout first. Locks exclude active finite
// workers, not idle processes holding old handles. Decreases are not implicit.
func MigrateHerdrBudget(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q, err := openQueueLayoutMode(getenv, revision, limits, false, false, true, nil)
	if q != nil {
		_ = q.Close()
	}
	return err
}

// Called under the original admission lock. A one-byte v3->v4 gate rejects old
// writers. The original locator bytes stay intact; a bounded journal reuses the
// existing length/hash/fsync framing for monotonic budget increases. An intact
// frame is the commit point; a torn tail is never a new acknowledged budget.
func (q *DiskQueue) openBudgetLocator(f *os.File, actual, want []byte, migrate bool) (bool, error) {
	var base struct {
		Version        int
		Revision, Path string
		Limits         QueueLimits
	}
	if len(actual) == 0 || json.Unmarshal(actual, &base) != nil {
		return false, nil // Existing initialization/corruption checks own this path.
	}
	if base.Version != 4 && !migrate {
		if base.Version == 3 && !bytes.Equal(actual, want) {
			changed := base
			changed.Limits.Bytes = q.limits.Bytes
			encoded, _ := json.Marshal(changed)
			if bytes.Equal(encoded, want) {
				return true, ErrVersion
			}
		}
		return false, nil
	}
	if base.Version != 3 && base.Version != 4 {
		return true, ErrVersion
	}
	encoded, err := json.Marshal(base)
	if err != nil || !bytes.Equal(encoded, actual) || base.Limits.Bytes < 1 || base.Limits.Bytes > 1<<40 {
		return true, ErrStore
	}
	oldBytes := base.Limits.Bytes
	oldLimits := base.Limits
	oldLimits.Bytes = q.limits.Bytes
	if oldLimits != q.limits {
		return true, ErrVersion
	}
	base.Version, base.Limits = 3, q.limits
	expected, _ := json.Marshal(base)
	if !bytes.Equal(expected, want) {
		return true, ErrVersion // Root/revision/other limits cannot change here.
	}
	base.Limits.Bytes = oldBytes
	original, _ := json.Marshal(base)
	if oldBytes > q.limits.Bytes {
		return true, ErrVersion
	}
	var workers []*os.File
	defer func() {
		for _, worker := range workers {
			unlockClose(worker)
		}
	}()
	if migrate {
		for i := range q.limits.Workers {
			worker, err := q.lockName(fmt.Sprintf("worker-%d", i))
			if err != nil {
				return true, err
			}
			workers = append(workers, worker)
		}
	}
	flags := os.O_RDONLY
	if !q.readOnly {
		flags = os.O_RDWR
	}
	if migrate && bytes.Equal(actual, original) {
		flags |= os.O_CREATE
	}
	journal, err := q.file("capacity-updates", flags, 0o600)
	if err != nil {
		return true, err
	}
	defer func() { _ = journal.Close() }()
	hash := sha256.Sum256(original)
	current := int64(0)
	_, err = readJournalFramesMode(journal, budgetJournalBytes, !q.readOnly, func(raw []byte) error {
		var r budgetRecord
		if json.Unmarshal(raw, &r) != nil || r.Version != 1 || r.Base != hash || r.Bytes > 1<<40 || current == 0 && r.Bytes != oldBytes || current != 0 && r.Bytes <= current {
			return ErrStore
		}
		current = r.Bytes
		return nil
	})
	if err != nil {
		return true, err
	}
	appendBudget := func(n int64) error {
		raw, _ := json.Marshal(budgetRecord{Version: 1, Base: hash, Bytes: n})
		return appendJournalFrame(journal, raw, budgetJournalBytes)
	}
	if current == 0 {
		if !migrate || !bytes.Equal(actual, original) {
			return true, ErrStore
		}
		if err := appendBudget(oldBytes); err != nil || syncQueueDir(q.root) != nil {
			return true, ErrStore
		}
		current = oldBytes
	}
	if q.limits.Bytes < current || !migrate && q.limits.Bytes != current {
		return true, ErrVersion
	}
	q.budgetBase = oldBytes
	if migrate {
		if err := q.migratePublicationBudget(false); err != nil {
			return true, err
		}
		q.markHistory("budget_after_base")
		if _, err := f.WriteAt([]byte{'4'}, int64(len(`{"Version":`))); err != nil || f.Sync() != nil || syncQueueDir(q.root) != nil {
			return true, ErrStore
		}
		q.markHistory("budget_after_gate")
		if err := q.migratePublicationBudget(true); err != nil {
			return true, err
		}
		q.markHistory("budget_after_publication")
		if q.limits.Bytes != current {
			if err := appendBudget(q.limits.Bytes); err != nil {
				return true, err
			}
		}
		q.markHistory("budget_after_commit")
	}
	return true, nil
}
