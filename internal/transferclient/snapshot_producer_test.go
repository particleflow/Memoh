package transferclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSnapshotProducerOnlyAdvancesRetainedPrivateBytes(t *testing.T) {
	f := &fixture{calls: map[string]int{}}
	var job Job
	var commits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/internal/computer-transfers/" + job.OperationID + "/" + job.DeliveryID
		if r.Header.Get("Authorization") != "Memoh-Transfer fixture-stable-device" || !strings.HasPrefix(r.URL.Path, prefix+"/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.Path, "/internal/computer-deliveries/") {
			if strings.HasSuffix(r.URL.Path, "/commit") {
				commits.Add(1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 3, "revision": "1", "outcome": "committed"})
			return
		}
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		r.Header.Set("Authorization", syntheticAuth)
		f.serve(w, r)
	}))
	defer server.Close()
	getenv, closed, _ := producerFixture(t, server.URL)
	raw, _ := json.Marshal(closed)
	action, _, err := ProducerAdmission(bytes.NewReader(raw), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	job = action.Job
	job.HostBinding = cfg.Binding
	limits := QueueLimits{Jobs: 8, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	q, err := OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	source, err := cfg.OpenSource(closed.Artifact.SourceRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Admit(t.Context(), "upload", job, source); err != nil {
		t.Fatal(err)
	}
	_ = source.Close()
	registered := job
	registered.DeliveryID, registered.SnapshotID = "metadata-only", "metadata-only"
	if _, err := q.RegisterUpload(t.Context(), registered, closed.Artifact.SourceRef, cfg.OpenSource); err != nil {
		t.Fatal(err)
	}
	before, err := q.Inspect(registered.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	download := job
	download.DeliveryID, download.SnapshotID = "download-only", "download-only"
	if err := q.Admit(t.Context(), "download", download, nil); err != nil {
		t.Fatal(err)
	}
	foreign := job
	foreign.DeliveryID, foreign.SnapshotID, foreign.HostBinding = "foreign-binding", "foreign-binding", strings.Repeat("f", 64)
	source, err = cfg.OpenSource(closed.Artifact.SourceRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Admit(t.Context(), "upload", foreign, source); err != nil {
		t.Fatal(err)
	}
	_ = source.Close()
	root := cfg.sourcePath
	_ = cfg.Close()
	_ = q.Close()
	if err := os.RemoveAll(root); err != nil { // Only this isolated fixture's producer source, never queue bytes.
		t.Fatal(err)
	}
	f.setDrop("PATCH /bots/" + job.BotID + "/media/uploads/receipt-1")
	var got QueueStatus
	for range 6 {
		got, err = AdvanceSnapshotProducer(t.Context(), getenv, "fixture", limits, server.Client(), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if got.Associated {
			break
		}
	}
	if !got.Associated || got.Job.DeliveryID != job.DeliveryID || got.Job.HostBinding != job.HostBinding || got.Job.Task != job.Task || got.Job.ReceiptID != "receipt-1" || !bytes.Equal(fixtureReady(f), []byte("fixture")) || commits.Load() != 1 || f.count("POST /bots/"+job.BotID+"/media/uploads") != 1 {
		t.Fatal("snapshot lost identity/bytes/receipt")
	}
	if _, err := AdvanceSnapshotProducer(t.Context(), getenv, "fixture", limits, server.Client(), time.Now().Add(time.Hour)); !errors.Is(err, ErrNoWork) {
		t.Fatal("registered/download/foreign record entered snapshot capability", err)
	}
	q, err = OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	after, err := q.Inspect(registered.DeliveryID)
	a, _ := json.Marshal(after)
	b, _ := json.Marshal(before)
	if err != nil || !bytes.Equal(a, b) || after.Activated || after.Admitted || after.Seal != nil {
		t.Fatal("metadata-only registration changed/activated", err)
	}
	for _, suffix := range []string{".snapshot", ".tree", ".admission"} {
		if _, err := q.root.Lstat(queueName(registered.DeliveryID + suffix)); !os.IsNotExist(err) {
			t.Fatal("registered bytes/index were opened", suffix, err)
		}
	}
	t.Log("source deleted; sealed TLS upload reconciled lost PATCH; Create=1 commit=1; registered journal unchanged; downloads/foreign binding excluded; no source activation")
}

func TestSnapshotProducerRejectsExplicitRegisteredAndIndexedJobs(t *testing.T) {
	getenv, closed, _ := producerFixture(t, "https://fixture.invalid")
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	raw, _ := json.Marshal(closed)
	action, _, err := ProducerAdmission(bytes.NewReader(raw), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	action.Job.HostBinding = cfg.Binding
	limits := QueueLimits{Jobs: 4, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	q, err := OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	r, err := q.RegisterUpload(t.Context(), action.Job, closed.Artifact.SourceRef, cfg.OpenSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, indexed := range []bool{false, true} {
		if indexed {
			q.openSource = cfg.OpenSource
			status, journal, err := q.load(r.Job.DeliveryID)
			if err != nil {
				t.Fatal(err)
			}
			r, err = q.prepareRegistered(t.Context(), status, journal, time.Now())
			_ = journal.Close()
			if err != nil || !r.Admitted || r.Seal == nil {
				t.Fatal("real source-indexed fixture", err)
			}
		}
		before, _ := json.Marshal(r)
		if _, err := InvokeConfiguredHerdr(t.Context(), getenv, "fixture", limits, ConfiguredAction{Kind: "advance-snapshot", DeliveryID: r.Job.DeliveryID}, nil, time.Now()); !errors.Is(err, ErrNoWork) {
			t.Fatal("explicit source-backed advance was accepted", indexed, err)
		}
		after, err := q.Inspect(r.Job.DeliveryID)
		actual, _ := json.Marshal(after)
		if err != nil || !bytes.Equal(before, actual) {
			t.Fatal("rejected source job was modified", err)
		}
	}
	_ = q.Close()
}

func TestSnapshotProducerMissingPrivateBytesFailsBeforeNetwork(t *testing.T) {
	getenv, closed, _ := producerFixture(t, "https://fixture.invalid")
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	raw, _ := json.Marshal(closed)
	action, _, err := ProducerAdmission(bytes.NewReader(raw), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	action.Job.HostBinding = cfg.Binding
	limits := QueueLimits{Jobs: 2, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	q, err := OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	source, err := cfg.OpenSource(closed.Artifact.SourceRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Admit(t.Context(), "upload", action.Job, source); err != nil {
		t.Fatal(err)
	}
	_ = source.Close()
	before, err := q.Inspect(action.Job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeQueueFile(q.root, queueName(action.Job.DeliveryID+".snapshot")); err != nil {
		t.Fatal(err)
	}
	_ = q.Close()
	// The reserved invalid HTTPS origin has no server. Correct rejection occurs
	// from the private snapshot handle before any HTTP/create/association call.
	if _, err := AdvanceSnapshotProducer(t.Context(), getenv, "fixture", limits, nil, time.Now()); err == nil || errors.Is(err, ErrTransport) {
		t.Fatal("missing bytes reached transport or passed", err)
	}
	q, err = OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	after, err := q.Inspect(action.Job.DeliveryID)
	a, _ := json.Marshal(after)
	b, _ := json.Marshal(before)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("missing snapshot mutated original job")
	}
}

func TestSnapshotProducerRetainsAuthorizationAndTLSGuards(t *testing.T) {
	for _, kind := range []string{"revoked", "untrusted-TLS", "missing-credential"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "Memoh-Transfer fixture-stable-device" {
					t.Error("original device credential was not retained")
				}
				w.WriteHeader(http.StatusForbidden)
			}))
			defer server.Close()
			getenv, closed, _ := producerFixture(t, server.URL)
			cfg, err := OpenHostConfig(getenv)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(closed)
			action, _, err := ProducerAdmission(bytes.NewReader(raw), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			action.Job.HostBinding = cfg.Binding
			limits := QueueLimits{Jobs: 2, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
			q, err := OpenHerdrQueue(getenv, "fixture", limits)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = q.Close() }()
			source, err := cfg.OpenSource(closed.Artifact.SourceRef)
			if err != nil {
				t.Fatal(err)
			}
			if err := q.Admit(t.Context(), "upload", action.Job, source); err != nil {
				t.Fatal(err)
			}
			_ = source.Close()
			_ = cfg.Close()
			transport := server.Client()
			if kind == "untrusted-TLS" {
				transport = nil // System roots must reject this isolated self-signed peer.
			}
			if kind == "missing-credential" {
				r, err := os.OpenRoot(getenv("HERDR_PLUGIN_CONFIG_DIR"))
				if err != nil {
					t.Fatal(err)
				}
				if err := r.Remove("device.credential"); err != nil {
					t.Fatal(err)
				}
				_ = r.Close()
			}
			_, _ = AdvanceSnapshotProducer(t.Context(), getenv, "fixture", limits, transport, time.Now())
			got, err := q.Inspect(action.Job.DeliveryID)
			if err != nil || got.Associated || got.Job.Ready != nil || got.Job.HostBinding != action.Job.HostBinding || got.Job.Task != action.Job.Task {
				t.Fatal("guard lost original identity or accepted a transfer", err)
			}
			if kind == "revoked" && (requests.Load() != 1 || got.Job.ErrorCode != "authorization" || got.Job.Phase != "blocked") {
				t.Fatal("revoked credential was retried or accepted", requests.Load(), got.Job.Phase, got.Job.ErrorCode)
			}
			if kind != "revoked" && requests.Load() != 0 {
				t.Fatal("TLS/credential guard reached HTTP application")
			}
			if _, release, err := q.openSnapshot(got); err != nil {
				t.Fatal("rejected transfer discarded private recovery bytes", err)
			} else {
				release()
			}
		})
	}
}
