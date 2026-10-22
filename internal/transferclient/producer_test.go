package transferclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
)

func producerFixture(t *testing.T, origin string) (func(string) string, deliverymeta.Closed, func(string, string, []byte)) {
	t.Helper()
	config, state, source := configFixtureRoot(t), configFixtureRoot(t), configFixtureRoot(t)
	write := func(root, name string, data []byte) {
		t.Helper()
		r, err := os.OpenRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		f, err := openPrivateFile(r, name, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		if f.Truncate(0) != nil {
			t.Fatal("truncate")
		}
		if _, err = f.Write(data); err != nil {
			t.Fatal(err)
		}
		if f.Sync() != nil {
			t.Fatal("sync")
		}
	}
	raw, _ := json.Marshal(hostConfigWire{Version: 1, Origin: origin, SourceRoot: source})
	write(config, "transfer.json", raw)
	write(config, "device.credential", []byte("fixture-stable-device"))
	closed := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "fixture-run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "result-one", Mime: "text/plain"}}, Closed: true, SHA256: digest([]byte("fixture")), SizeBytes: 7}
	write(source, closed.Artifact.SourceRef, []byte("fixture"))
	getenv := func(key string) string {
		switch key {
		case "HERDR_PLUGIN_CONFIG_DIR":
			return config
		case "HERDR_PLUGIN_STATE_DIR":
			return state
		}
		return ""
	}
	return getenv, closed, func(root, name string, raw []byte) {
		if root == "source" {
			root = source
		}
		write(root, name, raw)
	}
}

// Runs against real private host storage on Unix and Windows NTFS. The HTTP
// service in THIS test is a protocol fixture, not the production Server or PG.
func TestProducerClosedManifestRestartAndLostAssociation(t *testing.T) {
	exerciseProducerRecovery(t, false)
}
func TestProducerCollectMissingSourceStillAdvances(t *testing.T) { exerciseProducerRecovery(t, true) }

func TestProducerBadManifestDoesNotStarveValidDelivery(t *testing.T) {
	for _, kind := range []string{"oversize", "malformed", "name-mismatch", "root-mismatch", "trusted-plan"} {
		t.Run(kind, func(t *testing.T) { exerciseProducerRecovery(t, false, kind) })
	}
}

func exerciseProducerRecovery(t *testing.T, missingSource bool, invalidManifest ...string) {
	t.Helper()
	f := &fixture{calls: map[string]int{}}
	var committed atomic.Bool
	var commits atomic.Int64
	var closed deliverymeta.Closed
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/internal/computer-transfers/" + closed.Task.OperationID + "/" + closed.DeliveryID()
		if r.Header.Get("Authorization") != "Memoh-Transfer fixture-stable-device" || !strings.HasPrefix(r.URL.Path, prefix+"/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.Path, "/internal/computer-deliveries/") {
			outcome := "absent"
			if strings.HasSuffix(r.URL.Path, "/commit") {
				commits.Add(1)
				committed.Store(true)
				w.WriteHeader(499)
				return
			}
			if committed.Load() {
				outcome = "committed"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 3, "revision": "1", "outcome": outcome})
			return
		}
		// Adapt this fixture's URL and auth only after checking the task transport.
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		r.Header.Set("Authorization", syntheticAuth)
		f.serve(w, r)
	}))
	defer https.Close()
	getenv, c, write := producerFixture(t, https.URL)
	closed = c
	if len(invalidManifest) != 0 && (invalidManifest[0] == "root-mismatch" || invalidManifest[0] == "trusted-plan") {
		candidate := closed
		candidate.Artifact.SourceRef = "foreign"
		if candidate.ManifestName() > closed.ManifestName() {
			closed = candidate
			write("source", closed.Artifact.SourceRef, []byte("fixture"))
		}
	}
	limits := QueueLimits{Jobs: 2, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	raw, _ := json.Marshal(closed)
	plan := closed.Plan
	badName := deliverymeta.ManifestPrefix + strings.Repeat("0", 64)
	var badRaw []byte
	if len(invalidManifest) != 0 {
		switch invalidManifest[0] {
		case "oversize":
			badRaw = []byte(strings.Repeat("x", 16385))
		case "malformed":
			badRaw = []byte(`{"private":"must-not-be-forwarded"`)
		case "name-mismatch":
			badRaw = raw
		case "root-mismatch", "trusted-plan":
			foreign := closed
			foreign.Task.RootHash = strings.Repeat("0", 64)
			foreign.Artifact.SourceRef = "foreign"
			if closed.Artifact.SourceRef == "foreign" {
				foreign.Artifact.SourceRef = "result-one"
			}
			badName = foreign.ManifestName()
			badRaw, _ = json.Marshal(foreign)
			if invalidManifest[0] == "trusted-plan" {
				plan = deliverymeta.Plan{Version: 2, Task: closed.Task, Artifacts: []deliverymeta.Artifact{foreign.Artifact, closed.Artifact}}
				badRaw = []byte(`{"private":"invalid metadata"`)
			}
		}
		if badName >= closed.ManifestName() {
			t.Fatal("invalid fixture is not first")
		}
		write("source", badName, badRaw)
	}
	writeProducerContextFixture(t, getenv, plan)
	write("source", closed.ManifestName(), raw)
	f.setDrop("PATCH /bots/" + closed.Task.BotID + "/media/uploads/receipt-1")
	status, err := CollectProducer(t.Context(), getenv, "fixture", limits, https.Client(), time.Now())
	if (len(invalidManifest) == 0 && err != nil || len(invalidManifest) != 0 && !errors.Is(err, ErrProducerSource)) || !status.Admitted || status.Job.ReceiptID == "" || status.Job.NextAttempt.IsZero() {
		t.Fatal("first finite action", status.Job.Phase, err)
	}
	receipt := status.Job.ReceiptID
	// Source changes after sealing; a replayed manifest must recover the snapshot.
	write("source", "result-one", []byte("changed"))
	write("source", closed.ManifestName(), raw)
	if missingSource {
		cfg, openErr := OpenHostConfig(getenv)
		if openErr != nil {
			t.Fatal(openErr)
		}
		path := cfg.sourcePath
		_ = cfg.Close()
		if removeErr := os.RemoveAll(path); removeErr != nil {
			t.Fatal(removeErr)
		} // Only the private test source, after snapshot admission.
	}
	for range 6 {
		now := time.Now().Add(time.Hour)
		status, err = CollectProducer(t.Context(), getenv, "fixture", limits, https.Client(), now)
		switch {
		case missingSource:
			if !errors.Is(err, ErrProducerSource) || errors.Is(err, ErrNoWork) {
				t.Fatal("source error was hidden", err)
			}
		case len(invalidManifest) != 0:
			if !errors.Is(err, ErrProducerSource) || errors.Is(err, ErrNoWork) {
				t.Fatal("invalid collection was hidden", err)
			}
		case err != nil:
			t.Fatal("restart", err)
		}
		if status.Associated {
			break
		}
	}
	if !status.Associated || status.Job.ReceiptID != receipt || status.Job.OperationID != closed.Task.OperationID || commits.Load() != 1 || f.count("POST /bots/"+closed.Task.BotID+"/media/uploads") != 1 || string(fixtureReady(f)) != "fixture" {
		t.Fatal("lost original delivery", status.AssociationState, commits.Load())
	}
	if len(invalidManifest) != 0 {
		if err := PublishProducerFeedback(t.Context(), getenv, "fixture", limits, status.Job, time.Now()); err != nil {
			t.Fatal(err)
		}
		report := readProducerFeedbackFixture(t, getenv, plan)
		if invalidManifest[0] == "trusted-plan" {
			if len(report.Files) != 2 || report.Files[0].Code != "manifest_invalid" || report.Files[0].Retryable || report.Files[1].DeliveryID != closed.DeliveryID() {
				t.Fatal("trusted plan error not distinguished")
			}
		} else if len(report.Files) != 1 || report.Files[0].DeliveryID != closed.DeliveryID() {
			t.Fatal("unattributable data assigned to task")
		}
		cfg, err := OpenHostConfig(getenv)
		if err != nil {
			t.Fatal(err)
		}
		file, err := cfg.OpenSource(badName)
		if err != nil {
			t.Fatal("bad evidence missing", err)
		}
		kept, readErr := io.ReadAll(file)
		_ = file.Close()
		_ = cfg.Close()
		if readErr != nil || !bytes.Equal(kept, badRaw) {
			t.Fatal("bad evidence changed")
		}
		idle, err := CollectProducer(t.Context(), getenv, "fixture", limits, https.Client(), time.Now())
		if !errors.Is(err, ErrProducerSource) || !errors.Is(err, ErrProducerManifest) || errors.Is(err, ErrNoWork) || idle.Job.DeliveryID != "" || idle.Associated {
			t.Fatal("bad-only source guessed identity or success", err)
		}
	}
	if _, err = AdvanceProducer(t.Context(), getenv, "fixture", limits, https.Client(), time.Now()); !errors.Is(err, ErrNoWork) {
		t.Fatal("completed job re-executed", err)
	}
}

