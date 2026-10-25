package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/transferclient"
)

// Metadata only: the separate native PG fixture performs the actual 1 GiB
// transfer. This exercises the real CLI dispatcher with NO budget override.
func TestCLIDefaultDownloadDoesNotCapSourceSize(t *testing.T) {
	root := cliPrivateRoot(t)
	config, state := filepath.Join(root, "config"), filepath.Join(root, "state")
	for _, p := range []string{config, state} {
		if err := cliFixtureDir(p); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string][]byte{"transfer.json": []byte(`{"version":1,"origin":"https://localhost","source_root":""}`), "device.credential": []byte("synthetic-default-cli-device")} {
		if err := cliFixtureWrite(filepath.Join(config, name), data); err != nil {
			t.Fatal(err)
		}
	}
	env := func(key string) string {
		switch key {
		case "HERDR_PLUGIN_CONFIG_DIR":
			return config
		case "HERDR_PLUGIN_STATE_DIR":
			return state
		}
		return ""
	}
	if _, err := os.Stat(filepath.Join(config, "queue-budget.json")); !os.IsNotExist(err) {
		t.Fatal("unexpected budget override", err)
	}
	job := transferclient.Job{Version: 1, DeliveryID: "default-large-download", SnapshotID: "default-large-download", BotID: "bot-1", OperationID: "op-1", LocatorRevision: "native-profile-v1", Phase: "queued", Task: transferclient.DeviceTask{UserID: "user-1", SessionID: "session-1", TargetID: "target-1", RunID: "run-1", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}, Spec: transferclient.Spec{SizeBytes: 1 << 30, SHA256: strings.Repeat("1", 64), Mime: "application/octet-stream"}}
	input, _ := json.Marshal(admissionInput{Job: job})
	var output, stderr bytes.Buffer
	code := runCommand([]string{"-action", "enqueue-download"}, bytes.NewReader(input), &output, &stderr, env, nil, time.Now)
	if code != 0 {
		t.Fatalf("default CLI rejects legal large download: exit=%d output=%s stderr=%s", code, output.String(), stderr.String())
	}
	q, err := transferclient.OpenHerdrQueue(env, "native-profile-v1", transferclient.DefaultHostQueueLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	got, err := q.Inspect(job.DeliveryID)
	if err != nil || got.Job.Offset != 0 || got.Job.Spec != job.Spec || got.Job.Task != job.Task {
		t.Fatal("lost metadata/identity", got, err)
	}
	var payloadBytes int64
	if err := filepath.WalkDir(state, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".part") {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			payloadBytes += info.Size()
		}
		return err
	}); err != nil || payloadBytes != 0 {
		t.Fatal("enqueue copied or preallocated source", payloadBytes, err)
	}
}
