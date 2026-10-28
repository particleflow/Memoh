package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/transferclient"
	"github.com/google/uuid"
)

func TestResultRecoveryCLIUsesExistingFiniteEntry(t *testing.T) {
	root := cliPrivateRoot(t)
	config, state, source := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source")
	for _, path := range []string{config, state, source} {
		if err := cliFixtureDir(path); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, raw []byte) {
		t.Helper()
		if err := cliFixtureWrite(path, raw); err != nil {
			t.Fatal(err)
		}
	}
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": "https://fixture.invalid", "source_root": source})
	write(filepath.Join(config, "transfer.json"), settings)
	write(filepath.Join(config, "device.credential"), []byte("synthetic-recovery-device"))
	getenv := func(key string) string {
		if key == "HERDR_PLUGIN_CONFIG_DIR" {
			return config
		}
		if key == "HERDR_PLUGIN_STATE_DIR" {
			return state
		}
		return ""
	}
	sum := func(raw []byte) string { hash := sha256.Sum256(raw); return hex.EncodeToString(hash[:]) }
	plan := deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "original-synthetic-run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "legacy_result", Mime: "application/octet-stream"}}
	attempt, _ := deliverymeta.AttemptDirectory(plan.Task.Attempt)
	current := source
	for _, part := range strings.Split(attempt, "/") {
		current = filepath.Join(current, part)
		if err := cliFixtureDir(current); err != nil {
			t.Fatal(err)
		}
	}
	contextRaw, _ := json.Marshal(plan)
	write(filepath.Join(current, deliverymeta.ProducerContextName), contextRaw)
	data := []byte("synthetic actual original bytes retained at explicitly named file")
	write(filepath.Join(source, "explicit.bin"), data)
	identity := map[string]any{"attempt": plan.Task.Attempt, "generation": plan.Task.Generation, "request_hash": strings.Repeat("c", 64), "pi_session_id": strings.Repeat("d", 64), "workspace_id": "original-workspace", "pane_id": "original-pane", "terminal_id": "original-terminal"}
	completion := map[string]any{}
	for k, v := range identity {
		completion[k] = v
	}
	completion["version"], completion["result"] = 1, map[string]any{"outcome": "succeeded"}
	completionRaw, _ := json.Marshal(completion)
	write(filepath.Join(current, "result.json"), completionRaw)
	identity["version"], identity["started"], identity["exited"], identity["exit_code"] = 1, true, true, 0
	identity["completion"] = map[string]any{"name": "result.json", "sha256": sum(completionRaw), "size": len(completionRaw)}
	receipt, _ := json.Marshal(identity)
	write(filepath.Join(current, "receipt.json"), receipt)
	request := deliverymeta.ResultRecovery{Version: 1, Plan: plan, SourceRef: plan.Artifact.SourceRef, PhysicalName: "explicit.bin", SHA256: sum(data), SizeBytes: int64(len(data)), HostBinding: sum(settings), TargetFingerprint: strings.Repeat("e", 64), OperationFingerprint: strings.Repeat("f", 64), RequestHash: strings.Repeat("c", 64), ContextHash: sum(contextRaw), Receipt: receipt, RemoteOutcome: "absent"}
	raw, _ := json.Marshal(request)
	var calls atomic.Int64
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, transferclient.ErrTransport
	}}
	for range 2 {
		var stdout, stderr bytes.Buffer
		code := runCommand([]string{"-action", "recover-result"}, bytes.NewReader(raw), &stdout, &stderr, getenv, &http.Client{Transport: transport}, time.Now)
		var status map[string]any
		if code != 0 || json.Unmarshal(stdout.Bytes(), &status) != nil || status["registered"] != true || status["delivery_id"] != plan.DeliveryID() || status["feedback_status"] != "recorded" || stderr.Len() != 0 || calls.Load() != 0 {
			t.Fatal("finite CLI mapping", code, stdout.String(), stderr.String())
		}
		if strings.Contains(stdout.String(), "explicit.bin") || strings.Contains(stdout.String(), "synthetic-recovery-device") || strings.Contains(stdout.String(), source) {
			t.Fatal("private metadata escaped CLI")
		}
	}
	t.Log("real command decoder/host config/private queue/feedback: same old deliveryID registered twice idempotently; HTTP=0; no private metadata in output")
}
