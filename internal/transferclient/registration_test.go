package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRegisteredProcessExitBeforeAcknowledgement(t *testing.T) {
	if os.Getenv("MEMOH_REGISTRATION_CHILD") == "1" {
		var job Job
		if json.Unmarshal([]byte(os.Getenv("MEMOH_REGISTRATION_JOB")), &job) != nil {
			os.Exit(71)
		}
		cfg, err := OpenHostConfig(os.Getenv)
		if err != nil {
			os.Exit(72)
		}
		q, err := OpenHerdrQueue(os.Getenv, "fixture", QueueLimits{Jobs: 1, Bytes: 1, JournalBytes: 1 << 20, Workers: 1})
		if err != nil {
			os.Exit(73)
		}
		q.journalHook = func(stage string) {
			if stage == "registration_synced" {
				os.Exit(74)
			}
		}
		_, _ = q.RegisterUpload(t.Context(), job, "result-one", cfg.OpenSource)
		os.Exit(75)
	}
	q, _, cfg, job := registeredFixture(t)
	job.DeliveryID, job.SnapshotID = "crash-registered", "crash-registered"
	encoded, _ := json.Marshal(job)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRegisteredProcessExitBeforeAcknowledgement$") //nolint:gosec // Current test executable, exact helper test, synthetic fixture only.
	cmd.Env = append(os.Environ(), "MEMOH_REGISTRATION_CHILD=1", "MEMOH_REGISTRATION_JOB="+string(encoded), "HERDR_PLUGIN_CONFIG_DIR="+cfg.root.Name(), "HERDR_PLUGIN_STATE_DIR="+filepath.Dir(q.root.Name()))
	err := cmd.Run()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 74 {
		t.Fatal("registration exit hook", err)
	}
	r, err := q.RegisterUpload(t.Context(), job, "result-one", cfg.OpenSource)
	if err != nil || !r.Registered || r.Activated || r.Job != job {
		t.Fatal("lost registration acknowledgement changed identity", err)
	}
	if _, err := q.root.Lstat(queueName(job.DeliveryID + ".snapshot")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("process recovery copied payload", err)
	}
}

