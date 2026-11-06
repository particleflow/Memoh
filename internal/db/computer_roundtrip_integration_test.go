//go:build integration

package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	"github.com/felinics/memoh/internal/transferclient"
)

// Capture only the response being lost. Preserve net/http's real deadline
// controller: a plain ResponseRecorder cannot enforce the upload read timeout.
type interruptedRouteResponse struct {
	*httptest.ResponseRecorder
	network http.ResponseWriter
}

func (w interruptedRouteResponse) Unwrap() http.ResponseWriter { return w.network }

// This uses the parent's full-migration PostgreSQL/NOBYPASSRLS fixture. HTTP
// requests pass through the actual Server, device verifier, delivery ledger and
// filesystem Media store. The wrapper only interrupts real responses. Uploads
// use the production CLI. Downloads use its production transport library: Linux
// has no LocalPublication implementation and must not claim local publication.
func exerciseRealRouteRoundTrip(ctx context.Context, t *testing.T, store *postgresstore.Store, id computertask.DeliveryIdentity) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux cloud route acceptance; Windows NTFS publication has separate native coverage")
	}
	root := t.TempDir()
	config, state, source, originals := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source"), filepath.Join(root, "originals")
	for _, dir := range []string{config, state, source, originals} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Bounded synthetic data crosses both upload and download chunk boundaries.
	content := bytes.Repeat([]byte("isolated-roundtrip-content\x00"), (9<<20)/26+1)
	sum := sha256.Sum256(content)
	spec := transferclient.Spec{SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(sum[:]), Mime: "application/octet-stream"}
	closed := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID, TargetID: id.Binding.TargetID, RunID: id.Binding.RunID, Attempt: id.Attempt, Generation: id.Generation, RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "roundtrip-result", Mime: spec.Mime}}, Closed: true, SHA256: spec.SHA256, SizeBytes: spec.SizeBytes}
	op := dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID}
	current, err := store.ReadComputerOperation(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	var details computertask.Details
	if json.Unmarshal(current.Details, &details) != nil || details.Batch == nil {
		t.Fatal("missing original batch")
	}
	details.Batch.Progress.Producer = &closed.Plan
	current.Details, _ = json.Marshal(details)
	if _, err := store.SaveComputerOperation(ctx, current); err != nil {
		t.Fatal(err)
	}
	verifier := newRegisteredTransferVerifier(ctx, t, store, id)
	log := slog.New(slog.DiscardHandler)
	assets := media.NewService(nil, localfs.NewOriginals(originals))
	assets.SetRequirePersistence(true)
	if err := assets.EnableUploads(originals); err != nil { //nolint:contextcheck // Startup initializer; real HTTP requests carry their own contexts.
		t.Fatal(err)
	}
	defer func() { _ = assets.Uploads().Close() }()
	messages := handlers.NewMessageHandler(log, nil, nil, nil, nil)
	messages.SetMediaService(assets)
	active, err := handlers.NewDeviceComputerTransferHandler(ctx, computertask.NewPostgresDeliveryService(store, assets), messages, verifier, log)
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close()
	endpoint := server.NewServer(log, ":0", "isolated-roundtrip", active) //nolint:contextcheck // Real HTTP requests supply their own contexts.
	var creates, completes, commits, gets atomic.Int64
	var dropPatch, dropDownload atomic.Bool
	dropPatch.Store(true)
	dropDownload.Store(true)
	patchResult := make(chan media.Upload, 1)
	ranges := make(chan string, 4)
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media/uploads") {
			creates.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/complete") {
			completes.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/commit") {
			commits.Add(1)
		}
		if r.Method == http.MethodPatch && dropPatch.Swap(false) {
			response := httptest.NewRecorder()
			endpoint.ServeHTTP(interruptedRouteResponse{response, w}, r)
			var receipt media.Upload
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &receipt) != nil {
				t.Error("real PATCH failed before response interruption", response.Code)
			}
			patchResult <- receipt
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/"+spec.SHA256) {
			gets.Add(1)
			ranges <- r.Header.Get("Range")
			if dropDownload.Swap(false) {
				response := httptest.NewRecorder()
				endpoint.ServeHTTP(response, r)
				if response.Code != http.StatusOK || response.Body.Len() != len(content) {
					t.Error("real download failed before response interruption", response.Code)
				}
				for key, values := range response.Header() {
					w.Header()[key] = values
				}
				w.WriteHeader(response.Code)
				_, _ = w.Write(response.Body.Bytes()[:min(response.Body.Len(), transferclient.MaxChunkBytes+(128<<10))])
				w.(http.Flusher).Flush()
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
				return
			}
		}
		endpoint.ServeHTTP(w, r)
	}))
	defer https.Close()
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": https.URL, "source_root": source})
	write(filepath.Join(config, "transfer.json"), settings)
	write(filepath.Join(config, "device.credential"), []byte(verifier.token))
	write(filepath.Join(source, closed.Artifact.SourceRef), content)
	sourceInfo, err := os.Stat(filepath.Join(source, closed.Artifact.SourceRef))
	if err != nil {
		t.Fatal(err)
	}
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
	binary := filepath.Join(root, "transferqueue")
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, "../../cmd/transferqueue") //nolint:gosec // Fixed repository build, never a downloaded artifact.
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual CLI: %v %s", err, output)
	}
	invoke := func(action, delivery string, input []byte, wantExit int) map[string]json.RawMessage {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, "-action", action, "-delivery", delivery) //nolint:gosec // Fixed actions, no shell or secrets in argv.
		cmd.Env = append(os.Environ(), "HERDR_PLUGIN_CONFIG_DIR="+config, "HERDR_PLUGIN_STATE_DIR="+state, "SSL_CERT_FILE="+cert)
		cmd.Stdin = bytes.NewReader(input)
		var output, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &stderr
		err := cmd.Run()
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != wantExit {
			t.Fatalf("CLI %s: %v %s %s", action, err, output.String(), stderr.String())
		}
		for _, secret := range []string{verifier.token, source, https.URL} {
			if bytes.Contains(output.Bytes(), []byte(secret)) || bytes.Contains(stderr.Bytes(), []byte(secret)) {
				t.Fatal("private fixture data leaked")
			}
		}
		var status map[string]json.RawMessage
		if json.Unmarshal(output.Bytes(), &status) != nil {
			t.Fatal("invalid CLI JSON")
		}
		return status
	}
	raw, _ := json.Marshal(closed)
	if out := invoke("enqueue-result", "", raw, 0); string(out["registered"]) != "true" || string(out["admitted"]) == "true" {
		t.Fatal("producer metadata was not registered before indexing")
	}
	first := invoke("advance-results", "", nil, 0)
	if string(first["state"]) != `"waiting"` {
		t.Fatal("lost PATCH response did not preserve retry", first)
	}
	receipt := <-patchResult
	if receipt.ID == "" || receipt.Offset <= 0 || receipt.Offset >= spec.SizeBytes {
		t.Fatal("fault did not leave a partial durable server offset")
	}
	// Each advance is a new real CLI process. Respect its persisted due time.
	var final map[string]json.RawMessage
	for range 8 {
		summary := invoke("lifecycle-summary", "", nil, 0)
		if string(summary["state"]) == `"waiting"` {
			var due time.Time
			if json.Unmarshal(summary["next_due_at"], &due) != nil || time.Until(due) > 10*time.Second {
				t.Fatal("unbounded fixture retry")
			}
			timer := time.NewTimer(max(time.Until(due), 0) + 20*time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				t.Fatal(ctx.Err())
			}
		}
		final = invoke("advance-results", "", nil, 0)
		if string(final["associated"]) == "true" {
			break
		}
	}
	var ref transferclient.Reference
	if string(final["associated"]) != "true" || json.Unmarshal(final["reference"], &ref) != nil || ref.ContentHash != spec.SHA256 || ref.SizeBytes != spec.SizeBytes || ref.DeliveryID != closed.DeliveryID() || ref.OperationID != id.OperationID {
		t.Fatal("original CLI delivery/reference not recovered", final)
	}
	readProof := func() []byte {
		t.Helper()
		current, err := store.ReadComputerOperation(ctx, op)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := store.ReadComputerDeliveryEntry(ctx, current, closed.DeliveryID())
		if err != nil {
			t.Fatal(err)
		}
		entry := json.RawMessage(stored.Data)
		var proof struct {
			Outcome  string `json:"outcome"`
			Transfer struct {
				ID    string       `json:"upload_id"`
				Ready media.Upload `json:"ready"`
			} `json:"transfer"`
		}
		if json.Unmarshal(entry, &proof) != nil || proof.Outcome != "committed" || proof.Transfer.ID != receipt.ID || proof.Transfer.Ready.ContentHash != spec.SHA256 || proof.Transfer.Ready.SizeBytes != spec.SizeBytes {
			t.Fatal("missing original receipt/committed proof")
		}
		return entry
	}
	proof := readProof()
	if out := invoke("enqueue-result", "", raw, 0); string(out["associated"]) != "true" {
		t.Fatal("duplicate producer did not replay confirmed receipt")
	}
	conflict := closed
	conflict.SHA256 = strings.Repeat("0", 64)
	conflicting, _ := json.Marshal(conflict)
	invoke("enqueue-result", "", conflicting, 1)
	if out := invoke("enqueue-result", "", raw, 0); string(out["associated"]) != "true" || !bytes.Equal(readProof(), proof) {
		t.Fatal("conflicting replay changed confirmed identity")
	}
	if creates.Load() != 1 || completes.Load() != 1 || commits.Load() != 1 {
		t.Fatal("upload retried as new business", creates.Load(), completes.Load(), commits.Load())
	}
	job := transferclient.Job{Version: 1, DeliveryID: "roundtrip-download", OperationID: id.OperationID, BotID: id.Binding.BotID, SnapshotID: "roundtrip-download", LocatorRevision: "native-profile-v1", Phase: "queued", Task: pgDeviceTask(id), Spec: spec}
	newClient := func(job transferclient.Job) *transferclient.Client {
		t.Helper()
		client, err := transferclient.NewDeviceTask(https.URL, job, "download", job.Task, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + verifier.token, nil })
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	// No fake LocalPublication. This is only a private, uncommitted staging file.
	staging := filepath.Join(root, "download.part")
	checkpointPath := filepath.Join(root, "download.offset")
	f, err := os.OpenFile(staging, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600) //nolint:gosec // Fixed test-owned private staging path.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	checkpoint := func(offset int64) error {
		file, err := os.OpenFile(checkpointPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // Fixed test-owned offset file; not a production store substitute.
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		if _, err := fmt.Fprint(file, offset); err != nil {
			return err
		}
		return file.Sync()
	}
	if err := newClient(job).Download(ctx, job.BotID, spec.SHA256, spec.SizeBytes, 0, f, checkpoint); !errors.Is(err, transferclient.ErrTransport) {
		t.Fatal("interrupted real download was not retryable", err)
	}
	_ = f.Close()
	checkpointBytes, err := os.ReadFile(checkpointPath) //nolint:gosec // Fixed private fixture checkpoint.
	if err != nil {
		t.Fatal(err)
	}
	offset, err := strconv.ParseInt(string(checkpointBytes), 10, 64)
	if err != nil || offset < transferclient.MaxChunkBytes || offset >= spec.SizeBytes {
		t.Fatal("no incomplete durable download checkpoint", offset, err)
	}
	f, err = os.OpenFile(staging, os.O_RDWR, 0o600) //nolint:gosec // Reopen the same private fixture staging file.
	if err != nil {
		t.Fatal(err)
	}
	https.Client().CloseIdleConnections()
	if err := newClient(job).Download(ctx, job.BotID, spec.SHA256, spec.SizeBytes, offset, f, checkpoint); err != nil {
		t.Fatal("actual Range resume/hash verification", err)
	}
	if gets.Load() != 2 || <-ranges != "" || <-ranges != fmt.Sprintf("bytes=%d-", offset) {
		t.Fatal("download did not resume the original checkpoint")
	}
	assertContent := func(path string) {
		t.Helper()
		data, err := os.ReadFile(path) //nolint:gosec // Only the two test-owned paths below, never remote input.
		if err != nil || !bytes.Equal(data, content) {
			t.Fatal("source or staged result changed", err)
		}
	}
	assertContent(staging)
	assertContent(filepath.Join(source, closed.Artifact.SourceRef))
	if after, err := os.Stat(filepath.Join(source, closed.Artifact.SourceRef)); err != nil || !os.SameFile(sourceInfo, after) || sourceInfo.Size() != after.Size() || sourceInfo.ModTime() != after.ModTime() {
		t.Fatal("producer source was replaced or modified", err)
	}
	reader, original, err := assets.OpenSeekable(ctx, job.BotID, spec.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	// Resolve/OpenSeekable intentionally does not populate Asset.SizeBytes.
	// Verify actual bytes, as the production delivery verifier does via Seek.
	if err != nil || !bytes.Equal(data, content) || original.ContentHash != spec.SHA256 || int64(len(data)) != spec.SizeBytes || !bytes.Equal(readProof(), proof) {
		t.Fatal("original or committed proof changed on retry/read", err)
	}
	// The executable cannot publish downloads on Linux. Admission succeeds but
	// advance must fail before making a request; it must never report delivered.
	admission := func(job transferclient.Job) []byte {
		raw, _ := json.Marshal(map[string]any{"job": job})
		return raw
	}
	if out := invoke("enqueue-download", "", admission(job), 0); string(out["admitted"]) != "true" {
		t.Fatal("download admission")
	}
	beforeGets := gets.Load()
	if out := invoke("advance", job.DeliveryID, nil, 1); string(out["state"]) != `"unavailable"` || gets.Load() != beforeGets {
		t.Fatal("Linux CLI fabricated local publication", out)
	}
	if out := invoke("inspect", job.DeliveryID, nil, 0); string(out["associated"]) != "false" || string(out["phase"]) != `"queued"` {
		t.Fatal("unsupported publication changed queue", out)
	}
	large := job
	large.DeliveryID, large.SnapshotID, large.Spec.SizeBytes = "large-metadata", "large-metadata", 1<<30
	if out := invoke("enqueue-download", "", admission(large), 0); string(out["admitted"]) != "true" || string(out["offset"]) != "0" {
		t.Fatal("CLI download metadata rejected by old byte allowance", out)
	}
	for i := 1; i < 9; i++ {
		pending := job
		pending.DeliveryID = fmt.Sprintf("pending-%d", i)
		pending.SnapshotID = pending.DeliveryID
		if out := invoke("enqueue-download", "", admission(pending), 0); string(out["admitted"]) != "true" || string(out["offset"]) != "0" {
			t.Fatal("CLI download backlog rejected by old active-slot allowance", out)
		}
	}
	if gets.Load() != beforeGets {
		t.Fatal("metadata registration started payload transfer")
	}
	// Same device identity must still fail a wrong attempt and a later revoke.
	wrong := job
	wrong.Task.Attempt = strings.Repeat("c", 32)
	assertDenied := func(client *transferclient.Client) {
		t.Helper()
		err := client.Download(ctx, job.BotID, spec.SHA256, spec.SizeBytes, spec.SizeBytes, f, checkpoint)
		var status *transferclient.HTTPError
		if !errors.As(err, &status) || status.Status != http.StatusUnauthorized && status.Status != http.StatusForbidden {
			t.Fatal("unauthorized reference accepted", err)
		}
	}
	assertDenied(newClient(wrong))
	verifier.revoke(ctx, t)
	assertDenied(newClient(job))
	assertContent(staging)
	assertContent(filepath.Join(source, closed.Artifact.SourceRef))
	t.Logf("real HTTPS Server/PG/filesystem: CLI upload %d bytes, lost PATCH response -> same receipt, Create/Complete/Commit=1; library download Range resumed at %d, full content/hash match; CLI Linux publication unavailable; large/backlog metadata accepted without payload GET; attempt/revoke checks enforced", spec.SizeBytes, offset)
}
