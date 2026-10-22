package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
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

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/transferclient"
)

// This helper is compiled only into the test executable, never the shipped CLI.
// Every invocation runs the actual command dispatcher in a fresh process.
func TestCLIPathChild(_ *testing.T) {
	if os.Getenv("MEMOH_CLI_TEST_CHILD") != "1" {
		return
	}
	raw, err := os.ReadFile(os.Getenv("MEMOH_CLI_TEST_CERT")) //nolint:gosec // Test-only local certificate from the parent fixture.
	if err != nil {
		os.Exit(20)
	}
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		os.Exit(21)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	now, err := time.Parse(time.RFC3339Nano, os.Getenv("MEMOH_CLI_TEST_NOW"))
	if err != nil {
		os.Exit(22)
	}
	args := []string{"-action", os.Getenv("MEMOH_CLI_TEST_ACTION")}
	if id := os.Getenv("MEMOH_CLI_TEST_DELIVERY"); id != "" {
		args = append(args, "-delivery", id)
	}
	os.Exit(runCommand(args, os.Stdin, os.Stdout, os.Stderr, os.Getenv, &http.Client{Transport: transport}, func() time.Time { return now }))
}

// The NAS side is an explicit small protocol fixture, not production Server/PG.
// Client dispatch, stdin decoding, queue/NTFS, HTTP/TLS, hashes and outbox are real.
func TestCLIPathTLSRestartAndLostResponses(t *testing.T) {
	root := cliPrivateRoot(t)
	config, state, source := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source")
	for _, path := range []string{config, state, source} {
		if err := cliFixtureDir(path); err != nil {
			t.Fatal(err)
		}
	}
	data := []byte("fixture")
	digest := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	closed := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "fixture-run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "result-one", Mime: "text/plain"}}, Closed: true, SHA256: digest(data), SizeBytes: int64(len(data))}
	var mu sync.Mutex
	var uploaded []byte
	var receipt transferclient.Receipt
	creates, completes, commits, resolves := 0, 0, 0, 0
	committed := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		prefix := "/internal/computer-transfers/" + closed.Task.OperationID + "/" + closed.DeliveryID()
		if r.Header.Get("Authorization") != "Memoh-Transfer synthetic-cli-device" || !strings.HasPrefix(r.URL.Path, prefix+"/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		drop := func() {
			c, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = c.Close()
			}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/limits"):
			_ = json.NewEncoder(w).Encode(transferclient.Limits{MaxUploadBytes: 1 << 20, ChunkMaxBytes: 4, ResumableEnabled: true})
			return
		case strings.Contains(r.URL.Path, "/internal/computer-deliveries/"):
			var ref transferclient.Reference
			if json.NewDecoder(r.Body).Decode(&ref) != nil || ref.BotID != closed.Task.BotID || ref.ContentHash != closed.SHA256 || !strings.Contains(r.URL.Path, "/internal/computer-deliveries/"+closed.Task.OperationID+"/"+closed.DeliveryID()+"/") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/commit") {
				commits++
				committed = true
				drop()
				return
			}
			resolves++
			outcome := "absent"
			if committed {
				outcome = "committed"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 3, "revision": "1", "outcome": outcome})
			return
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/uploads"):
			creates++
			var spec transferclient.Spec
			if json.NewDecoder(r.Body).Decode(&spec) != nil || spec.SHA256 != closed.SHA256 || spec.SizeBytes != closed.SizeBytes {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			receipt = transferclient.Receipt{ID: "receipt-one", State: "uploading", SizeBytes: spec.SizeBytes, Mime: spec.Mime, ExpiresAt: time.Now().Add(24 * time.Hour)}
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPatch:
			chunk, err := io.ReadAll(io.LimitReader(r.Body, 5))
			offset, _ := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
			if err != nil || len(chunk) > 4 || offset != receipt.Offset || r.Header.Get("Upload-Length") != strconv.Itoa(len(chunk)) || r.Header.Get("Upload-SHA256") != digest(chunk) {
				w.WriteHeader(http.StatusConflict)
				return
			}
			uploaded = append(uploaded, chunk...)
			receipt.Offset = int64(len(uploaded))
		case strings.HasSuffix(r.URL.Path, "/complete"):
			if !bytes.Equal(uploaded, data) || digest(uploaded) != closed.SHA256 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			completes++
			receipt.State, receipt.ContentHash = "ready", closed.SHA256
			drop()
			return
		case r.Method != http.MethodGet:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(receipt)
	}))
	defer server.Close()
	write := func(path string, raw []byte) {
		t.Helper()
		if err := cliFixtureWrite(path, raw); err != nil {
			t.Fatal(err)
		}
	}
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": server.URL, "source_root": source})
	write(filepath.Join(config, "transfer.json"), settings)
	write(filepath.Join(config, "device.credential"), []byte("synthetic-cli-device"))
	write(filepath.Join(source, "result-one"), data)
	certPath := filepath.Join(root, "fixture.der")
	write(certPath, server.Certificate().Raw)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	invoke := func(action, id string, input []byte) map[string]json.RawMessage {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bin, "-test.run=^TestCLIPathChild$") //nolint:gosec // Current test executable, fixed arguments; no shell.
		cmd.Env = append(os.Environ(), "MEMOH_CLI_TEST_CHILD=1", "MEMOH_CLI_TEST_CERT="+certPath, "MEMOH_CLI_TEST_NOW="+now.Format(time.RFC3339Nano), "MEMOH_CLI_TEST_ACTION="+action, "MEMOH_CLI_TEST_DELIVERY="+id, "HERDR_PLUGIN_CONFIG_DIR="+config, "HERDR_PLUGIN_STATE_DIR="+state)
		cmd.Stdin = bytes.NewReader(input)
		var output, stderr bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("finite CLI %s failed: %v, stderr=%s status=%s", action, err, stderr.String(), output.String())
		}
		if bytes.Contains(output.Bytes(), []byte("synthetic-cli-device")) || bytes.Contains(output.Bytes(), []byte(source)) || bytes.Contains(output.Bytes(), []byte(server.URL)) {
			t.Fatal("private configuration leaked")
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(output.Bytes(), &result) != nil {
			t.Fatal("CLI did not return one JSON object")
		}
		return result
	}
	raw, _ := json.Marshal(closed)
	enqueued := invoke("enqueue-result", "", raw)
	if string(enqueued["admitted"]) != "true" {
		t.Fatal("stdin admission failed")
	}
	before := invoke("lifecycle-summary", "", nil)
	if string(before["state"]) != `"due"` || len(before) != 4 {
		t.Fatal("wrong initial summary", before)
	}
	// Fresh processes reopen the original journal after each dropped response.
	for range 2 {
		waiting := invoke("advance-results", "", nil)
		if string(waiting["state"]) != `"waiting"` || string(waiting["error_class"]) != `"transient"` {
			t.Fatal("lost response not deferred", waiting)
		}
		var deadline time.Time
		if json.Unmarshal(waiting["next_due_at"], &deadline) != nil || !deadline.After(now) {
			t.Fatal("missing persisted deadline")
		}
		now = deadline.Add(time.Millisecond)
	}
	done := invoke("advance-results", "", nil)
	if string(done["associated"]) != "true" || string(done["state"]) != `"idle"` {
		t.Fatal("association recovery", done)
	}
	inspected := invoke("inspect", closed.DeliveryID(), nil)
	if string(inspected["delivery_id"]) != strconv.Quote(closed.DeliveryID()) || string(inspected["associated"]) != "true" {
		t.Fatal("identity changed")
	}
	mu.Lock()
	defer mu.Unlock()
	if creates != 1 || completes != 1 || commits != 1 || resolves != 1 || !bytes.Equal(uploaded, data) {
		t.Fatal("duplicate mutation or content loss", creates, completes, commits, resolves)
	}
	original, err := os.ReadFile(filepath.Join(source, "result-one")) //nolint:gosec // Fixed test file beneath the private fixture.
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("source changed")
	}
}

