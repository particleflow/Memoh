//go:build integration

package db_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

// Called inside the existing full-migration, NOBYPASSRLS PostgreSQL fixture.
// This builds and starts the production CLI, not a protocol stand-in or a test
// dispatcher. Only its process's trust store contains the isolated TLS root.
func exerciseReadyProofCLI(ctx context.Context, t *testing.T, store *postgresstore.Store, id computertask.DeliveryIdentity, ref computertask.DeliveryRequest) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("NAS upload fixture requires POSIX UploadStore; native Windows uses its separate NTFS tests")
	}
	binary := filepath.Join(t.TempDir(), "transferqueue")
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, "../../cmd/transferqueue") //nolint:gosec // Fixed repository command, isolated output; not a distribution build.
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual CLI: %v %s", err, output)
	}
	verifier := newRegisteredTransferVerifier(ctx, t, store, id)
	for _, mode := range []string{"lost-response", "client-killed-before-checkpoint", "no-proof-gap"} {
		t.Run(mode, func(t *testing.T) { exerciseReadyProofCLIRecovery(ctx, t, store, id, ref, verifier, binary, mode) })
	}
}

func exerciseReadyProofCLIRecovery(ctx context.Context, t *testing.T, store *postgresstore.Store, id computertask.DeliveryIdentity, ref computertask.DeliveryRequest, verifier *registeredTransferVerifier, binary, mode string) {
	t.Helper()
	root := t.TempDir()
	config, state, source, originals := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source"), filepath.Join(root, "originals")
	for _, dir := range []string{config, state, source, originals} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	closed := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID, TargetID: id.Binding.TargetID, RunID: id.Binding.RunID, Attempt: id.Attempt, Generation: id.Generation, RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "result-" + mode, Mime: ref.Mime}}, Closed: true, SHA256: ref.ContentHash, SizeBytes: ref.SizeBytes}
	operation := dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID}
	current, err := store.ReadComputerOperation(ctx, operation)
	if err != nil {
		t.Fatal(err)
	}
	var details computertask.Details
	if json.Unmarshal(current.Details, &details) != nil || details.Batch == nil {
		t.Fatal("missing batch")
	}
	details.Batch.Progress.Producer = &closed.Plan
	current.Details, _ = json.Marshal(details)
	if _, err := store.SaveComputerOperation(ctx, current); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	var assets *media.Service
	var active *handlers.ComputerTransferHandler
	var httpHandler http.Handler
	var dispatch sync.RWMutex
	start := func() {
		t.Helper()
		assets = media.NewService(nil, localfs.NewOriginals(originals))
		assets.SetRequirePersistence(true)
		if err := assets.EnableUploads(originals); err != nil { //nolint:contextcheck // Startup initializer has no context parameter; Server requests carry ctx.
			t.Fatal(err)
		}
		messages := handlers.NewMessageHandler(log, nil, nil, nil, nil)
		messages.SetMediaService(assets)
		var err error
		active, err = handlers.NewDeviceComputerTransferHandler(ctx, computertask.NewPostgresDeliveryService(store, assets), messages, verifier, log)
		if err != nil {
			t.Fatal(err)
		}
		dispatch.Lock()
		httpHandler = server.NewServer(log, ":0", "isolated-ready-proof", active) //nolint:contextcheck // Real HTTP requests supply their own contexts.
		dispatch.Unlock()
	}
	start()
	defer func() { active.Close(); _ = assets.Uploads().Close() }()
	var creates, completes, commits atomic.Int64
	entered, finished := make(chan int, 1), make(chan struct{})
	var inject atomic.Bool
	inject.Store(true)
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatch.RLock()
		endpoint := httpHandler
		dispatch.RUnlock()
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media/uploads") {
			creates.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/commit") {
			commits.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/complete") {
			completes.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/complete") && inject.Swap(false) {
			defer close(finished)
			result := httptest.NewRecorder()
			if mode == "no-proof-gap" {
				// Explicit interruption between filesystem publication and the
				// operation proof transaction. Never a task success response.
				parts := strings.Split(r.URL.Path, "/")
				_, err := assets.Uploads().Complete(r.Context(), id.Binding.UserID, id.Binding.BotID, parts[len(parts)-2])
				result.Code = http.StatusOK
				if err != nil {
					result.Code = http.StatusInternalServerError
				}
			} else {
				endpoint.ServeHTTP(result, r)
			}
			entered <- result.Code
			if mode == "client-killed-before-checkpoint" {
				<-r.Context().Done()
				return
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		endpoint.ServeHTTP(w, r)
	}))
	defer https.Close()
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": https.URL, "source_root": source})
	write(filepath.Join(config, "transfer.json"), settings)
	write(filepath.Join(config, "device.credential"), []byte(verifier.token))
	write(filepath.Join(source, closed.Artifact.SourceRef), []byte("synthetic delivery content"))
	attempt, ok := deliverymeta.AttemptDirectory(id.Attempt)
	if !ok {
		t.Fatal("invalid attempt")
	}
	attemptPath := filepath.Join(source, filepath.FromSlash(attempt))
	if err := os.MkdirAll(attemptPath, 0o700); err != nil {
		t.Fatal(err)
	}
	plan, _ := json.Marshal(closed.Plan)
	write(filepath.Join(attemptPath, deliverymeta.ProducerContextName), plan)
	cert := filepath.Join(root, "isolated-ca.pem")
	write(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: https.Certificate().Raw}))
	command := func(action string, input []byte) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, "-action", action) //nolint:gosec // Locally built production CLI, fixed action; no shell.
		cmd.Env = append(os.Environ(), "HERDR_PLUGIN_CONFIG_DIR="+config, "HERDR_PLUGIN_STATE_DIR="+state, "SSL_CERT_FILE="+cert)
		cmd.Stdin = bytes.NewReader(input)
		return cmd
	}
	invoke := func(action string, input []byte) map[string]json.RawMessage {
		t.Helper()
		cmd := command(action, input)
		var output, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("CLI %s: %v %s %s", action, err, output.String(), stderr.String())
		}
		if bytes.Contains(output.Bytes(), []byte(verifier.token)) || bytes.Contains(output.Bytes(), []byte(source)) || bytes.Contains(output.Bytes(), []byte(https.URL)) {
			t.Fatal("private fixture data leaked")
		}
		var status map[string]json.RawMessage
		if json.Unmarshal(output.Bytes(), &status) != nil {
			t.Fatal("invalid CLI status")
		}
		return status
	}
	raw, _ := json.Marshal(closed)
	if out := invoke("enqueue-result", raw); string(out["registered"]) != "true" {
		t.Fatal("admission failed", out)
	}
	if mode == "client-killed-before-checkpoint" {
		cmd := command("advance-results", nil)
		var output, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		select {
		case code := <-entered:
			if code != http.StatusOK {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("complete failed", code)
			}
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("CLI did not reach completion", output.String(), stderr.String())
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Wait(); err == nil {
			t.Fatal("fixture process did not stop")
		}
	} else {
		out := invoke("advance-results", nil)
		if string(out["state"]) != `"waiting"` {
			t.Fatal("lost response was not deferred", out)
		}
		if code := <-entered; code != http.StatusOK {
			t.Fatal("server completion failed", code)
		}
	}
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("interrupted request did not finish")
	}
	readEntry := func() (media.Upload, string, string, bool) {
		t.Helper()
		r, err := store.ReadComputerOperation(ctx, operation)
		if err != nil {
			t.Fatal(err)
		}
		var d struct {
			Ledger struct {
				Version int `json:"version"`
				Entries map[string]struct {
					Outcome  string `json:"outcome"`
					Transfer struct {
						ID    string        `json:"upload_id"`
						Ready *media.Upload `json:"ready"`
					} `json:"transfer"`
				} `json:"entries"`
			} `json:"delivery_ledger"`
		}
		if json.Unmarshal(r.Details, &d) != nil || d.Ledger.Version != 4 {
			t.Fatal("proof ledger version")
		}
		e := d.Ledger.Entries[closed.DeliveryID()]
		if e.Transfer.Ready == nil {
			return media.Upload{}, e.Transfer.ID, e.Outcome, false
		}
		return *e.Transfer.Ready, e.Transfer.ID, e.Outcome, true
	}
	proof, receiptID, outcome, found := readEntry()
	if receiptID == "" || outcome != "reserved" || found != (mode != "no-proof-gap") {
		t.Fatal("ready/committed boundary", outcome, found)
	}
	if found && (proof.ID != receiptID || proof.ContentHash != ref.ContentHash || proof.SizeBytes != ref.SizeBytes) {
		t.Fatal("wrong exact proof")
	}
	// Simulate only the staging clock moving beyond ready TTL; run actual Sweep.
	path := filepath.Join(originals, ".uploads", receiptID+".json")
	staging, err := os.ReadFile(path) //nolint:gosec // Synthetic staging receipt only.
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(staging, &fields) != nil || string(fields["state"]) != `"ready"` {
		t.Fatal("not ready")
	}
	// Simulate a historical generic receipt: new task receipts never expire before confirmation.
	fields["progress_lease"] = json.RawMessage(`0`)
	fields["expires_at"], _ = json.Marshal(time.Now().Add(-time.Hour))
	staging, _ = json.Marshal(fields)
	write(path, staging)
	if err := assets.Uploads().Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staging survived expiry", err)
	}
	active.Close()
	_ = assets.Uploads().Close()
	start() // New server/Media/UploadStore objects, same authorized PG row and origin.

	// A new process uses the actual persisted due time, never resets its budget.
	waitDue := func(status map[string]json.RawMessage) {
		t.Helper()
		if string(status["state"]) != `"waiting"` {
			return
		}
		var due time.Time
		if json.Unmarshal(status["next_due_at"], &due) != nil || time.Until(due) > 10*time.Second {
			t.Fatal("unbounded fixture retry")
		}
		timer := time.NewTimer(max(time.Until(due), 0) + 20*time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var final map[string]json.RawMessage
	for range 8 {
		waitDue(invoke("lifecycle-summary", nil))
		final = invoke("advance-results", nil)
		if string(final["associated"]) == "true" || string(final["state"]) == `"unknown"` {
			break
		}
	}
	if mode == "no-proof-gap" {
		if string(final["associated"]) != "false" || string(final["state"]) != `"unknown"` || string(final["error_code"]) != `"receipt_unavailable"` {
			t.Fatal("unknown publication fabricated success", final)
		}
		snapshot, err := os.ReadFile(filepath.Join(source, closed.Artifact.SourceRef)) //nolint:gosec // Isolated original source; the indexed path never duplicates its payload.
		if err != nil || string(snapshot) != "synthetic delivery content" {
			t.Fatal("unknown evidence released", err)
		}
		if info, err := os.Stat(filepath.Join(state, "deliveries-v1", closed.DeliveryID()+".tree")); err != nil || info.Size() == 0 {
			t.Fatal("unknown block index removed", err)
		}
		feedbackRaw, err := os.ReadFile(filepath.Join(attemptPath, deliverymeta.ProducerFeedbackName)) //nolint:gosec // Isolated producer receipt.
		if err != nil {
			t.Fatal(err)
		}
		feedback, valid := deliverymeta.DecodeFeedback(feedbackRaw, closed.Plan)
		if !valid || feedback.Files[0].State != "unknown" || feedback.Files[0].Code != "receipt_unavailable" || feedback.Files[0].Retryable {
			t.Fatal("unknown feedback was not preserved")
		}
		before := creates.Load()
		invoke("advance-results", nil)
		if creates.Load() != before || commits.Load() != 0 {
			t.Fatal("unknown replayed mutation")
		}
	} else {
		if string(final["associated"]) != "true" || string(final["state"]) != `"idle"` || string(final["delivery_id"]) != `"`+closed.DeliveryID()+`"` {
			t.Fatal("original delivery not recovered", final)
		}
		_, originalID, outcome, found := readEntry()
		if !found || originalID != receiptID || outcome != "committed" || commits.Load() != 1 {
			t.Fatal("association changed identity", outcome)
		}
		// Duplicate producer hook is metadata replay, never new upload/business.
		if out := invoke("enqueue-result", raw); string(out["associated"]) != "true" {
			t.Fatal("duplicate hook lost final receipt", out)
		}
	}
	if creates.Load() != 1 || completes.Load() != 1 {
		t.Fatal("duplicate upload", creates.Load(), completes.Load())
	}
	data, err := os.ReadFile(filepath.Join(source, closed.Artifact.SourceRef)) //nolint:gosec // Isolated producer file.
	if err != nil || string(data) != "synthetic delivery content" {
		t.Fatal("source changed", err)
	}
	reader, asset, err := assets.OpenSeekable(ctx, id.Binding.BotID, ref.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || asset.ContentHash != ref.ContentHash || string(data) != "synthetic delivery content" {
		t.Fatal("original content changed", err)
	}
	t.Log("actual CLI + full-migration PG + TLS: original operation/attempt/delivery/receipt retained; creates=1, completes=1; mode=", mode)
}
