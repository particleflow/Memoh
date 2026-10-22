package transferclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestAdmissionCompactionRetryKeepsCorruptEvidence(t *testing.T) {
	for _, mode := range []string{"empty", "checksum", "version"} {
		t.Run(mode, func(t *testing.T) {
			q, r, _ := activeJournalFixture(t)
			name := queueName(r.Job.DeliveryID + ".journal")
			f, err := q.file(r.Job.DeliveryID+".journal", os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "version" {
				r.Version = 99
				body, _ := json.Marshal(r)
				if err = f.Truncate(0); err == nil {
					err = appendJournalFrame(f, body, activeJournalLimits.JournalBytes)
				}
			} else if mode == "empty" {
				err = f.Truncate(0)
			} else {
				var first [1]byte
				if _, err = f.ReadAt(first[:], 4); err == nil {
					first[0] ^= 1
					_, err = f.WriteAt(first[:], 4)
				}
			}
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
			before, err := q.root.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			retries := 0
			q.journalHook = func(stage string) {
				if stage == "admission_retry_head" {
					retries++
				}
			}
			global, err := q.lock("admission")
			if err != nil {
				t.Fatal(err)
			}
			defer unlockClose(global)
			wantRetries := 1
			if mode == "version" {
				wantRetries = 0
			}
			if _, err := q.admissions(); !errors.Is(err, ErrStore) || retries != wantRetries {
				t.Fatal("corruption became retryable/empty or retried unboundedly", err, retries)
			}
			after, err := q.root.ReadFile(name)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("admission rewrote corrupt evidence", err)
			}
		})
	}
}

func TestAdmissionReadDuringActiveCompaction(t *testing.T) {
	q, before, getenv := activeJournalFixture(t)
	reader, err := OpenHerdrQueue(getenv, "fixture", activeJournalLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	truncated, resume, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var release sync.Once
	defer release.Do(func() { close(resume) })
	q.journalHook = func(stage string) {
		if stage == "after_truncate" {
			close(truncated)
			<-resume
		}
	}
	go func() {
		lock, err := q.lock(before.Job.DeliveryID)
		if err != nil {
			done <- err
			return
		}
		defer unlockClose(lock)
		r, journal, err := q.load(before.Job.DeliveryID)
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = journal.Close() }()
		r.Job.Offset = 321
		done <- q.save(journal, r)
	}()
	select {
	case <-truncated:
	case err := <-done:
		t.Fatal("compaction did not pause", err)
	case <-time.After(5 * time.Second):
		t.Fatal("compaction did not reach truncate")
	}
	reader.journalHook = func(stage string) {
		if stage == "admission_after_head_error" {
			// Force the exact race: first read saw the empty journal, then
			// the writer finishes and removes its witness before fallback.
			release.Do(func() { close(resume) })
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("writer blocked by admission lock")
			}
		}
	}
	global, err := reader.lock("admission")
	if err != nil {
		t.Fatal(err)
	}
	defer unlockClose(global)
	records, err := reader.admissions()
	if err != nil || len(records) != 1 || records[0].Job.DeliveryID != before.Job.DeliveryID || records[0].Job.Spec != before.Job.Spec || records[0].Job.Offset != 321 {
		t.Fatal("compaction completion mistaken for missing/corrupt admission", records, err)
	}
	if err := reader.capacity(1); !errors.Is(err, ErrCapacity) {
		t.Fatal("active reservation was lost", err)
	}
	if _, err := reader.root.Lstat(queueName(before.Job.DeliveryID + ".checkpoint")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("test did not reach removed-witness window", err)
	}
}
