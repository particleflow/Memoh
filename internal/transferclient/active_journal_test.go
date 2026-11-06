package transferclient

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestActiveJournalBoundedAcrossProgress(t *testing.T) {
	getenv, _, _ := producerFixture(t, "https://fixture.invalid")
	limits := QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 65536, Workers: 1}
	q, err := OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	job := newJob(make([]byte, 65536))
	job.SnapshotID, job.LocatorRevision = job.DeliveryID, "fixture"
	if err := q.Admit(t.Context(), "download", job, nil); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 2048; i++ {
		lock, err := q.lock(job.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		r, file, err := q.load(job.DeliveryID)
		if err != nil {
			unlockClose(lock)
			t.Fatal("reopen", err)
		}
		r.Job.Phase, r.Job.ReceiptID, r.Job.ChunkBytes = "uploading", "original-receipt", 4096
		r.Job.Offset, r.Job.Attempts = i, 3
		r.Job.NextAttempt = time.Unix(2000000000, 0).UTC()
		err = q.save(file, r)
		_ = file.Close()
		unlockClose(lock)
		if err != nil {
			t.Fatalf("progress %d exhausted active journal: %v", i, err)
		}
	}
	r, err := q.Inspect(job.DeliveryID)
	if err != nil || r.Job.Offset != 2048 || r.Job.ReceiptID != "original-receipt" || r.Job.Attempts != 3 || r.Job.NextAttempt != time.Unix(2000000000, 0).UTC() || r.Associated {
		t.Fatal("original progress/retry identity lost", err)
	}
	stat, err := q.root.Stat(queueName(job.DeliveryID + ".journal"))
	if err != nil || stat.Size() > limits.JournalBytes {
		t.Fatal("journal grew beyond quota", err)
	}
}

var activeJournalLimits = QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 65536, Workers: 1}

