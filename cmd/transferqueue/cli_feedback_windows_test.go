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
	root := cliPrivateRoot(t)
	config, state := filepath.Join(root, "config"), filepath.Join(root, "state")
	for _, path := range []string{config, state} {
		if err := cliFixtureDir(path); err != nil {
			t.Fatal(err)
		}
	}
	data := bytes.Repeat([]byte("inbound-feedback-fixture"), 4096)
	sum := sha256.Sum256(data)
	job := transferclient.Job{Version: 1, DeliveryID: "inbound-feedback", SnapshotID: "inbound-feedback", BotID: "bot-1", OperationID: "op-1", LocatorRevision: "native-profile-v1", Phase: "queued", Task: transferclient.DeviceTask{UserID: "user-1", SessionID: "session-1", TargetID: "target-1", RunID: "run-1", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}, Spec: transferclient.Spec{SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), Mime: "application/octet-stream"}}
	ref := transferclient.Reference{DeliveryID: job.DeliveryID, OperationID: job.OperationID, BotID: job.BotID, ContentHash: job.Spec.SHA256, SizeBytes: job.Spec.SizeBytes, Mime: job.Spec.Mime}
	var mu sync.Mutex
	requests, gets, commits, ledgerEntries := 0, 0, 0, 0
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
			if r.Method != http.MethodGet && r.Method != http.MethodHead || r.Header.Get("Range") != "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("ETag", `"`+job.Spec.SHA256+`"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			if r.Method == http.MethodGet {
				gets++
				_, _ = w.Write(data)
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
		if kind == "advance-results" {
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
		if json.Unmarshal(out.Bytes(), &result) != nil || string(result["feedback_status"]) != `"not_applicable"` || string(result["delivery_id"]) != `"`+job.DeliveryID+`"` {
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
	completed := invoke("advance-results")
	var gotRef transferclient.Reference
	if string(completed["associated"]) != "true" || string(completed["phase"]) != `"ready"` || json.Unmarshal(completed["reference"], &gotRef) != nil || gotRef != ref {
		t.Fatal("download did not complete with original reference", completed)
	}
	mu.Lock()
	before := requests
	if gets != 1 || commits != 1 || ledgerEntries != 1 {
		t.Fatal("duplicate or missing transfer/association", gets, commits, ledgerEntries)
	}
	mu.Unlock()
	for range 2 {
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
}
