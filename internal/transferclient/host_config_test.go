package transferclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfiguredHostSecretsPathsAndRecovery(t *testing.T) {
	config, state, source := configFixtureRoot(t), configFixtureRoot(t), configFixtureRoot(t)
	write := func(root, name string, data []byte) {
		t.Helper()
		dir, err := os.OpenRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = dir.Close() }()
		file, err := openPrivateFile(dir, name, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal("private fixture file", err)
		}
		defer func() { _ = file.Close() }()
		if err := file.Truncate(0); err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := file.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	settings, _ := json.Marshal(hostConfigWire{Version: 1, Origin: "https://fixture.invalid", SourceRoot: source})
	write(config, "transfer.json", settings)
	write(config, "device.credential", []byte("synthetic-device-secret\n"))
	write(source, "result-one", []byte("fixture"))
	getenv := func(key string) string {
		switch key {
		case "HERDR_PLUGIN_CONFIG_DIR":
			return config
		case "HERDR_PLUGIN_STATE_DIR":
			return state
		}
		return ""
	}
	h, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := h.Authorization(t.Context())
	if err != nil || auth != "Memoh-Transfer synthetic-device-secret" {
		t.Fatal("credential read failed")
	}
	if _, err := h.OpenSource("../device.credential"); !errors.Is(err, ErrBinding) {
		t.Fatal("source traversal")
	}
	_ = h.Close()
	job := Job{Version: 1, DeliveryID: "configured-one", SnapshotID: "configured-one", BotID: "bot", OperationID: "operation", LocatorRevision: "fixture-v1", Phase: "queued", Spec: Spec{SizeBytes: 7, SHA256: "f16d05ec6b29248d2c61adb1e9263f78e4f7bace1b955014a2d17872cfe4064d", Mime: "text/plain"}, Task: DeviceTask{UserID: "user", SessionID: "session", TargetID: "target", RunID: "run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}}
	// Download admission does not fetch credentials or bytes. Hash is metadata.
	action := ConfiguredAction{Kind: "enqueue-download", DeliveryID: job.DeliveryID, Job: job}
	limits := QueueLimits{Jobs: 4, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	first, err := InvokeConfiguredHerdr(t.Context(), getenv, "fixture-v1", limits, action, nil, time.Now())
	if err != nil || !first.Admitted {
		t.Fatal("admission", err)
	}
	second, err := InvokeConfiguredHerdr(t.Context(), getenv, "fixture-v1", limits, action, nil, time.Now())
	if err != nil || second.Job.HostBinding != first.Job.HostBinding || second.Job.Task != job.Task {
		t.Fatal("same ID recovery", err)
	}
	upload := job
	upload.DeliveryID, upload.SnapshotID = "configured-upload", "configured-upload"
	sum := sha256.Sum256([]byte("fixture"))
	upload.Spec.SHA256 = hex.EncodeToString(sum[:])
	uploadAction := ConfiguredAction{Kind: "enqueue-upload", DeliveryID: upload.DeliveryID, Job: upload, SourceRef: "result-one"}
	sealed, err := InvokeConfiguredHerdr(t.Context(), getenv, "fixture-v1", limits, uploadAction, nil, time.Now())
	if err != nil || !sealed.Admitted || sealed.Seal == nil {
		t.Fatal("configured source sealing", err)
	}
	write(source, "result-one", []byte("changed"))
	// A finished admission keeps the immutable sealed file when source changes.
	sealedAgain, err := InvokeConfiguredHerdr(t.Context(), getenv, "fixture-v1", limits, uploadAction, nil, time.Now())
	if err != nil || sealedAgain.Job.Spec != sealed.Job.Spec {
		t.Fatal("source mutation replaced snapshot", err)
	}
	if err := os.Remove(filepath.Join(source, "result-one")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	sealedAgain, err = InvokeConfiguredHerdr(t.Context(), getenv, "fixture-v1", limits, uploadAction, nil, time.Now())
	if err != nil || !sealedAgain.Admitted || sealedAgain.Job.Spec != sealed.Job.Spec {
		t.Fatal("sealed recovery depends on deleted producer directory", err)
	}
	uploadAction.Job.Task.Attempt = strings.Repeat("c", 32)
	if _, err := InvokeConfiguredHerdr(t.Context(), getenv, "fixture-v1", limits, uploadAction, nil, time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatal("same ID changed task", err)
	}
	settings, _ = json.Marshal(hostConfigWire{Version: 1, Origin: "https://changed.invalid", SourceRoot: source})
	write(config, "transfer.json", settings)
	action.Kind = "advance"
	_, err = InvokeConfiguredHerdr(t.Context(), getenv, "fixture-v1", limits, action, nil, time.Now())
	if !errors.Is(err, ErrVersion) {
		t.Fatal("changed origin did not fail before network", err)
	}
	settings, _ = json.Marshal(hostConfigWire{Version: 1, Origin: "http://fixture.invalid", SourceRoot: source})
	write(config, "transfer.json", settings)
	if _, err := OpenHostConfig(getenv); !errors.Is(err, ErrBinding) {
		t.Fatal("HTTP accepted", err)
	}
	settings, _ = json.Marshal(hostConfigWire{Version: 1, Origin: "https://fixture.invalid", SourceRoot: config})
	write(config, "transfer.json", settings)
	if _, err := OpenHostConfig(getenv); !errors.Is(err, ErrBinding) {
		t.Fatal("credential directory became producer source", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := h.Authorization(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled credential read", err)
	}
}
