//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/transferclient"
)

// Real command dispatcher in fresh processes, HTTPS and NTFS publication. The
// NAS is an explicit protocol fixture, not production Server/PostgreSQL. Roots
// are injected only into the existing test helper; system trust is untouched.
func TestCLIPathInboundFeedbackRestart(t *testing.T) {
	exerciseCLIInboundFeedback(t, false)
}

func TestCLIPathRuntimeReconnectDownload(t *testing.T) {
	exerciseCLIInboundFeedback(t, true)
}

func exerciseCLIInboundFeedback(t *testing.T, reconnect bool) {
	t.Helper()
	root := cliPrivateRoot(t)
	config, state := filepath.Join(root, "config"), filepath.Join(root, "state")
	for _, path := range []string{config, state} {
		if err := cliFixtureDir(path); err != nil {
			t.Fatal(err)
		}
	}
	data := bytes.Repeat([]byte("inbound-feedback-fixture"), 4096)
	if reconnect {
		// One existing 8 MiB checkpoint plus a small tail, not another 1 GiB
		// acceptance run. The first body is disconnected after that checkpoint.
		data = append(bytes.Repeat([]byte{0x5a}, int(transferclient.MaxChunkBytes)), data...)
	}
	sum := sha256.Sum256(data)
	job := transferclient.Job{Version: 1, DeliveryID: "inbound-feedback", SnapshotID: "inbound-feedback", BotID: "bot-1", OperationID: "op-1", LocatorRevision: "native-profile-v1", Phase: "queued", Task: transferclient.DeviceTask{UserID: "user-1", SessionID: "session-1", TargetID: "target-1", RunID: "run-1", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}, Spec: transferclient.Spec{SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), Mime: "application/octet-stream"}}
	ref := transferclient.Reference{DeliveryID: job.DeliveryID, OperationID: job.OperationID, BotID: job.BotID, ContentHash: job.Spec.SHA256, SizeBytes: job.Spec.SizeBytes, Mime: job.Spec.Mime}
	var mu sync.Mutex
	requests, gets, commits, ledgerEntries, sent := 0, 0, 0, 0, 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		prefix := "/internal/computer-transfers/" + job.OperationID + "/" + job.DeliveryID
		if r.Header.Get("Authorization") != "Memoh-Transfer synthetic-inbound-device" || r.Header.Get("Memoh-Transfer-Context") == "" || !strings.HasPrefix(r.URL.Path, prefix+"/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/limits"):
			_ = json.NewEncoder(w).Encode(transferclient.Limits{MaxUploadBytes: 1 << 20, ChunkMaxBytes: 8 << 20, ResumableEnabled: true})
		case strings.HasSuffix(r.URL.Path, "/media/"+job.Spec.SHA256):
			offset := 0
			if reconnect && gets == 1 && r.Method == http.MethodGet {
				offset = int(transferclient.MaxChunkBytes)
			}
			wantRange := ""
			if offset > 0 {
				wantRange = "bytes=" + strconv.Itoa(offset) + "-"
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead || r.Header.Get("Range") != wantRange || offset > 0 && r.Header.Get("If-Range") != `"`+job.Spec.SHA256+`"` {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("ETag", `"`+job.Spec.SHA256+`"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)-offset))
			if offset > 0 {
				w.Header().Set("Content-Range", "bytes "+strconv.Itoa(offset)+"-"+strconv.Itoa(len(data)-1)+"/"+strconv.Itoa(len(data)))
				w.WriteHeader(http.StatusPartialContent)
			}
			if r.Method == http.MethodGet {
				gets++
				if reconnect && gets == 1 {
					n, _ := w.Write(data[:transferclient.MaxChunkBytes])
					sent += n
					w.(http.Flusher).Flush()
					connection, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = connection.Close()
					}
					return
				}
				n, _ := w.Write(data[offset:])
				sent += n
			}
		case strings.Contains(r.URL.Path, "/internal/computer-deliveries/"+job.OperationID+"/"+job.DeliveryID+"/"):
			var body struct {
				transferclient.Reference
				Direction   string `json:"direction"`
				Publication string `json:"publication"`
			}
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil || body.BotID != ref.BotID || body.ContentHash != ref.ContentHash || body.SizeBytes != ref.SizeBytes || body.Mime != ref.Mime || body.Direction != "download" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/commit") {
				if body.Publication != "published" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				commits++
				ledgerEntries = 1
			}
			outcome := "absent"
			if ledgerEntries == 1 {
				outcome = "committed"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 3, "revision": "1", "outcome": outcome})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": server.URL, "source_root": ""})
	certPath := filepath.Join(root, "fixture.der")
	for path, raw := range map[string][]byte{filepath.Join(config, "transfer.json"): settings, filepath.Join(config, "device.credential"): []byte("synthetic-inbound-device"), certPath: server.Certificate().Raw} {
		if err := cliFixtureWrite(path, raw); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	input, _ := json.Marshal(admissionInput{Job: job})
	invoke := func(kind string) map[string]json.RawMessage {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestCLIPathChild$") //nolint:gosec // Existing test executable, fixed arguments, no shell.
		id := job.DeliveryID
		if kind == "advance-results" || kind == "lifecycle-summary" {
			id = ""
		}
		cmd.Env = append(os.Environ(), "MEMOH_CLI_TEST_CHILD=1", "MEMOH_CLI_TEST_CERT="+certPath, "MEMOH_CLI_TEST_NOW="+now.Format(time.RFC3339Nano), "MEMOH_CLI_TEST_ACTION="+kind, "MEMOH_CLI_TEST_DELIVERY="+id, "HERDR_PLUGIN_CONFIG_DIR="+config, "HERDR_PLUGIN_STATE_DIR="+state)
		cmd.Stdin = bytes.NewReader(input)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("finite %s failed: %v output=%s stderr=%s", kind, err, out.String(), stderr.String())
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(out.Bytes(), &result) != nil {
			t.Fatal("invalid CLI JSON", out.String())
		}
		if kind != "lifecycle-summary" && (string(result["feedback_status"]) != `"not_applicable"` || string(result["delivery_id"]) != `"`+job.DeliveryID+`"`) {
			t.Fatal("inbound command lost its feedback applicability/identity", out.String())
		}
		for _, private := range []string{"synthetic-inbound-device", config, state, server.URL} {
			if strings.Contains(out.String(), private) || strings.Contains(stderr.String(), private) {
				t.Fatal("private configuration leaked")
			}
		}
		return result
	}
	if enqueued := invoke("enqueue-download"); string(enqueued["associated"]) != "false" {
		t.Fatal("not_applicable is not completion")
	}
	if reconnect {
		waiting := invoke("advance-results")
		var deadline time.Time
		if string(waiting["state"]) != `"waiting"` || string(waiting["error_class"]) != `"transient"` || string(waiting["offset"]) != strconv.FormatInt(transferclient.MaxChunkBytes, 10) || string(waiting["associated"]) != "false" || json.Unmarshal(waiting["next_due_at"], &deadline) != nil || !deadline.After(now) {
			t.Fatal("disconnection did not preserve its checkpoint/backoff", waiting)
		}
		beforeFiles := runtimeWakeFiles(t, root)
		mu.Lock()
		beforeRequests := requests
		mu.Unlock()
		for _, event := range []string{"startup", "network", "network"} {
			got := invoke("lifecycle-summary")
			if len(got) != 4 || string(got["state"]) != `"waiting"` || !bytes.Equal(got["next_due_at"], waiting["next_due_at"]) {
				t.Fatal("reconnect changed producer deadline", event, got)
			}
		}
		mu.Lock()
		gotRequests := requests
		mu.Unlock()
		if gotRequests != beforeRequests || !reflect.DeepEqual(beforeFiles, runtimeWakeFiles(t, root)) {
			t.Fatal("reconnect changed queue/config or replayed bytes")
		}
		now = deadline.Add(time.Millisecond)
	}
	completed := invoke("advance-results")
	var gotRef transferclient.Reference
	if string(completed["associated"]) != "true" || string(completed["phase"]) != `"ready"` || json.Unmarshal(completed["reference"], &gotRef) != nil || gotRef != ref {
		t.Fatal("download did not complete with original reference", completed)
	}
	mu.Lock()
	before := requests
	wantGets := 1
	if reconnect {
		wantGets = 2
	}
	if gets != wantGets || sent != len(data) || commits != 1 || ledgerEntries != 1 {
		t.Fatal("duplicate or missing transfer/association", gets, commits, ledgerEntries)
	}
	mu.Unlock()
	for range 2 {
		if reconnect {
			idle := invoke("lifecycle-summary")
			if string(idle["state"]) != `"idle"` {
				t.Fatal("completed reconnect scheduled more work", idle)
			}
		}
		for _, kind := range []string{"enqueue-download", "inspect"} {
			got := invoke(kind)
			if string(got["associated"]) != "true" || !bytes.Equal(got["reference"], completed["reference"]) || !bytes.Equal(got["offset"], completed["offset"]) {
				t.Fatal("fresh process lost completed download", got)
			}
		}
	}
	mu.Lock()
	if requests != before {
		t.Fatal("completed reopen replayed network work", before, requests)
	}
	mu.Unlock()
	q, err := transferclient.OpenHerdrQueue(func(string) string { return state }, "native-profile-v1", transferclient.DefaultHostQueueLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	persisted, err := q.Inspect(job.DeliveryID)
	if err != nil || persisted.Job.Task != job.Task || persisted.Job.OperationID != job.OperationID || persisted.Job.Ready == nil || *persisted.Job.Ready != ref || !persisted.Associated {
		t.Fatal("durable task/reference changed", err)
	}
	p, err := q.OpenNativePublication(func(ctx context.Context, candidate transferclient.Reference) error {
		if candidate != ref {
			return transferclient.ErrAuthorization
		}
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	file, err := p.OpenPublished(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	got, err := io.ReadAll(io.LimitReader(file, int64(len(data))+1))
	if err != nil || !bytes.Equal(got, data) || sha256.Sum256(got) != sum {
		t.Fatal("published bytes/hash changed", err)
	}
	if reconnect {
		t.Logf("bytes=%d checkpoint=%d GET=%d Commit=%d ledger_entries=%d confirmed_prefix_retransmitted=0 wake_mutations=0 completed_reopen=2; real NTFS/CLI, synthetic reconnect hints and NAS protocol", len(data), transferclient.MaxChunkBytes, gets, commits, ledgerEntries)
	}
}
