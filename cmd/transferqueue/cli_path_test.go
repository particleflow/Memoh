package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
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
	t.Run("trusted-matching-host", func(t *testing.T) { exerciseCLIPathTLS(t, false, false) })
	t.Run("trusted-wrong-host", func(t *testing.T) { exerciseCLIPathTLS(t, true, false) })
}

func TestCLIPathRuntimeReconnectUpload(t *testing.T) {
	exerciseCLIPathTLS(t, false, true)
}

func exerciseCLIPathTLS(t *testing.T, wrongHost, reconnect bool) {
	t.Helper()
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
	requests, creates, patches, completes, commits, resolves := 0, 0, 0, 0, 0, 0
	committed := false
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
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
			patches++
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
	if wrongHost {
		// An isolated trusted certificate with no loopback SAN: keep the real
		// URL hostname and all production TLS verification, without DNS tricks.
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"wrong-host.invalid"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	}
	server.StartTLS()
	defer server.Close()
	if wrongHost {
		roots := x509.NewCertPool()
		roots.AddCert(server.Certificate())
		if _, err := server.Certificate().Verify(x509.VerifyOptions{Roots: roots, DNSName: "wrong-host.invalid"}); err != nil {
			t.Fatal("negative fixture certificate is not trusted", err)
		}
		var hostname x509.HostnameError
		if !errors.As(server.Certificate().VerifyHostname("127.0.0.1"), &hostname) {
			t.Fatal("negative fixture must fail only the requested hostname")
		}
	}
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
	attempt, ok := deliverymeta.AttemptDirectory(closed.Task.Attempt)
	if !ok {
		t.Fatal("invalid original attempt")
	}
	attemptPath := source
	for _, segment := range strings.Split(attempt, "/") {
		attemptPath = filepath.Join(attemptPath, segment)
		if err := cliFixtureDir(attemptPath); err != nil {
			t.Fatal(err)
		}
	}
	contextRaw, _ := json.Marshal(closed.Plan)
	contextPath := filepath.Join(attemptPath, deliverymeta.ProducerContextName)
	write(contextPath, contextRaw)
	checkFeedback := func(result map[string]json.RawMessage) deliverymeta.Feedback {
		t.Helper()
		if string(result["feedback_status"]) != `"recorded"` {
			t.Fatal("original attempt feedback was not recorded", result)
		}
		raw, err := os.ReadFile(filepath.Join(attemptPath, deliverymeta.ProducerFeedbackName)) //nolint:gosec // Fixed original-attempt fixture path.
		feedback, valid := deliverymeta.DecodeFeedback(raw, closed.Plan)
		if err != nil || !valid || feedback.Version != 1 || len(feedback.Files) != 1 || feedback.Files[0].DeliveryID != closed.DeliveryID() {
			t.Fatal("feedback lost the original task/attempt/declaration", err)
		}
		return *feedback
	}
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
		for _, private := range []string{"synthetic-cli-device", source, server.URL} {
			if bytes.Contains(output.Bytes(), []byte(private)) || bytes.Contains(stderr.Bytes(), []byte(private)) {
				t.Fatal("private configuration leaked")
			}
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(output.Bytes(), &result) != nil {
			t.Fatal("CLI did not return one JSON object")
		}
		return result
	}
	raw, _ := json.Marshal(closed)
	enqueued := invoke("enqueue-result", "", raw)
	if string(enqueued["registered"]) != "true" || string(enqueued["admitted"]) != "false" {
		t.Fatal("stdin metadata registration failed")
	}
	checkFeedback(enqueued)
	before := invoke("lifecycle-summary", "", nil)
	if string(before["state"]) != `"due"` || len(before) != 4 {
		t.Fatal("wrong initial summary", before)
	}
	if wrongHost {
		blocked := invoke("advance-results", "", nil)
		if string(blocked["state"]) != `"waiting"` || string(blocked["error_class"]) != `"transient"` || string(blocked["associated"]) != "false" {
			t.Fatal("hostname rejection did not preserve the pending task", blocked)
		}
		checkFeedback(blocked)
		mu.Lock()
		defer mu.Unlock()
		if requests != 0 || creates != 0 || len(uploaded) != 0 {
			t.Fatal("trusted wrong-host certificate reached the HTTP handler")
		}
		t.Log("trusted CA + wrong hostname rejected before HTTP; original attempt feedback recorded")
		return
	}
	// Fresh processes reopen the original journal after each dropped response.
	for range 2 {
		waiting := invoke("advance-results", "", nil)
		if string(waiting["state"]) != `"waiting"` || string(waiting["error_class"]) != `"transient"` {
			t.Fatal("lost response not deferred", waiting)
		}
		checkFeedback(waiting)
		var deadline time.Time
		if json.Unmarshal(waiting["next_due_at"], &deadline) != nil || !deadline.After(now) {
			t.Fatal("missing persisted deadline")
		}
		if reconnect {
			mu.Lock()
			beforeRequests := requests
			mu.Unlock()
			beforeFiles := runtimeWakeFiles(t, root)
			// These labels are host events, not a new producer wire protocol.
			// Reconnect permits a summary read, never an early advance or enqueue.
			for _, event := range []string{"startup", "network", "network"} {
				got := invoke("lifecycle-summary", "", nil)
				if string(got["state"]) != `"waiting"` || !bytes.Equal(got["next_due_at"], waiting["next_due_at"]) || string(got["error_class"]) != `"transient"` {
					t.Fatal("reconnect changed producer backoff", event, got)
				}
			}
			mu.Lock()
			gotRequests := requests
			mu.Unlock()
			if gotRequests != beforeRequests || !reflect.DeepEqual(beforeFiles, runtimeWakeFiles(t, root)) {
				t.Fatal("reconnect changed queue/config/context or replayed network work")
			}
		}
		now = deadline.Add(time.Millisecond)
	}
	done := invoke("advance-results", "", nil)
	if string(done["associated"]) != "true" || string(done["state"]) != `"idle"` {
		t.Fatal("association recovery", done)
	}
	feedback := checkFeedback(done)
	if feedback.Files[0].State != "unknown" || feedback.Files[0].Code != "awaiting_nas_ledger" {
		t.Fatal("local feedback claimed authoritative NAS delivery")
	}
	inspected := invoke("inspect", closed.DeliveryID(), nil)
	if string(inspected["delivery_id"]) != strconv.Quote(closed.DeliveryID()) || string(inspected["associated"]) != "true" {
		t.Fatal("identity changed")
	}
	wantRef := transferclient.Reference{DeliveryID: closed.DeliveryID(), OperationID: closed.Task.OperationID, BotID: closed.Task.BotID, ContentHash: closed.SHA256, SizeBytes: closed.SizeBytes, Mime: closed.Artifact.Mime}
	checkReference := func(result map[string]json.RawMessage) {
		t.Helper()
		var ref transferclient.Reference
		if json.Unmarshal(result["reference"], &ref) != nil || ref != wantRef || string(result["associated"]) != "true" {
			t.Fatal("completed reference changed", result)
		}
	}
	checkReference(done)
	checkReference(inspected)
	getenv := func(key string) string {
		return map[string]string{"HERDR_PLUGIN_CONFIG_DIR": config, "HERDR_PLUGIN_STATE_DIR": state}[key]
	}
	readRecord := func() transferclient.QueueStatus {
		t.Helper()
		limits, err := transferclient.LoadHostQueueLimits(getenv)
		if err != nil {
			t.Fatal(err)
		}
		q, err := transferclient.OpenHerdrQueue(getenv, "native-profile-v1", limits)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = q.Close() }()
		r, err := q.Inspect(closed.DeliveryID())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	completed := readRecord()
	if completed.Job.ReceiptID != "receipt-one" || completed.AssociationState != transferclient.AssociationCommitted {
		t.Fatal("completed durable receipt missing")
	}
	mu.Lock()
	completedRequests := requests
	mu.Unlock()
	for range 2 {
		idle := invoke("advance-results", "", nil)
		if string(idle["state"]) != `"idle"` {
			t.Fatal("completed restart scheduled more work", idle)
		}
		replayed := invoke("enqueue-result", "", raw)
		checkFeedback(replayed)
		checkReference(replayed)
		checkReference(invoke("inspect", closed.DeliveryID(), nil))
		reopened := readRecord()
		if !reopened.Associated || reopened.AssociationState != completed.AssociationState || !reflect.DeepEqual(reopened.Job, completed.Job) {
			t.Fatal("restart changed original task/attempt/receipt/reference")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != completedRequests || creates != 1 || patches != 2 || completes != 1 || commits != 1 || resolves != 1 || !bytes.Equal(uploaded, data) {
		t.Fatal("duplicate mutation or content loss", creates, completes, commits, resolves)
	}
	if raw, err := os.ReadFile(contextPath); err != nil || !bytes.Equal(raw, contextRaw) { //nolint:gosec // Original private attempt fixture only.
		t.Fatal("restart replaced the original producer context")
	}
	original, err := os.ReadFile(filepath.Join(source, "result-one")) //nolint:gosec // Fixed test file beneath the private fixture.
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("source changed")
	}
	t.Log("Complete/Commit response loss recovered; original attempt v1 feedback recorded; two completed-process reopens retained task/receipt/reference with zero additional HTTP requests")
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

func TestCLIPathHistoryMigration(t *testing.T) {
	state := cliPrivateRoot(t)
	env := func(key string) string {
		if key == "HERDR_PLUGIN_STATE_DIR" {
			return state
		}
		return ""
	}
	limits := transferclient.QueueLimits{Jobs: 8, Bytes: 256 << 20, JournalBytes: 8 << 20, Workers: 1}
	q, err := transferclient.OpenHerdrQueue(env, "native-profile-v1", limits)
	if err != nil {
		t.Fatal(err)
	}
	_ = q.Close()
	path := filepath.Join(state, "deliveries-v1", "locator")
	raw, err := os.ReadFile(path) //nolint:gosec // Synthetic private CLI fixture.
	if err != nil {
		t.Fatal(err)
	}
	old := bytes.Replace(raw, []byte(`"Version":3`), []byte(`"Version":2`), 1)
	if bytes.Equal(old, raw) {
		t.Fatal("unexpected layout")
	}
	// Mutate only the version byte in this test's existing locator. The Windows
	// fixture creator deliberately refuses replacement of an existing file.
	f, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // Existing private fixture file; no ACL or owner change.
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte{'2'}, int64(len(`{"Version":`)))
	if err == nil {
		err = f.Sync()
	}
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := runCommand([]string{"-action", "probe"}, strings.NewReader(""), &out, &stderr, env, nil, time.Now); code != 1 {
		t.Fatal("implicit migration", code)
	}
	for range 2 {
		out.Reset()
		stderr.Reset()
		code := runCommand([]string{"-action", "migrate-history"}, strings.NewReader(""), &out, &stderr, env, nil, time.Now)
		if code != 0 || strings.TrimSpace(out.String()) != `{"queue_layout":3,"history":"enabled"}` {
			t.Fatal("migration dispatcher", code, out.String(), stderr.String())
		}
	}
	backup, err := os.ReadFile(filepath.Join(state, "deliveries-v1", "locator-v2.backup")) //nolint:gosec // Synthetic private CLI fixture.
	if err != nil || !bytes.Equal(backup, old) {
		t.Fatal("old locator backup", err)
	}
	out.Reset()
	stderr.Reset()
	if code := runCommand([]string{"-action", "probe"}, strings.NewReader(""), &out, &stderr, env, nil, time.Now); code != 0 {
		t.Fatal("migrated probe", code, stderr.String())
	}
}
