package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/transferclient"
)

// Admission exceeds the real 64 MiB finite copy budget. Test the real command,
// durable checkpoint and replay, without network transfer or real credentials.
func TestProducerAdmissionPendingCanResumeSameDelivery(t *testing.T) {
	for _, action := range []string{"enqueue-result", "collect-result"} {
		t.Run(action, func(t *testing.T) {
			root := cliPrivateRoot(t)
			config, state, source := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source")
			for _, p := range []string{config, state, source} {
				if err := cliFixtureDir(p); err != nil {
					t.Fatal(err)
				}
			}
			write := func(p string, b []byte) {
				t.Helper()
				if err := cliFixtureWrite(p, b); err != nil {
					t.Fatal(err)
				}
			}
			var requests atomic.Int64
			endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer endpoint.Close()
			settings, _ := json.Marshal(map[string]any{"version": 1, "origin": endpoint.URL, "source_root": source})
			write(filepath.Join(config, "transfer.json"), settings)
			write(filepath.Join(config, "device.credential"), []byte("synthetic-admission-device"))
			path := filepath.Join(source, "large-result")
			write(path, nil)                                // Native Windows fixture assigns only this new file's owner/DACL.
			f, err := os.OpenFile(path, os.O_WRONLY, 0o600) //nolint:gosec // Fixed synthetic source, created privately above.
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			block := make([]byte, 64<<10)
			hash := sha256.New()
			const size = 65 << 20
			for n := 0; n < size; n += len(block) {
				if _, err := f.Write(block); err != nil {
					t.Fatal(err)
				}
				_, _ = hash.Write(block)
			}
			if err := f.Sync(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			closed := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "admission-fixture", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "large-result", Mime: "application/octet-stream"}}, Closed: true, SHA256: hex.EncodeToString(hash.Sum(nil)), SizeBytes: size}
			attempt, ok := deliverymeta.AttemptDirectory(closed.Task.Attempt)
			if !ok {
				t.Fatal("attempt")
			}
			current := source
			for _, segment := range strings.Split(attempt, "/") {
				current = filepath.Join(current, segment)
				if err := cliFixtureDir(current); err != nil {
					t.Fatal(err)
				}
			}
			plan, _ := json.Marshal(closed.Plan)
			write(filepath.Join(current, deliverymeta.ProducerContextName), plan)
			raw, _ := json.Marshal(closed)
			manifest := filepath.Join(source, closed.ManifestName())
			if action == "collect-result" {
				write(manifest, raw)
			}
			getenv := func(key string) string {
				switch key {
				case "HERDR_PLUGIN_CONFIG_DIR":
					return config
				case "HERDR_PLUGIN_STATE_DIR":
					return state
				}
				return ""
			}
			invoke := func(action string, input []byte) (map[string]json.RawMessage, int) {
				t.Helper()
				var output, stderr bytes.Buffer
				code := runCommand([]string{"-action", action}, bytes.NewReader(input), &output, &stderr, getenv, endpoint.Client(), time.Now)
				for _, secret := range []string{source, endpoint.URL, "synthetic-admission-device"} {
					if bytes.Contains(output.Bytes(), []byte(secret)) || bytes.Contains(stderr.Bytes(), []byte(secret)) {
						t.Fatal("private configuration leaked")
					}
				}
				var status map[string]json.RawMessage
				if json.Unmarshal(output.Bytes(), &status) != nil {
					t.Fatal("invalid JSON")
				}
				return status, code
			}
			status, code := invoke(action, raw)
			limits := transferclient.QueueLimits{Jobs: 8, Bytes: 256 << 20, JournalBytes: 8 << 20, Workers: 1}
			inspect := func() (transferclient.QueueStatus, error) {
				t.Helper()
				q, err := transferclient.OpenHerdrQueue(getenv, "native-profile-v1", limits)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = q.Close() }()
				return q.Inspect(closed.DeliveryID())
			}
			pending, err := inspect()
			if !errors.Is(err, transferclient.ErrAdmissionPending) || pending.Admitted || pending.Job.DeliveryID != closed.DeliveryID() {
				t.Fatal("missing durable admission", err)
			}
			if action == "collect-result" {
				if _, err := os.Stat(manifest); err != nil {
					t.Fatal("pending manifest consumed", err)
				}
			}
			if code != 1 || string(status["state"]) != `"pending"` || string(status["error_class"]) != `"transient"` || string(status["feedback_status"]) != `"recorded"` {
				t.Fatalf("resumable admission reported as unknown: exit=%d state=%s class=%s", code, status["state"], status["error_class"])
			}
			for range 2 {
				status, code = invoke("enqueue-result", raw)
				if code != 0 || string(status["admitted"]) != "true" || string(status["delivery_id"]) != `"`+closed.DeliveryID()+`"` {
					t.Fatal("same-ID admission did not resume/replay", code)
				}
			}
			sealed, err := inspect()
			if err != nil || !sealed.Admitted || sealed.Job.OperationID != pending.Job.OperationID || sealed.Job.Task != pending.Job.Task || sealed.Job.Spec != pending.Job.Spec || sealed.Job.ReceiptID != "" || sealed.Seal == nil || sealed.Associated {
				t.Fatal("resume changed original admission identity", err)
			}
			conflict := closed
			conflict.SHA256 = strings.Repeat("c", 64)
			bad, _ := json.Marshal(conflict)
			status, code = invoke("enqueue-result", bad)
			if code != 1 || string(status["state"]) == `"pending"` {
				t.Fatal("conflicting metadata mislabeled resumable")
			}
			if requests.Load() != 0 {
				t.Fatal("admission performed a network operation")
			}
		})
	}
}
