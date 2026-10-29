package transferclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/felinics/memoh/internal/deliverymeta"
)

func receiptBindingFixture(t *testing.T) (func(string) string, *DiskQueue, deliverymeta.Closed, func(string, string, []byte)) {
	t.Helper()
	env, closed, write := producerFixture(t, "https://fixture.invalid")
	cfg, err := OpenHostConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	q, err := OpenHerdrQueue(env, "fixture", lifecycleLimits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	j := producerAction(closed, "fixture").Job
	j.HostBinding = cfg.Binding
	f, err := cfg.OpenSource(closed.Artifact.SourceRef)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	source, err := describeSource(closed.Artifact.SourceRef, f, closed.SizeBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Admit(t.Context(), "upload", j, f); err != nil {
		t.Fatal(err)
	}
	// Fixture seeds already-durable metadata, not a simulated HTTP success.
	lifecycleEdit(t, q, j.DeliveryID, func(r *QueueStatus) {
		r.Version, r.Registered, r.Activated, r.Source = 5, true, true, source
		r.Job.Phase, r.Job.ReceiptID, r.Job.ChunkBytes = "uploading", "original-receipt", 4
	})
	return env, q, closed, write
}

func TestReceiptBindingReadOnlyOriginalAndHistory(t *testing.T) {
	env, q, closed, _ := receiptBindingFixture(t)
	// Absence proves this path does not depend on reading a credential/body.
	if err := os.Remove(filepath.Join(env("HERDR_PLUGIN_CONFIG_DIR"), "device.credential")); err != nil {
		t.Fatal(err)
	}
	read := func() {
		t.Helper()
		before := lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))
		out, err := ReadReceiptBinding(t.Context(), env, "fixture", lifecycleLimits, closed.DeliveryID())
		if err != nil || out.KnownReceipt != "original-receipt" || out.Task != closed.Task || out.SourceRef != closed.Artifact.SourceRef ||
			out.ContentHash != closed.SHA256 || out.SizeBytes != closed.SizeBytes || out.ServerEffects != "none" || out.Scope != "local_queue_read_only" {
			t.Fatal(out, err)
		}
		if !reflect.DeepEqual(before, lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))) {
			t.Fatal("receipt observation mutated queue files/mtime")
		}
	}
	read()
	lifecycleEdit(t, q, closed.DeliveryID(), func(r *QueueStatus) {
		r.Job.Phase, r.Job.Offset = "ready", closed.SizeBytes
		r.Job.Ready = &Reference{DeliveryID: closed.DeliveryID(), OperationID: closed.Task.OperationID, BotID: closed.Task.BotID,
			ContentHash: closed.SHA256, SizeBytes: closed.SizeBytes, Mime: closed.Artifact.Mime}
		r.Associated, r.AssociationState = true, AssociationCommitted
	})
	if err := q.ReleaseConfirmed(t.Context(), closed.DeliveryID()); err != nil {
		t.Fatal(err)
	}
	if err := q.ReclaimConfirmed(t.Context()); err != nil {
		t.Fatal(err)
	}
	read()
}

func TestReceiptBindingReadOnlyRejectsForeignBusyAndCorrupt(t *testing.T) {
	for _, mode := range []string{"foreign-config", "busy", "corrupt", "torn", "missing-delivery", "canceled-context", "no-recorded-receipt"} {
		t.Run(mode, func(t *testing.T) {
			env, q, closed, write := receiptBindingFixture(t)
			id := closed.DeliveryID()
			switch mode {
			case "foreign-config":
				raw, _ := json.Marshal(hostConfigWire{Version: 1, Origin: "https://different.invalid", SourceRoot: closedSourceRoot(t, env)})
				write(env("HERDR_PLUGIN_CONFIG_DIR"), "transfer.json", raw)
			case "busy":
				lock, err := q.lock(id)
				if err != nil {
					t.Fatal(err)
				}
				defer unlockClose(lock)
			case "corrupt", "torn":
				f, err := q.file(id+".journal", os.O_WRONLY|os.O_APPEND, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				raw := []byte{0, 0}
				if mode == "corrupt" {
					raw = make([]byte, 37)
					raw[3] = 1
				}
				if _, err := f.Write(raw); err != nil {
					t.Fatal(err)
				}
				_ = f.Close()
			case "missing-delivery":
				id = "unknown-delivery"
			case "no-recorded-receipt":
				lifecycleEdit(t, q, id, func(r *QueueStatus) { r.Job.ReceiptID, r.Job.Phase = "", "create_unknown" })
			}
			ctx := t.Context()
			if mode == "canceled-context" {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			before := lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))
			out, err := ReadReceiptBinding(ctx, env, "fixture", lifecycleLimits, id)
			if mode == "torn" || mode == "no-recorded-receipt" {
				if err != nil {
					t.Fatal(err)
				}
				if mode == "no-recorded-receipt" && out.KnownReceipt != "" {
					t.Fatal("invented receipt")
				}
			} else if err == nil {
				t.Fatal("unverified receipt accepted", mode)
			}
			if mode == "busy" && !errors.Is(err, ErrBusy) {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))) {
				t.Fatal("read repaired/created/changed queue files", mode)
			}
		})
	}
}

func closedSourceRoot(t *testing.T, env func(string) string) string {
	t.Helper()
	cfg, err := OpenHostConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	return cfg.sourcePath
}

func TestReceiptBindingDoesNotInitializeMissingQueue(t *testing.T) {
	env, closed, _ := producerFixture(t, "https://fixture.invalid")
	before := lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))
	if _, err := ReadReceiptBinding(t.Context(), env, "fixture", lifecycleLimits, closed.DeliveryID()); err == nil {
		t.Fatal("missing queue accepted")
	}
	if !reflect.DeepEqual(before, lifecycleFiles(t, env("HERDR_PLUGIN_STATE_DIR"))) {
		t.Fatal("read initialized missing queue")
	}
}