func activeJournalFixture(t *testing.T) (*DiskQueue, QueueStatus, func(string) string) {
	t.Helper()
	getenv, _, _ := producerFixture(t, "https://fixture.invalid")
	q, err := OpenHerdrQueue(getenv, "fixture", activeJournalLimits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	job := newJob(make([]byte, 65536))
	job.SnapshotID, job.LocatorRevision = job.DeliveryID, "fixture"
	if err := q.Admit(t.Context(), "download", job, nil); err != nil {
		t.Fatal(err)
	}
	r, journal, err := q.load(job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	r.Version = 4
	r.Job.Phase, r.Job.ReceiptID, r.Job.ChunkBytes = "uploading", "original-receipt", 4096
	r.Job.CancelRequested, r.Job.Attempts = true, 7
	r.Job.NextAttempt = time.Unix(2000000000, 0).UTC()
	body, _ := json.Marshal(r)
	for {
		stat, err := journal.Stat()
		if err != nil {
			t.Fatal(err)
		}
		// The child changes offset from 0 to 321, adding two JSON bytes.
		if stat.Size()+int64(len(body)+38) >= activeJournalLimits.JournalBytes/2 {
			break
		}
		if err := appendJournalFrame(journal, body, activeJournalLimits.JournalBytes); err != nil {
			t.Fatal(err)
		}
	}
	return q, r, getenv
}

func TestActiveJournalProcessCrash(t *testing.T) {
	if path := os.Getenv("MEMOH_JOURNAL_FIXTURE_ROOT"); path != "" {
		q, err := OpenHerdrQueue(func(string) string { return path }, "fixture", activeJournalLimits)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = q.Close() }()
		lock, err := q.lock("delivery-1")
		if err != nil {
			t.Fatal(err)
		}
		defer unlockClose(lock)
		r, f, err := q.load("delivery-1")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		q.journalHook = func(stage string) {
			if stage == os.Getenv("MEMOH_JOURNAL_FIXTURE_STAGE") {
				os.Exit(84)
			}
		}
		r.Job.Offset = 321
		if err := q.save(f, r); err != nil {
			t.Fatal(err)
		}
		t.Fatal("crash hook not reached")
	}
	for _, stage := range []string{"after_record", "after_checkpoint", "after_truncate", "after_compact", "after_finish"} {
		t.Run(stage, func(t *testing.T) {
			q, before, getenv := activeJournalFixture(t)
			if err := q.Close(); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestActiveJournalProcessCrash$") //nolint:gosec // Current test executable and fixed test selector; isolated synthetic root only.
			cmd.Env = append(os.Environ(), "MEMOH_JOURNAL_FIXTURE_ROOT="+getenv("HERDR_PLUGIN_STATE_DIR"), "MEMOH_JOURNAL_FIXTURE_STAGE="+stage)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 84 {
				t.Fatalf("crash child: %v %s", err, out)
			}
			reopened, err := OpenHerdrQueue(getenv, "fixture", activeJournalLimits)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()
			// In the truncate window enumeration must still reserve this job.
			if heads, err := reopened.admissions(); err != nil || len(heads) != 1 {
				t.Fatal("reservation disappeared", len(heads), err)
			}
			r, err := reopened.Inspect(before.Job.DeliveryID)
			before.Job.Offset = 321
			got, _ := json.Marshal(r)
			want, _ := json.Marshal(before)
			if err != nil || string(got) != string(want) {
				t.Fatal("latest cancel/retry/receipt state lost", err)
			}
			if _, err := reopened.root.Stat(queueName(before.Job.DeliveryID + ".part")); err != nil {
				t.Fatal("unconfirmed payload removed", err)
			}
			if _, err := reopened.root.Lstat(queueName(before.Job.DeliveryID + ".checkpoint")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("checkpoint not reconciled", err)
			}
		})
	}
}

func TestActiveJournalIncompleteAndCorruptCheckpoint(t *testing.T) {
	for _, mode := range []string{"partial", "checksum", "identity"} {
		t.Run(mode, func(t *testing.T) {
			q, r, _ := activeJournalFixture(t)
			f, err := q.file(r.Job.DeliveryID+".journal", os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := queueObjectIdentity(f)
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
			witness := activeCheckpoint{Version: 1, FileID: identity, Record: r}
			if mode == "identity" {
				witness.FileID = "foreign-file"
			}
			f, err = q.file(r.Job.DeliveryID+".checkpoint", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "partial" {
				_, err = f.Write([]byte{1, 2, 3})
			} else {
				raw, _ := json.Marshal(witness)
				err = appendJournalFrame(f, raw, releaseReceiptBytes)
				if err == nil && mode == "checksum" {
					_, err = f.WriteAt([]byte{0}, 36)
				}
			}
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
			_, err = q.Inspect(r.Job.DeliveryID)
			if mode == "partial" {
				if err != nil {
					t.Fatal("partial pre-truncate witness cannot destroy valid journal", err)
				}
			} else {
				if !errors.Is(err, ErrStore) {
					t.Fatal("invalid complete witness accepted", err)
				}
				if _, err := q.root.Lstat(queueName(r.Job.DeliveryID + ".checkpoint")); err != nil {
					t.Fatal("corrupt evidence removed", err)
				}
			}
		})
	}
}

func TestActiveJournalPreservesUnknownAssociation(t *testing.T) {
	for _, phase := range []string{"create_unknown", "association_unknown"} {
		t.Run(phase, func(t *testing.T) {
			q, r, _ := activeJournalFixture(t)
			f, err := q.file(r.Job.DeliveryID+".journal", os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			if phase == "create_unknown" {
				r.Job.Phase, r.Job.ReceiptID = phase, ""
			} else {
				r.Job.Phase, r.Job.Offset = "ready", r.Job.Spec.SizeBytes
				r.Job.Ready = &Reference{DeliveryID: r.Job.DeliveryID, OperationID: r.Job.OperationID, BotID: r.Job.BotID, ContentHash: r.Job.Spec.SHA256, SizeBytes: r.Job.Spec.SizeBytes, Mime: r.Job.Spec.Mime}
				r.AssociationState, r.AssociationAttempts = AssociationUnknown, 4
				r.AssociationDue = time.Unix(2000000100, 0).UTC()
			}
			if err := q.save(f, r); err != nil {
				t.Fatal(err)
			}
			// Exercise checkpoint even when the smaller unknown record happens
			// to land just below the automatic threshold.
			if err := q.compactActiveJournal(f, r); err != nil {
				t.Fatal(err)
			}
			got, err := q.Inspect(r.Job.DeliveryID)
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(r)
			if err != nil || string(a) != string(b) || got.Associated || !got.Job.CancelRequested {
				t.Fatal("unknown/cancel evidence changed", err)
			}
		})
	}
}

func TestActiveJournalLegacyReadAndFullBoundary(t *testing.T) {
	for _, full := range []bool{false, true} {
		q, r, _ := activeJournalFixture(t)
		f, err := q.file(r.Job.DeliveryID+".journal", os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if f.Truncate(0) != nil {
			t.Fatal("truncate fixture")
		}
		r.Version = 2
		body, _ := json.Marshal(r)
		if err := appendJournalFrame(f, body, activeJournalLimits.JournalBytes); err != nil {
			t.Fatal(err)
		}
		if full {
			for {
				err := appendJournalFrame(f, body, activeJournalLimits.JournalBytes)
				if errors.Is(err, ErrCapacity) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		r.Job.Offset = 321
		err = q.save(f, r)
		_ = f.Close()
		got, readErr := q.Inspect(r.Job.DeliveryID)
		if full {
			if !errors.Is(err, ErrCapacity) || readErr != nil || got.Version != 2 || got.Job.Offset != 0 {
				t.Fatal("full legacy journal must stay intact without room for v4 fence", err, readErr)
			}
		} else if err != nil || readErr != nil || got.Version != 4 || got.Job.Offset != 321 {
			t.Fatal("legacy progress did not upgrade on write", err, readErr)
		}
		_ = q.Close()
	}
}

func TestActiveJournalMaximumRecordEnvelope(t *testing.T) {
	q, r, _ := activeJournalFixture(t)
	r.Job.ErrorCode = "x"
	raw, _ := json.Marshal(r)
	r.Job.ErrorCode = strings.Repeat("x", 16384-len(raw)+1)
	raw, _ = json.Marshal(r)
	if len(raw) != 16384 {
		t.Fatal("fixture is not the accepted maximum record")
	}
	f, err := q.file(r.Job.DeliveryID+".journal", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for range 3 {
		if err := q.save(f, r); err != nil {
			t.Fatal("object-identity envelope excluded a valid maximum record", err)
		}
	}
	got, err := q.Inspect(r.Job.DeliveryID)
	if err != nil || got.Job.ErrorCode != r.Job.ErrorCode {
		t.Fatal("maximum record changed", err)
	}
}
