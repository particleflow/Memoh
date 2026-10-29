package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/transferclient"
)

// Test-executable child only. Calls the same nil-transport run() used by the
// shipping main, without any test TLS root, fake reader or recovery shortcut.
func TestReceiptBindingCommandChild(_ *testing.T) {
	if os.Getenv("MEMOH_RECEIPT_BINDING_CHILD") != "1" {
		return
	}
	os.Args = []string{os.Args[0], "-action", "receipt-binding", "-delivery", os.Getenv("MEMOH_RECEIPT_BINDING_DELIVERY")}
	os.Exit(run())
}

func TestCLIPathReceiptBindingPositiveChildReadOnly(t *testing.T) {
	root := cliPrivateRoot(t)
	config, state, source := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source")
	for _, dir := range []string{config, state, source} {
		if err := cliFixtureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	data := []byte("original receipt CLI fixture")
	sum := sha256.Sum256(data)
	closed := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1,
		Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(),
			TargetID: uuid.NewString(), RunID: "receipt-cli-fixture", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(source)},
		Artifact: deliverymeta.Artifact{SourceRef: "result.bin", Mime: "application/octet-stream"}}, Closed: true, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data))}
	var requests, creates atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Memoh-Transfer synthetic-receipt-setup" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/limits"):
			_ = json.NewEncoder(w).Encode(transferclient.Limits{MaxUploadBytes: 1 << 20, ChunkMaxBytes: 4, ResumableEnabled: true})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/uploads"):
			creates.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(transferclient.Receipt{ID: "original-receipt", State: "uploading", Offset: 0,
				SizeBytes: closed.SizeBytes, ExpiresAt: time.Now().UTC().Add(time.Hour), Mime: closed.Artifact.Mime})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": server.URL, "source_root": source})
	for file, raw := range map[string][]byte{filepath.Join(config, "transfer.json"): settings,
		filepath.Join(config, "device.credential"): []byte("synthetic-receipt-setup"), filepath.Join(source, closed.Artifact.SourceRef): data} {
		if err := cliFixtureWrite(file, raw); err != nil {
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
	raw, _ := json.Marshal(closed)
	action, _, err := transferclient.ProducerAdmission(bytes.NewReader(raw), "native-profile-v1")
	if err != nil {
		t.Fatal(err)
	}
	limits := transferclient.DefaultHostQueueLimits()
	if _, err := transferclient.InvokeConfiguredHerdr(t.Context(), env, "native-profile-v1", limits, action, server.Client(), time.Now()); err != nil {
		t.Fatal(err)
	}
	action.Kind = "advance"
	prepared, err := transferclient.InvokeConfiguredHerdr(t.Context(), env, "native-profile-v1", limits, action, server.Client(), time.Now())
	if err != nil || prepared.Job.ReceiptID != "original-receipt" || prepared.Job.Phase != "uploading" || creates.Load() != 1 {
		t.Fatal("failed to prepare original durable receipt", prepared, err, creates.Load())
	}
	// Fixture preparation above reads bytes/credential and makes TLS requests.
	// Only the fresh receipt-reader child below is the no-network/read-only scope.
	if err := os.Remove(filepath.Join(config, "device.credential")); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		err := filepath.WalkDir(root, func(file string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			raw, err := os.ReadFile(file) //nolint:gosec // Enumerated isolated fixture, never a product reader.
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			out[file] = hex.EncodeToString(sum[:]) + ":" + info.ModTime().UTC().Format(time.RFC3339Nano)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before, setupRequests := snapshot(), requests.Load()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReceiptBindingCommandChild$") //nolint:gosec // This exact test executable, fixed helper only.
	cmd.Dir = root                                                                            // The reader must use the explicit config/state pair, never cwd.
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key = strings.ToUpper(key); key != "HERDR_PLUGIN_CONFIG_DIR" && key != "HERDR_PLUGIN_STATE_DIR" && !strings.HasPrefix(key, "MEMOH_RECEIPT_BINDING_") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "HERDR_PLUGIN_CONFIG_DIR="+config, "HERDR_PLUGIN_STATE_DIR="+state,
		"MEMOH_RECEIPT_BINDING_CHILD=1", "MEMOH_RECEIPT_BINDING_DELIVERY="+closed.DeliveryID())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stderr.Len() != 0 {
		t.Fatal("positive receipt command failed", err, stderr.String())
	}
	var out transferclient.ReceiptBinding
	decoder := json.NewDecoder(&stdout)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("expected only the bounded receipt JSON")
	}
	binding := sha256.Sum256(settings)
	if out.Schema != "memoh.delivery.receipt-binding.v1" || out.State != "local_receipt_binding" || out.Scope != "local_queue_read_only" || out.ServerEffects != "none" ||
		out.DeliveryID != closed.DeliveryID() || out.Task != closed.Task || out.HostBinding != hex.EncodeToString(binding[:]) ||
		out.SourceRef != closed.Artifact.SourceRef || out.ContentHash != closed.SHA256 || out.SizeBytes != closed.SizeBytes || out.Mime != closed.Artifact.Mime ||
		out.KnownReceipt != "original-receipt" || out.Phase != "uploading" || out.Associated || out.AssociationState != "idle" {
		t.Fatal("command lost original identity or falsely claimed delivery", out)
	}
	if requests.Load() != setupRequests || creates.Load() != 1 || !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("receipt-reader child made a request or changed queue/config/source")
	}
}