func registeredFixture(t *testing.T) (*DiskQueue, func() *DiskQueue, *HostConfig, Job) {
	t.Helper()
	env, closed, _ := producerFixture(t, "https://fixture.invalid")
	cfg, err := OpenHostConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cfg.Close() })
	raw, _ := json.Marshal(closed)
	action, _, err := ProducerAdmission(bytes.NewReader(raw), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	action.Job.HostBinding = cfg.Binding
	open := func() *DiskQueue {
		q, err := OpenHerdrQueue(env, "fixture", QueueLimits{Jobs: 1, Bytes: 1, JournalBytes: 1 << 20, Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		q.openSource = cfg.OpenSource
		t.Cleanup(func() { _ = q.Close() })
		return q
	}
	q := open()
	r, err := q.RegisterUpload(t.Context(), action.Job, closed.Artifact.SourceRef, cfg.OpenSource)
	if err != nil || !r.Registered || r.Admitted {
		t.Fatal("metadata registration", err)
	}
	return q, open, cfg, action.Job
}

func TestRegisteredSourceRestartAndMissingChanges(t *testing.T) {
	for _, mode := range []string{"changed", "deleted", "canceled", "busy"} {
		t.Run(mode, func(t *testing.T) {
			q, reopen, cfg, job := registeredFixture(t)
			r, err := q.Inspect(job.DeliveryID)
			if err != nil {
				t.Fatal(err)
			}
			for _, suffix := range []string{".snapshot", ".tree", ".admission"} {
				if _, err := q.root.Lstat(queueName(job.DeliveryID + suffix)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("registration allocated content", suffix, err)
				}
			}
			switch mode {
			case "changed":
				f, err := openPrivateFile(cfg.sources, r.Source.Ref, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteAt([]byte("changed"), 0)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := removeQueueFile(cfg.sources, r.Source.Ref); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				if err := q.RequestCancel(job.DeliveryID); err != nil {
					t.Fatal(err)
				}
			}
			_ = q.Close()
			q = reopen()
			replayed, err := q.RegisterUpload(t.Context(), job, r.Source.Ref, cfg.OpenSource)
			if err != nil || replayed.Job.Spec != job.Spec || replayed.Source.Identity != r.Source.Identity {
				t.Fatal("original identity not replayed", err)
			}
			f, client := newFixture(t)
			var worker *os.File
			if mode == "busy" {
				worker, err = q.worker()
				if err != nil {
					t.Fatal(err)
				}
			}
			err = q.RunOne(t.Context(), job.DeliveryID, client, nil, time.Now())
			if worker != nil {
				unlockClose(worker)
			}
			if mode == "busy" {
				if !errors.Is(err, ErrBusy) {
					t.Fatal("worker limit", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(f.calls) != 0 {
				t.Fatal("source failure performed network work")
			}
			got, err := q.Inspect(job.DeliveryID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "changed" && got.Job.ErrorCode != "source_changed" || mode == "deleted" && got.Job.ErrorCode != "source_missing" || mode == "canceled" && got.Job.Phase != "canceled" {
				t.Fatal("source outcome", got.Job.ErrorCode, got.Job.Phase)
			}
			conflict := job
			conflict.Spec.Mime = "application/octet-stream"
			if _, err := q.RegisterUpload(t.Context(), conflict, r.Source.Ref, cfg.OpenSource); !errors.Is(err, ErrInvalid) {
				t.Fatal("conflicting ID accepted", err)
			}
		})
	}
}

func TestRegisteredSourceResumesReceiptWithoutSnapshot(t *testing.T) {
	q, reopen, cfg, job := registeredFixture(t)
	f, client := newFixture(t)
	now := time.Now()
	// The isolated transport fixture is not the real NAS Server/PG.
	if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, now); err != nil {
		t.Fatal(err)
	}
	r, err := q.Inspect(job.DeliveryID)
	if err != nil || !r.Admitted || r.Seal == nil {
		t.Fatal("index", err)
	}
	if _, err := q.root.Lstat(queueName(job.DeliveryID + ".snapshot")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("source duplicated", err)
	}
	f.setDrop("PATCH /bots/" + job.BotID + "/media/uploads/receipt-1")
	if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, now); err != nil {
		t.Fatal(err)
	}
	_ = q.Close()
	q = reopen()
	commits := 0
	binding := &AssociationBinding{Commit: func(context.Context, string, Reference, io.Reader) error { commits++; return nil }, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		return OutcomeCommitted, nil
	}}
	for range 12 {
		r, err = q.Inspect(job.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		if r.Job.Phase == "ready" {
			break
		}
		if r.Job.NextAttempt.After(now) {
			now = r.Job.NextAttempt
		}
		if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, now); err != nil {
			t.Fatal(err)
		}
	}
	if r.Job.Phase != "ready" || string(fixtureReady(f)) != "fixture" || f.count("POST /bots/"+job.BotID+"/media/uploads") != 1 || f.count("PATCH /bots/"+job.BotID+"/media/uploads/receipt-1") != 2 {
		t.Fatal("resume replayed confirmed content", r.Job.Phase)
	}
	if err := q.RunOne(t.Context(), job.DeliveryID, client, binding, now); err != nil {
		t.Fatal(err)
	}
	if err := q.ReleaseConfirmed(t.Context(), job.DeliveryID); err != nil {
		t.Fatal(err)
	}
	if commits != 1 {
		t.Fatal("duplicate association", commits)
	}
	source, err := cfg.OpenSource("result-one")
	if err != nil {
		t.Fatal("source removed", err)
	}
	_ = source.Close()
	if err := removeQueueFile(cfg.sources, "result-one"); err != nil {
		t.Fatal(err)
	}
	r, err = q.RegisterUpload(t.Context(), job, "result-one", cfg.OpenSource)
	if err != nil || !r.Associated || r.PayloadState != PayloadReleased {
		t.Fatal("receipt replay after source removal", err)
	}
}

func TestRegisteredSourceBlockProofRejectsUndetectedChange(t *testing.T) {
	q, _, cfg, job := registeredFixture(t)
	f, client := newFixture(t)
	if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	file, err := openPrivateFile(cfg.sources, "result-one", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteAt([]byte("bad"), 4)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Same identity/size/mtime: only the persisted block proof detects this.
	if err := cfg.sources.Chtimes("result-one", info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	r, err := q.Inspect(job.DeliveryID)
	if err != nil || r.Job.Phase != "blocked" || r.Job.ErrorCode != "source_changed" || r.Job.ReceiptID == "" {
		t.Fatal("changed source mixed into receipt", err)
	}
	if f.count("PATCH /bots/"+job.BotID+"/media/uploads/receipt-1") != 0 {
		t.Fatal("invalid block was transmitted")
	}
}
