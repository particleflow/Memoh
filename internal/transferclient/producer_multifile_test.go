package transferclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// Real host queue and repeated finite actions; HTTPS NAS/association endpoints
// here are synthetic. Aggregate task status has separate ledger/CAS tests.
func TestProducerMultiplePartialRestartAndDeniedFile(t *testing.T) {
	exerciseProducerDeniedPosition(t, 2, 1)
}

func TestProducerFeedbackDeniedFirstAndMiddle(t *testing.T) {
	for _, slots := range []int{1, 2} {
		for _, position := range []int{0, 1} {
			t.Run(fmt.Sprintf("slots-%d-denied-%d", slots, position), func(t *testing.T) {
				exerciseProducerDeniedPosition(t, position, slots)
			})
		}
	}
}

func exerciseProducerDeniedPosition(t *testing.T, deniedPosition, slots int) {
	t.Helper()
	type remote struct {
		upload    *fixture
		committed atomic.Bool
		commits   atomic.Int64
		denied    bool
	}
	remotes := map[string]*remote{}
	var operation string
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/internal/computer-transfers/"), "/", 3)
		if len(parts) != 3 || parts[0] != operation || r.Header.Get("Authorization") != "Memoh-Transfer fixture-stable-device" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		state := remotes[parts[1]]
		if state == nil || state.denied {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.HasPrefix(parts[2], "internal/computer-deliveries/") {
			if strings.HasSuffix(parts[2], "/commit") {
				state.commits.Add(1)
				state.committed.Store(true)
				w.WriteHeader(499)
				return
			}
			outcome := "absent"
			if state.committed.Load() {
				outcome = "committed"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 3, "revision": "1", "outcome": outcome})
			return
		}
		r.URL.Path = "/" + parts[2]
		r.Header.Set("Authorization", syntheticAuth)
		state.upload.serve(w, r)
	}))
	defer https.Close()
	getenv, one, write := producerFixture(t, https.URL)
	operation = one.Task.OperationID
	files := []deliverymeta.Closed{one, one, one}
	files[1].Artifact.SourceRef = "result-two"
	files[2].Artifact.SourceRef = "result-three"
	sort.Slice(files, func(i, j int) bool { return files[i].ManifestName() < files[j].ManifestName() })
	plan := deliverymeta.Plan{Version: 2, Task: one.Task}
	for _, file := range files {
		plan.Artifacts = append(plan.Artifacts, file.Artifact)
	}
	writeProducerContextFixture(t, getenv, plan)
	for i, file := range files {
		state := &remote{upload: &fixture{calls: map[string]int{}}, denied: i == deniedPosition}
		if i == 1 {
			state.upload.setDrop("PATCH /bots/" + file.Task.BotID + "/media/uploads/receipt-1")
		}
		remotes[file.DeliveryID()] = state
		write("source", file.Artifact.SourceRef, []byte("fixture"))
		raw, _ := json.Marshal(file)
		write("source", file.ManifestName(), raw)
	}
	limits := QueueLimits{Jobs: slots, Bytes: int64(slots) * 16384, JournalBytes: 65536, Workers: 1}
	for range 12 {
		status, err := CollectProducer(t.Context(), getenv, "fixture", limits, https.Client(), time.Now().Add(time.Hour))
		if err != nil && !errors.Is(err, ErrNoWork) && !errors.Is(err, ErrCapacity) {
			t.Fatal("finite hook", err)
		}
		if status.Job.DeliveryID != "" {
			if err := PublishProducerFeedback(t.Context(), getenv, "fixture", limits, status.Job, time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	q, err := OpenHerdrQueue(getenv, "fixture", limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	report := readProducerFeedbackFixture(t, getenv, plan)
	for i, file := range files {
		if slots == 1 && i > deniedPosition {
			if report.Files[i].Code != "capacity_blocked" || report.Files[i].Retryable {
				t.Fatal("full blocked slot not reported", report.Files[i])
			}
			if remotes[file.DeliveryID()].upload.count("POST /bots/"+file.Task.BotID+"/media/uploads") != 0 {
				t.Fatal("unadmitted file uploaded")
			}
			continue
		}
		r, err := q.Inspect(file.DeliveryID())
		if err != nil || r.Job.OperationID != operation {
			t.Fatal("original operation lost", err)
		}
		remote := remotes[file.DeliveryID()]
		if i == deniedPosition {
			if report.Files[i].Code != "authorization" || report.Files[i].Retryable {
				t.Fatal("denial not reported", report.Files[i])
			}
			if r.Associated || r.Job.Phase != "blocked" || r.PayloadState != "" || remote.upload.count("POST /bots/"+file.Task.BotID+"/media/uploads") != 0 {
				t.Fatal("denied file was delivered")
			}
			continue
		}
		if !r.Associated || r.PayloadState != PayloadReleased || remote.commits.Load() != 1 || remote.upload.count("POST /bots/"+file.Task.BotID+"/media/uploads") != 1 {
			t.Fatal("partial recovery duplicated upload")
		}
		// A late duplicate close hook keeps the same receipt after payload release.
		raw, _ := json.Marshal(file)
		write("source", file.ManifestName(), raw)
	}
	_ = q.Close()
	for range 3 {
		if _, err := CollectProducer(t.Context(), getenv, "fixture", limits, https.Client(), time.Now().Add(2*time.Hour)); err != nil && !errors.Is(err, ErrNoWork) && !errors.Is(err, ErrCapacity) {
			t.Fatal("duplicate hook", err)
		}
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	for i, file := range files {
		source, err := cfg.OpenSource(file.Artifact.SourceRef)
		if err != nil {
			t.Fatal("user source deleted", err)
		}
		_ = source.Close()
		if slots == 1 && i > deniedPosition {
			if _, err := cfg.sources.Lstat(file.ManifestName()); err != nil {
				t.Fatal("unadmitted evidence removed", err)
			}
			continue
		}
		if _, err := cfg.sources.Lstat(file.ManifestName()); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("manifest not consumed", err)
		}
	}
}
