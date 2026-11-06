package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/transferclient"
)

func TestCLISnapshotScopeNeverActivatesRegisteredInput(t *testing.T) {
	root := cliPrivateRoot(t)
	config, state, source := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source")
	for _, path := range []string{config, state, source} {
		if err := cliFixtureDir(path); err != nil {
			t.Fatal(err)
		}
	}
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": "https://fixture.invalid", "source_root": source})
	for path, data := range map[string][]byte{filepath.Join(config, "transfer.json"): settings, filepath.Join(config, "device.credential"): []byte("synthetic-snapshot-device"), filepath.Join(source, "result-one"): []byte("snapshot-scope-fixture")} {
		if err := cliFixtureWrite(path, data); err != nil {
			t.Fatal(err)
		}
	}
	getenv := func(key string) string {
		if key == "HERDR_PLUGIN_CONFIG_DIR" {
			return config
		}
		if key == "HERDR_PLUGIN_STATE_DIR" {
			return state
		}
		return ""
	}
	cfg, err := transferclient.OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("snapshot-scope-fixture")
	sum := sha256.Sum256(data)
	job := transferclient.Job{Version: 1, DeliveryID: "registered-original", SnapshotID: "registered-original", BotID: "bot-1", OperationID: "operation-1", LocatorRevision: "native-profile-v1", HostBinding: cfg.Binding, Phase: "queued", Task: transferclient.DeviceTask{UserID: "user", SessionID: "session", TargetID: "target", RunID: "run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}, Spec: transferclient.Spec{SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), Mime: "application/octet-stream"}}
	q, err := transferclient.OpenHerdrQueue(getenv, "native-profile-v1", transferclient.DefaultHostQueueLimits())
	if err != nil {
		t.Fatal(err)
	}
	before, err := q.RegisterUpload(t.Context(), job, "result-one", cfg.OpenSource)
	if err != nil {
		t.Fatal(err)
	}
	_ = q.Close()
	_ = cfg.Close()
	if err := os.RemoveAll(source); err != nil { // Test-owned source only; private queue/config retained.
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-action", "advance-snapshots"}, {"-action", "advance-snapshot", "-delivery", job.DeliveryID}} {
		var stdout, stderr bytes.Buffer
		code := runCommand(args, bytes.NewReader(nil), &stdout, &stderr, getenv, nil, time.Now)
		var out map[string]any
		if code != 0 || json.Unmarshal(stdout.Bytes(), &out) != nil || out["state"] != "idle" || out["advance_scope"] != "retained_upload_snapshots" || out["feedback_status"] != nil || stderr.Len() != 0 {
			t.Fatal("narrow CLI scope/result changed", code, stdout.String(), stderr.String())
		}
	}
	q, err = transferclient.OpenHerdrQueue(getenv, "native-profile-v1", transferclient.DefaultHostQueueLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	after, err := q.Inspect(job.DeliveryID)
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if err != nil || !bytes.Equal(a, b) || after.Activated || after.Admitted {
		t.Fatal("CLI activated/mutated source-backed registration", err)
	}
}