func TestCLIPathSummaryMissingConfigurationDoesNotCreateState(t *testing.T) {
	root := cliPrivateRoot(t)
	var output, stderr bytes.Buffer
	code := runCommand([]string{"-action", "lifecycle-summary"}, strings.NewReader(""), &output, &stderr, func(string) string { return root }, nil, time.Now)
	var summary transferclient.Lifecycle
	if code != 1 || json.Unmarshal(output.Bytes(), &summary) != nil || summary.State != "blocked" || summary.ErrorClass != "configuration" {
		t.Fatal("missing configuration decision", code, output.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("summary initialized state")
	}
}

func TestCLIPathCorruptQueueCannotAdvertiseIndependentDue(t *testing.T) {
	root := cliPrivateRoot(t)
	config, state := filepath.Join(root, "config"), filepath.Join(root, "state")
	for _, path := range []string{config, state} {
		if err := cliFixtureDir(path); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(map[string]any{"version": 1, "origin": "https://fixture.invalid"})
	if err := cliFixtureWrite(filepath.Join(config, "transfer.json"), raw); err != nil {
		t.Fatal(err)
	}
	if err := cliFixtureWrite(filepath.Join(config, "device.credential"), []byte("synthetic-device")); err != nil {
		t.Fatal(err)
	}
	env := func(key string) string {
		if key == "HERDR_PLUGIN_CONFIG_DIR" {
			return config
		}
		if key == "HERDR_PLUGIN_STATE_DIR" {
			return state
		}
		return ""
	}
	invoke := func(action string, input []byte) (int, []byte) {
		var output, stderr bytes.Buffer
		code := runCommand([]string{"-action", action}, bytes.NewReader(input), &output, &stderr, env, nil, time.Now)
		return code, output.Bytes()
	}
	for _, id := range []string{"a-corrupt", "b-due"} {
		j := transferclient.Job{Version: 1, DeliveryID: id, SnapshotID: id, BotID: "bot", OperationID: "operation", LocatorRevision: "native-profile-v1", Phase: "queued", Spec: transferclient.Spec{SizeBytes: 7, SHA256: strings.Repeat("a", 64), Mime: "text/plain"}, Task: transferclient.DeviceTask{UserID: "user", SessionID: "session", TargetID: "target", RunID: "run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}}
		input, _ := json.Marshal(admissionInput{Job: j})
		if code, raw := invoke("enqueue-download", input); code != 0 {
			t.Fatalf("admit: %d %s", code, raw)
		}
	}
	queueRoot := filepath.Join(state, "deliveries-v1")
	entries, err := os.ReadDir(queueRoot)
	if err != nil {
		t.Fatal(err)
	}
	var badPath string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".journal") {
			badPath = filepath.Join(queueRoot, entry.Name())
			break
		}
	}
	if badPath == "" {
		t.Fatal("missing admitted journal")
	}
	f, err := os.OpenFile(badPath, os.O_WRONLY|os.O_APPEND, 0) //nolint:gosec // Existing enumerated journal inside this test's private fixture.
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 37)
	frame[3] = 1
	frame[36] = 'x'
	if _, err = f.Write(frame); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	before, err := os.ReadFile(badPath) //nolint:gosec // Fixed private fixture corruption evidence.
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"lifecycle-summary", "advance-results"} {
		code, raw := invoke(action, nil)
		var out struct {
			State      string `json:"state"`
			ErrorClass string `json:"error_class"`
		}
		if code != 1 || json.Unmarshal(raw, &out) != nil || out.State != "unknown" || out.ErrorClass != "unknown" {
			t.Fatalf("impossible due result: %d %s", code, raw)
		}
	}
	after, err := os.ReadFile(badPath) //nolint:gosec // Same private fixture evidence after real CLI dispatch.
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("corrupt proof modified")
	}
}
