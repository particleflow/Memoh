package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/transferclient"
)

// The host owns profile selection and its single-instance gate. These are real
// CLI child processes with explicit locator pairs, not a native host connector
// or proof that Herdr's unprofiled legacy completion hook has been disabled.
func TestCLIPathSelectedProfileIsolation(t *testing.T) {
	root := cliPrivateRoot(t)
	config, state, source := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source")
	principal := "memoh\x0011111111-1111-4111-8111-111111111111\x0011111111-1111-4111-8111-111111111111\x00synthetic-herdr"
	hash := sha256.Sum256([]byte(principal))
	profile := hex.EncodeToString(hash[:])
	nativeConfig, nativeState := filepath.Join(config, "profiles", profile), filepath.Join(state, "profiles", profile)
	for _, path := range []string{config, state, source, filepath.Join(config, "profiles"), filepath.Join(state, "profiles"), nativeConfig, nativeState} {
		if err := cliFixtureDir(path); err != nil {
			t.Fatal(err)
		}
	}
	data := []byte("shared immutable synthetic original")
	contentHash := sha256.Sum256(data)
	write := func(path string, data []byte) {
		t.Helper()
		if err := cliFixtureWrite(path, data); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(source, "original"), data)
	job := transferclient.Job{Version: 1, DeliveryID: "same-logical-id", SnapshotID: "same-logical-id", BotID: uuid.NewString(), OperationID: uuid.NewString(), LocatorRevision: "native-profile-v1", Phase: "queued", Task: transferclient.DeviceTask{UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "synthetic-run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}, Spec: transferclient.Spec{SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(contentHash[:]), Mime: "application/octet-stream"}}
	nativeJob := job
	nativeJob.OperationID, nativeJob.Task.TargetID = uuid.NewString(), uuid.NewString()
	var mu sync.Mutex
	requests, wrongRequests := 0, 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		if r.Header.Get("Authorization") != "Memoh-Transfer synthetic-native-profile" || !strings.HasPrefix(r.URL.Path, "/internal/computer-transfers/"+nativeJob.OperationID+"/") {
			wrongRequests++
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": server.URL, "source_root": source})
	// Identical transfer.json deliberately means identical HostBinding. Physical
	// profile/locator isolation must not depend on changing or reformatting it.
	write(filepath.Join(config, "transfer.json"), settings)
	write(filepath.Join(nativeConfig, "transfer.json"), settings)
	write(filepath.Join(config, "device.credential"), []byte("synthetic-legacy-profile"))
	write(filepath.Join(nativeConfig, "device.credential"), []byte("synthetic-native-profile"))
	cert := filepath.Join(root, "fixture.der")
	write(cert, server.Certificate().Raw)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	invoke := func(config, state, action string, input []byte) map[string]json.RawMessage {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestCLIPathChild$") //nolint:gosec // Current test executable, fixed child entry.
		cmd.Env = append(os.Environ(), "MEMOH_CLI_TEST_CHILD=1", "MEMOH_CLI_TEST_CERT="+cert, "MEMOH_CLI_TEST_NOW="+now.Format(time.RFC3339Nano), "MEMOH_CLI_TEST_ACTION="+action, "MEMOH_CLI_TEST_DELIVERY=", "HERDR_PLUGIN_CONFIG_DIR="+config, "HERDR_PLUGIN_STATE_DIR="+state)
		cmd.Stdin = bytes.NewReader(input)
		var output, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("profile CLI %s: %v output=%s stderr=%s", action, err, output.Bytes(), stderr.Bytes())
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(output.Bytes(), &result) != nil {
			t.Fatal("invalid CLI result", output.String())
		}
		return result
	}
	for _, item := range []struct {
		config, state string
		job           transferclient.Job
	}{{config, state, job}, {nativeConfig, nativeState, nativeJob}} {
		raw, _ := json.Marshal(admissionInput{Job: item.job, SourceRef: "original"})
		invoke(item.config, item.state, "enqueue-upload", raw)
	}
	oldQueue := filepath.Join(state, "deliveries-v1")
	oldFiles, originals := runtimeWakeFiles(t, oldQueue), runtimeWakeFiles(t, source)
	configuration := runtimeWakeFiles(t, config)
	result := invoke(nativeConfig, nativeState, "advance-results", nil)
	// This uses direct queue admissions, not producer manifests. Missing outbound
	// context must retain its existing unavailable guard, never become success.
	if string(result["state"]) != `"waiting"` || string(result["feedback_status"]) != `"unavailable"` {
		t.Fatal("native profile did not retain retryable failure", result)
	}
	if len(result["next_due_at"]) == 0 || string(result["next_due_at"]) == "null" {
		t.Fatal("missing native retry deadline", result)
	}
	mu.Lock()
	beforeSummary := requests
	mu.Unlock()
	nativeQueue := filepath.Join(nativeState, "deliveries-v1")
	nativeFiles := runtimeWakeFiles(t, nativeQueue)
	for range 2 {
		summary := invoke(nativeConfig, nativeState, "lifecycle-summary", nil)
		if string(summary["state"]) != `"waiting"` || !bytes.Equal(summary["next_due_at"], result["next_due_at"]) || !reflect.DeepEqual(nativeFiles, runtimeWakeFiles(t, nativeQueue)) {
			t.Fatal("native summary changed the retry deadline or queue", summary)
		}
	}
	mu.Lock()
	count, wrong := requests, wrongRequests
	mu.Unlock()
	if count == 0 || count != beforeSummary || wrong != 0 || !reflect.DeepEqual(oldFiles, runtimeWakeFiles(t, oldQueue)) || !reflect.DeepEqual(originals, runtimeWakeFiles(t, source)) || !reflect.DeepEqual(configuration, runtimeWakeFiles(t, config)) {
		t.Fatal("selected profile touched the legacy identity, state, credentials or source", count, wrong)
	}
	// Switching back is a read only summary, not an automatic advance/re-enqueue.
	legacy := invoke(config, state, "lifecycle-summary", nil)
	if string(legacy["state"]) != `"due"` || !reflect.DeepEqual(oldFiles, runtimeWakeFiles(t, oldQueue)) {
		t.Fatal("legacy queue did not survive profile switch", legacy)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != count {
		t.Fatal("reading the old profile dispatched network work")
	}
	t.Log("selected native locator retained retry; legacy journal/config/source unchanged; same delivery ID isolated; no old network request; fresh-process summaries did not replay")
}
