package agenvo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/transferclient"
)

// This starts the actual batchScript/producerEnvelope, synthetic Pi, and the
// built production collector. No test writes a closed manifest or enqueues the
// new file. The cygpath/launcher shim ONLY maps the declared Windows executable
// and directory pair into isolated Linux paths; Windows/NTFS is not simulated.
func TestNativeGeneratedCompletionInvokesProductionCollector(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux generator-to-production-CLI combination; Windows acceptance is separate")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	bin, source := filepath.Join(root, "bin"), filepath.Join(root, "native-source")
	nativeConfig, nativeState := filepath.Join(root, "native-config"), filepath.Join(root, "native-state")
	legacy := filepath.Join(root, "legacy-agenvo")
	legacyConfig, legacyState := filepath.Join(legacy, "config"), filepath.Join(legacy, "state")
	for _, path := range []string{bin, source, nativeConfig, nativeState, legacyConfig, legacyState} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, raw []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, raw, mode); err != nil {
			t.Fatal(err)
		}
	}
	var creates, other atomic.Int64
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Memoh-Transfer synthetic-generated-producer" {
			t.Error("collector used another profile's authorization")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/limits") {
			_ = json.NewEncoder(w).Encode(transferclient.Limits{MaxUploadBytes: 1 << 20, ChunkMaxBytes: 4096, ResumableEnabled: true})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/uploads") {
			creates.Add(1)
			// The caller cannot know whether creation happened. Retain its
			// create_unknown fence across further generated collect invocations.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		other.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer https.Close()
	cert := filepath.Join(root, "synthetic-ca.pem")
	write(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: https.Certificate().Raw}), 0o600)
	for path, credential := range map[string]string{nativeConfig: "synthetic-generated-producer", legacyConfig: "synthetic-retired-producer"} {
		settings, _ := json.Marshal(map[string]any{"version": 1, "origin": https.URL, "source_root": source})
		write(filepath.Join(path, "transfer.json"), settings, 0o600)
		write(filepath.Join(path, "device.credential"), []byte(credential), 0o600)
	}
	binary := filepath.Join(bin, "transferqueue")
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, "../../cmd/transferqueue") //nolint:gosec // Fixed local production source and test-owned output.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build producer: %v %s", err, out)
	}
	probe := exec.CommandContext(ctx, binary, "-action", "probe") //nolint:gosec // Locally built binary and fixed action, no shell.
	probe.Env = []string{"HERDR_PLUGIN_CONFIG_DIR=" + legacyConfig, "HERDR_PLUGIN_STATE_DIR=" + legacyState}
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("seed old queue: %v %s", err, out)
	}
	write(filepath.Join(legacyState, "retired-record"), []byte("existing queue bytes must stay unchanged"), 0o600)
	legacyBytes := generatedProducerTreeDigest(t, legacy)
	p := batchFixtureProgress()
	p.LayoutVersion, p.CompletionVersion = BatchLayoutVersion, BatchCompletionVersion
	target := Target{NativeRuntimeID: uuid.NewString(), WorkDir: source, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic-model"}, Producer: ProducerLocator{ProgramPath: `C:\Native Owner's Bin\producer.exe`, ConfigDir: `C:\Native Config\producer`, StateDir: `C:\Native State\producer`}}
	p.Producer = &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "generated-completion-run", Attempt: p.Attempt, Generation: p.Generation, RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "fresh_result", Mime: "text/plain"}}
	// Internal generator contract: WorkDir and RootHash use the actual Linux
	// source directory. The native owner API still requires Windows locators.
	for name, script := range map[string]string{
		"uname": "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
		"cygpath": `#!/bin/sh
if [ "$2" = "$SYNTH_PROGRAM_WINDOWS" ]; then printf '%s' "$SYNTH_LAUNCHER"; else printf '%s' "$2"; fi
`,
		"pi": `#!/bin/sh
test -f "$MEMOH_DELIVERY_CONTEXT_FILE" || exit 1
cat > "$SYNTH_PROMPT"
printf 'generated fresh result bytes' > fresh_result
printf 'run\n' >> "$SYNTH_PI_COUNT"
cat "$SYNTH_EVENTS"
`,
		"herdr": `#!/bin/sh
printf 'called\n' >> "$SYNTH_LEGACY_CALLS"
exit 99
`,
		"producer.exe": `#!/bin/sh
test "$HERDR_PLUGIN_CONFIG_DIR" = "$SYNTH_CONFIG_WINDOWS" || exit 91
test "$HERDR_PLUGIN_STATE_DIR" = "$SYNTH_STATE_WINDOWS" || exit 92
test "$#" = 2 && test "$1" = -action && test "$2" = collect-result || exit 93
printf 'collect\n' >> "$SYNTH_PRODUCER_COUNT"
export HERDR_PLUGIN_CONFIG_DIR="$SYNTH_CONFIG_LINUX"
export HERDR_PLUGIN_STATE_DIR="$SYNTH_STATE_LINUX"
exec "$SYNTH_PRODUCER_BINARY" "$@"
`,
	} {
		write(filepath.Join(bin, name), []byte(script), 0o700)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required for generated completion", err)
	}
	if err := os.Symlink(node, filepath.Join(bin, "node")); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(root, "events"), []byte(piEvents(p.PiSessionID, "stop", "synthetic task completed", false)), 0o600)
	env := []string{
		"PATH=" + bin + ":/usr/bin:/bin", "SSL_CERT_FILE=" + cert,
		"HERDR_PLUGIN_CONFIG_DIR=" + legacyConfig, "HERDR_PLUGIN_STATE_DIR=" + legacyState,
		"SYNTH_PROGRAM_WINDOWS=" + target.Producer.ProgramPath, "SYNTH_CONFIG_WINDOWS=" + target.Producer.ConfigDir, "SYNTH_STATE_WINDOWS=" + target.Producer.StateDir,
		"SYNTH_CONFIG_LINUX=" + nativeConfig, "SYNTH_STATE_LINUX=" + nativeState,
		"SYNTH_PRODUCER_BINARY=" + binary, "SYNTH_LAUNCHER=" + filepath.Join(bin, "producer.exe"),
		"SYNTH_PROMPT=" + filepath.Join(root, "prompt"), "SYNTH_EVENTS=" + filepath.Join(root, "events"),
		"SYNTH_PI_COUNT=" + filepath.Join(root, "pi-count"), "SYNTH_PRODUCER_COUNT=" + filepath.Join(root, "producer-count"), "SYNTH_LEGACY_CALLS=" + filepath.Join(root, "legacy-calls"),
	}
	run := func(script string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "/bin/bash", "--noprofile", "--norc", "-p", "-c", script) //nolint:gosec // Actual generated envelope, synthetic paths/inputs only.
		cmd.Env, cmd.Dir = env, source
		return cmd.CombinedOutput()
	}
	script, err := batchScript(p, target, source, "create fresh_result exactly once")
	if err != nil {
		t.Fatal(err)
	}
	out, err := run(script)
	if err != nil {
		t.Fatalf("generated completion: %v %s", err, out)
	}
	receipt, err := parseBatchReceipt(string(out), p)
	if err != nil || receipt == nil || !receipt.Started || receipt.Completion == nil {
		t.Fatal("generated receipt missing", err)
	}
	attempt, _ := deliverymeta.AttemptDirectory(p.Attempt)
	attemptPath := filepath.Join(source, filepath.FromSlash(attempt))
	completion := readBatchFixture(t, filepath.Join(attemptPath, "result.json"))
	parsed, err := ParseBatchCompletion(completion, p, *receipt)
	if err != nil || parsed.Outcome != "succeeded" {
		t.Fatal("generated compact completion invalid", err)
	}
	if _, err := os.Stat(filepath.Join(source, p.Producer.ManifestName())); !os.IsNotExist(err) {
		t.Fatal("real collector did not consume generated manifest", err)
	}
	getenv := func(key string) string {
		switch key {
		case "HERDR_PLUGIN_CONFIG_DIR":
			return nativeConfig
		case "HERDR_PLUGIN_STATE_DIR":
			return nativeState
		}
		return ""
	}
	inspect := func() transferclient.QueueStatus {
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
		r, err := q.Inspect(p.Producer.DeliveryID())
		if err != nil {
			t.Fatal("new generated result did not enter native queue", err)
		}
		return r
	}
	registered := inspect()
	sum := sha256.Sum256([]byte("generated fresh result bytes"))
	if !registered.Registered || !registered.Admitted || registered.Associated || registered.Job.Phase != "create_unknown" || registered.Job.OperationID != p.Producer.Task.OperationID || registered.Job.Task.RunID != p.Producer.Task.RunID || registered.Job.Spec.SHA256 != hex.EncodeToString(sum[:]) || creates.Load() != 1 || other.Load() != 0 {
		t.Fatalf("generated registration or unknown boundary lost: phase=%s registered=%v admitted=%v associated=%v creates=%d", registered.Job.Phase, registered.Registered, registered.Admitted, registered.Associated, creates.Load())
	}
	wake, err := nativeProducerCollectCommand(target.Producer)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if out, err := run(wake); err != nil {
			t.Fatalf("generated collection wake: %v %s", err, out)
		}
		current := inspect()
		if current.Job.DeliveryID != registered.Job.DeliveryID || current.Job.OperationID != registered.Job.OperationID || current.Job.Task != registered.Job.Task || current.Associated || current.Job.Phase != "create_unknown" || creates.Load() != 1 {
			t.Fatal("unknown collection replayed or replaced business/delivery")
		}
	}
	if _, err := run(script); err == nil {
		t.Fatal("duplicate generated completion reran Pi")
	}
	if string(readBatchFixture(t, filepath.Join(root, "pi-count"))) != "run\n" || string(readBatchFixture(t, filepath.Join(root, "producer-count"))) != "collect\ncollect\ncollect\n" {
		t.Fatal("wrong Pi or finite producer invocation count")
	}
	if _, err := os.Stat(filepath.Join(root, "legacy-calls")); !os.IsNotExist(err) {
		t.Fatal("generated native completion invoked disabled legacy hook")
	}
	if !reflect.DeepEqual(legacyBytes, generatedProducerTreeDigest(t, legacy)) {
		t.Fatal("generated completion changed legacy queue/config bytes")
	}
	entries, err := os.ReadDir(filepath.Join(nativeState, "deliveries-v1"))
	if err != nil {
		t.Fatal(err)
	}
	journals := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".journal") {
			journals++
		}
	}
	if journals != 1 {
		t.Fatal("generated collection created duplicate queue jobs", journals)
	}
	trigger := readBatchFixture(t, filepath.Join(attemptPath, "producer-trigger.json"))
	var result map[string]json.RawMessage
	if json.Unmarshal(trigger, &result) != nil || string(result["associated"]) != "false" {
		t.Fatal("producer trigger acceptance became a delivery success")
	}
	for _, private := range []string{"synthetic-generated-producer", "synthetic-retired-producer", "generated fresh result bytes", https.URL} {
		if bytes.Contains(out, []byte(private)) || bytes.Contains(trigger, []byte(private)) {
			t.Fatal("receipt/trigger exposed private bytes")
		}
	}
}

func generatedProducerTreeDigest(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := make(map[string][32]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path) //nolint:gosec // Enumerated files under the synthetic test directory only.
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[name] = sha256.Sum256(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
