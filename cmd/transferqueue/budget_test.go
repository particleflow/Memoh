package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/transferclient"
)

func TestCLIBudgetConfigurationAndOldQueueRecovery(t *testing.T) {
	config, state := cliPrivateRoot(t), cliPrivateRoot(t)
	settings := []byte(`{"version":1,"origin":"https://fixture.invalid"}`)
	if err := cliFixtureWrite(filepath.Join(config, "transfer.json"), settings); err != nil {
		t.Fatal(err)
	}
	if err := cliFixtureWrite(filepath.Join(config, "device.credential"), []byte("synthetic-budget")); err != nil {
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
	invoke := func(action string, raw []byte) (int, map[string]json.RawMessage) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := runCommand([]string{"-action", action}, bytes.NewReader(raw), &out, &stderr, env, nil, time.Now)
		for _, private := range []string{config, state, "synthetic-budget", "fixture.invalid"} {
			if bytes.Contains(out.Bytes(), []byte(private)) || bytes.Contains(stderr.Bytes(), []byte(private)) {
				t.Fatal("private details leaked")
			}
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(out.Bytes(), &result) != nil {
			t.Fatal("no JSON status", code, stderr.String())
		}
		return code, result
	}
	job := transferclient.Job{Version: 1, DeliveryID: "budget-old", OperationID: "operation", BotID: "bot", SnapshotID: "budget-old", LocatorRevision: "native-profile-v1", Phase: "queued", Spec: transferclient.Spec{SizeBytes: 7, SHA256: strings.Repeat("a", 64), Mime: "text/plain"}, Task: transferclient.DeviceTask{UserID: "user", SessionID: "session", TargetID: "target", RunID: "run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}}
	raw, _ := json.Marshal(admissionInput{Job: job})
	if code, _ := invoke("enqueue-download", raw); code != 0 {
		t.Fatal("legacy admission", code)
	}
	limits := transferclient.DefaultHostQueueLimits()
	inspect := func() transferclient.QueueStatus {
		t.Helper()
		q, err := transferclient.OpenHerdrQueue(env, "native-profile-v1", limits)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = q.Close() }()
		status, err := q.Inspect("budget-old")
		if err != nil {
			t.Fatal(err)
		}
		return status
	}
	before := inspect()
	if code, _ := invoke("enqueue-download", raw); code != 0 {
		t.Fatal("legacy restart", code)
	}
	if err := cliFixtureWrite(filepath.Join(config, "queue-budget.json"), []byte(`{"version":1,"bytes":8589934592}`)); err != nil {
		t.Fatal(err)
	}
	// A policy edit alone cannot replace or reinterpret the existing queue.
	if code, status := invoke("lifecycle-summary", nil); code != 1 || string(status["error_class"]) != `"configuration"` {
		t.Fatal("silent policy migration", code, status)
	}
	if code, _ := invoke("migrate-budget", nil); code != 0 {
		t.Fatal("explicit migration", code)
	}
	limits.Bytes = 8 << 30
	if got := inspect(); !reflect.DeepEqual(got, before) {
		t.Fatal("task/binding changed")
	}
	if code, _ := invoke("enqueue-download", raw); code != 0 {
		t.Fatal("original ID replay", code)
	}
	// This deliberately exceeds the configured aggregate quota, before any I/O.
	job.DeliveryID, job.SnapshotID, job.Spec.SizeBytes = "too-large", "too-large", (8<<30)+1
	raw, _ = json.Marshal(admissionInput{Job: job})
	if code, status := invoke("enqueue-download", raw); code != 1 || string(status["state"]) != `"capacity"` {
		t.Fatal("capacity classification", code, status)
	}
	f, err := os.OpenFile(filepath.Join(config, "queue-budget.json"), os.O_WRONLY|os.O_TRUNC, 0) //nolint:gosec // Existing private fixture only; preserve its owner/DACL.
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"version":1,"bytes":1073741824}`)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if code, status := invoke("migrate-budget", nil); code != 1 || string(status["error_class"]) != `"configuration"` {
		t.Fatal("implicit shrink", code, status)
	}
	got, err := os.ReadFile(filepath.Join(config, "transfer.json")) //nolint:gosec // Fixed private fixture configuration.
	if err != nil || !bytes.Equal(got, settings) {
		t.Fatal("identity configuration changed", err)
	}
	if got := inspect(); !reflect.DeepEqual(got, before) {
		t.Fatal("rejected edit changed task")
	}
}
