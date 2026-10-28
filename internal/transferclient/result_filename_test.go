package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
)

func filenameNoHTTP() (*http.Client, *atomic.Int64) {
	calls := &atomic.Int64{}
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("isolated filename test forbids HTTP")
	}}
	return &http.Client{Transport: transport}, calls
}

func TestExplicitFilenameRegistrationProcessRecovery(t *testing.T) {
	limits := QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	if os.Getenv("MEMOH_FILENAME_CHILD") == "1" {
		cfg, err := OpenHostConfig(os.Getenv)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = cfg.Close() }()
		raw, err := cfg.readProducerManifest(os.Getenv("MEMOH_FILENAME_MANIFEST"), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		client, calls := filenameNoHTTP()
		r, err := EnqueueProducer(t.Context(), os.Getenv, "fixture", limits, bytes.NewReader(raw), client, time.Now())
		if err != nil || !r.Registered || r.Admitted || calls.Load() != 0 {
			t.Fatal("child exact-source registration", err)
		}
		return // Fresh process exits after durable registration, before consumption.
	}
	getenv, closed, write := producerFixture(t, "https://fixture.invalid")
	data := make([]byte, 196731)
	for i := range data {
		data[i] = byte(i*37 + i/13) //nolint:gosec // Deterministic real fixture bytes.
	}
	closed.Artifact = deliverymeta.Artifact{SourceRef: "windows_photo_a0_result.png", Mime: "image/png"}
	closed.SHA256, closed.SizeBytes = digest(data), int64(len(data))
	write("source", closed.Artifact.SourceRef, data)
	writeProducerContextFixture(t, getenv, closed.Plan)
	raw, _ := json.Marshal(closed)
	write("source", closed.ManifestName(), raw)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestExplicitFilenameRegistrationProcessRecovery$") //nolint:gosec // Current test executable and exact helper only.
	cmd.Env = append(os.Environ(), "MEMOH_FILENAME_CHILD=1", "MEMOH_FILENAME_MANIFEST="+closed.ManifestName(), "HERDR_PLUGIN_CONFIG_DIR="+getenv("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR="+getenv("HERDR_PLUGIN_STATE_DIR"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fresh registration process: %v %s", err, out)
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	q, err := OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	q.openSource = cfg.OpenSource
	r, err := q.Inspect(closed.DeliveryID())
	if err != nil || !r.Registered || r.Admitted || r.Source.Ref != closed.Artifact.SourceRef || r.Job.HostBinding != cfg.Binding || r.Job.OperationID != closed.Task.OperationID || r.Job.Task.Attempt != closed.Task.Attempt || r.Job.Task.Generation != closed.Task.Generation {
		t.Fatal("original filename/task/binding did not survive process exit", err)
	}
	client, calls := filenameNoHTTP()
	replay, err := EnqueueProducer(t.Context(), getenv, "fixture", limits, bytes.NewReader(raw), client, time.Now())
	if err != nil || replay.Job != r.Job || *replay.Source != *r.Source || calls.Load() != 0 {
		t.Fatal("original manifest replay changed durable registration", err)
	}
	_, journal, err := q.load(r.Job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = q.prepareRegistered(t.Context(), r, journal, time.Now())
	_ = journal.Close()
	if err != nil || !r.Admitted || r.Seal == nil {
		t.Fatal("exact-source hash indexing", err)
	}
	snapshot, closeSnapshot, err := q.openSnapshot(r)
	if err != nil {
		t.Fatal(err)
	}
	actual := make([]byte, len(data))
	err = snapshot.ReadAt(t.Context(), r.Job.SnapshotID, r.Job.LocatorRevision, r.Job.Spec, actual, 0)
	closeSnapshot()
	if err != nil || !bytes.Equal(actual, data) || digest(actual) != closed.SHA256 {
		t.Fatal("protected original bytes/hash differ", err)
	}
	if _, err := q.root.Lstat(queueName(r.Job.DeliveryID + ".snapshot")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("filename support copied the source", err)
	}
	if err := cfg.consumeSourceManifest(closed.ManifestName(), raw); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.sourcePath, closed.Artifact.SourceRef)); err != nil {
		t.Fatal("manifest confirmation removed original", err)
	}
	action := producerAction(closed, "fixture")
	action.Job.HostBinding = cfg.Binding
	if _, err := q.RegisterUpload(t.Context(), action.Job, "another.png", cfg.OpenSource); !errors.Is(err, ErrInvalid) {
		t.Fatal("same ID accepted another filename", err)
	}
	action.Job.Spec.SHA256 = strings.Repeat("0", 64)
	if _, err := q.RegisterUpload(t.Context(), action.Job, closed.Artifact.SourceRef, cfg.OpenSource); !errors.Is(err, ErrInvalid) {
		t.Fatal("same ID accepted conflicting bytes", err)
	}
	if validID(closed.Artifact.SourceRef) || calls.Load() != 0 {
		t.Fatal("global identifier/HTTP boundary widened")
	}
	t.Logf("two real processes; original delivery/task/HostBinding retained; source=%dB sha256=%s; protected hash read; payload copies=0 HTTP=0", len(data), closed.SHA256)
}

func TestExplicitFilenameRejectsForeignContext(t *testing.T) {
	for _, mismatch := range []string{"root", "operation", "attempt", "generation", "filename"} {
		t.Run(mismatch, func(t *testing.T) {
			getenv, closed, write := producerFixture(t, "https://fixture.invalid")
			closed.Artifact.SourceRef = "photo.png"
			writeProducerContextFixture(t, getenv, closed.Plan)
			switch mismatch {
			case "root":
				closed.Task.RootHash = strings.Repeat("0", 64)
			case "operation":
				closed.Task.OperationID = "11111111-1111-4111-8111-111111111111"
			case "attempt":
				closed.Task.Attempt = strings.Repeat("c", 32)
			case "generation":
				closed.Task.Generation = strings.Repeat("c", 32)
			case "filename":
				closed.Artifact.SourceRef = "another.png"
			}
			raw, _ := json.Marshal(closed)
			write("source", closed.ManifestName(), raw)
			cfg, err := OpenHostConfig(getenv)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cfg.Close() }()
			if _, err := cfg.readProducerManifest(closed.ManifestName(), "fixture"); !errors.Is(err, ErrProducerManifest) {
				t.Fatal("foreign context accepted", err)
			}
		})
	}
}