func TestProducerInputAndRootFailClosed(t *testing.T) {
	getenv, closed, _ := producerFixture(t, "https://fixture.invalid")
	limits := QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	raw, _ := json.Marshal(closed)
	if _, _, err := ProducerAdmission(bytes.NewReader(append(raw, raw...)), "fixture"); !errors.Is(err, ErrInvalid) {
		t.Fatal("trailing document")
	}
	if _, _, err := ProducerAdmission(strings.NewReader(strings.Repeat(" ", 16385)), "fixture"); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded input")
	}
	closed.Task.RootHash = strings.Repeat("0", 64)
	raw, _ = json.Marshal(closed)
	if _, err := EnqueueProducer(t.Context(), getenv, "fixture", limits, bytes.NewReader(raw), nil, time.Now()); !errors.Is(err, ErrBinding) {
		t.Fatal("root mismatch", err)
	}
	closed.Artifact.SourceRef = "../device.credential"
	raw, _ = json.Marshal(closed)
	if _, _, err := ProducerAdmission(bytes.NewReader(raw), "fixture"); !errors.Is(err, ErrInvalid) {
		t.Fatal("traversal")
	}
}

func TestProducerConfigurationCannotChangeBetweenCheckAndAdmission(t *testing.T) {
	getenv, closed, write := producerFixture(t, "https://fixture.invalid")
	raw, _ := json.Marshal(closed)
	stateReads := 0
	changing := func(key string) string {
		if key == "HERDR_PLUGIN_STATE_DIR" {
			stateReads++
			if stateReads == 2 {
				// The consistency check has returned. Change the resolved export location
				// just before the existing queue/admission boundary rereads configuration.
				changed, _ := json.Marshal(hostConfigWire{Version: 1, Origin: "https://fixture.invalid", SourceRoot: configFixtureRoot(t)})
				write(getenv("HERDR_PLUGIN_CONFIG_DIR"), "transfer.json", changed)
			}
		}
		return getenv(key)
	}
	_, err := EnqueueProducer(t.Context(), changing, "fixture", QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}, bytes.NewReader(raw), nil, time.Now())
	if !errors.Is(err, ErrVersion) {
		t.Fatal("configuration changed silently", err)
	}
}

func TestProducerMissingSourceWithoutQueueIsNotIdleSuccess(t *testing.T) {
	getenv, _, _ := producerFixture(t, "https://fixture.invalid")
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	path := cfg.sourcePath
	_ = cfg.Close()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	status, err := CollectProducer(t.Context(), getenv, "fixture", QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}, nil, time.Now())
	if !errors.Is(err, ErrProducerSource) || errors.Is(err, ErrNoWork) || status.Admitted || status.Associated {
		t.Fatal("missing producer was reported as complete/idle", err)
	}
}
