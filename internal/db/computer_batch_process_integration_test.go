//go:build integration

package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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
	"github.com/felinics/memoh/internal/transferclient"
)

// This is a bounded cloud test of the existing CLI and real NAS modules. Only
// the CLI process is killed. Reopening Media/handlers is not a server process
// kill, power loss, a Windows publication test, or a bandwidth benchmark.
func exerciseBatchProcessRecovery(ctx context.Context, t *testing.T, store *postgresstore.Store, id computertask.DeliveryIdentity) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux cloud CLI/real NAS route fixture; Windows publication is covered separately")
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
	plan := deliverymeta.Plan{Version: 2, Task: deliverymeta.Task{OperationID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID, TargetID: id.Binding.TargetID, RunID: id.Binding.RunID, Attempt: id.Attempt, Generation: id.Generation, RootHash: deliverymeta.RootHash(source)}}
	var files []deliverymeta.Closed
	var sourceInfos []os.FileInfo
	// 51 MiB + 369 bytes of sources; each file spans at least three chunks.
	// At most one content buffer lives here, and all fixture files use t.TempDir.
	for i := range 3 {
		content := bytes.Repeat([]byte{byte(i + 17)}, (17<<20)+123)
		sum := sha256.Sum256(content)
		artifact := deliverymeta.Artifact{SourceRef: fmt.Sprintf("batch-result-%d", i), Mime: "application/octet-stream"}
		plan.Artifacts = append(plan.Artifacts, artifact)
		files = append(files, deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: plan.Task, Artifact: artifact}, Closed: true, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(content))})
		path := filepath.Join(source, artifact.SourceRef)
		write(path, content)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		sourceInfos = append(sourceInfos, info)
	}
	scope := dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID}
	current, err := store.ReadComputerOperation(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	var details computertask.Details
	if json.Unmarshal(current.Details, &details) != nil || details.Batch == nil {
		t.Fatal("missing batch")
	}
	details.Batch.Progress.Producer = &plan
	current.Details, _ = json.Marshal(details)
	if _, err := store.SaveComputerOperation(ctx, current); err != nil {
		t.Fatal(err)
	}
	verifier := newRegisteredTransferVerifier(ctx, t, store, id)
	log := slog.New(slog.DiscardHandler)
	var assets *media.Service
	var active *handlers.ComputerTransferHandler
	var endpoint http.Handler
	var dispatch sync.RWMutex
	start := func() {
		t.Helper()
		assets = media.NewService(nil, localfs.NewOriginals(originals))
		assets.SetRequirePersistence(true)
		if err := assets.EnableUploads(originals); err != nil { //nolint:contextcheck // Startup initializer; actual requests carry their own contexts.
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
		endpoint = server.NewServer(log, ":0", "isolated-batch-process", active) //nolint:contextcheck // Actual HTTP requests supply their own contexts.
		dispatch.Unlock()
	}
	start()
	defer func() { active.Close(); _ = assets.Uploads().Close() }()
	type requestFacts struct {
		creates, completes, commits int
		accepted, attempted         []int64
		upload                      media.Upload
	}
	facts := make(map[string]*requestFacts)
	for _, file := range files {
		facts[file.DeliveryID()] = &requestFacts{}
	}
	var factsMu sync.Mutex
	var pauseFirst atomic.Bool
	var rejected atomic.Int64
	entered, finished := make(chan media.Upload, 1), make(chan struct{})
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatch.RLock()
		defer dispatch.RUnlock()
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
		if len(parts) < 4 || parts[0] != "internal" || parts[1] != "computer-transfers" {
			t.Error("request escaped the configured API base")
			http.NotFound(w, r)
			return
		}
		factsMu.Lock()
		fact := facts[parts[3]]
		if fact != nil {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media/uploads") {
				fact.creates++
			}
			if strings.HasSuffix(r.URL.Path, "/complete") {
				fact.completes++
			}
			if strings.HasSuffix(r.URL.Path, "/commit") {
				fact.commits++
			}
		}
		factsMu.Unlock()
		serve := http.StripPrefix("/api", endpoint)
		if r.Method != http.MethodPatch || fact == nil {
			serve.ServeHTTP(w, r)
			return
		}
		offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
		if err != nil {
			t.Error("missing PATCH offset")
		}
		factsMu.Lock()
		fact.attempted = append(fact.attempted, offset)
		factsMu.Unlock()
		if parts[3] == files[0].DeliveryID() && pauseFirst.Load() {
			rejected.Add(1)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response := httptest.NewRecorder()
		serve.ServeHTTP(interruptedRouteResponse{response, w}, r)
		var receipt media.Upload
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &receipt) != nil {
			t.Error("real PATCH failed", response.Code)
		} else {
			factsMu.Lock()
			fact.accepted = append(fact.accepted, offset)
			fact.upload = receipt
			killWindow := parts[3] == files[0].DeliveryID() && len(fact.accepted) == 2
			factsMu.Unlock()
			if killWindow {
				entered <- receipt
				<-r.Context().Done() // Parent kills the real CLI before this response/checkpoint.
				close(finished)
				return
			}
		}
		for key, values := range response.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	}))
	defer https.Close()
	settings, _ := json.Marshal(map[string]any{"version": 1, "origin": https.URL + "/api", "source_root": source})
	write(filepath.Join(config, "transfer.json"), settings)
	write(filepath.Join(config, "device.credential"), []byte(verifier.token))
	attempt, ok := deliverymeta.AttemptDirectory(id.Attempt)
	if !ok {
		t.Fatal("invalid attempt")
	}
	attemptPath := filepath.Join(source, filepath.FromSlash(attempt))
	if err := os.MkdirAll(attemptPath, 0o700); err != nil {
		t.Fatal(err)
	}
	rawPlan, _ := json.Marshal(plan)
	write(filepath.Join(attemptPath, deliverymeta.ProducerContextName), rawPlan)
	cert := filepath.Join(root, "isolated-ca.pem")
	write(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: https.Certificate().Raw}))
	binary := filepath.Join(root, "transferqueue")
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, "../../cmd/transferqueue") //nolint:gosec // Fixed local source, isolated output; no downloaded artifacts.
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, output)
	}
	command := func(action, delivery string, input []byte) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, "-action", action, "-delivery", delivery) //nolint:gosec // Fixed actions/IDs; no shell or secrets in argv.
		cmd.Env = append(os.Environ(), "HERDR_PLUGIN_CONFIG_DIR="+config, "HERDR_PLUGIN_STATE_DIR="+state, "SSL_CERT_FILE="+cert)
		cmd.Stdin = bytes.NewReader(input)
		return cmd
	}
	checkOutput := func(output, stderr []byte) {
		t.Helper()
		for _, secret := range []string{verifier.token, source, https.URL} {
			if bytes.Contains(output, []byte(secret)) || bytes.Contains(stderr, []byte(secret)) {
				t.Fatal("private fixture data leaked")
			}
		}
	}
	invoke := func(action, delivery string, input []byte) map[string]json.RawMessage {
		t.Helper()
		cmd := command(action, delivery, input)
		var output, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &stderr
		err := cmd.Run()
		checkOutput(output.Bytes(), stderr.Bytes())
		if err != nil {
			t.Fatalf("CLI %s: %v %s %s", action, err, output.String(), stderr.String())
		}
		var out map[string]json.RawMessage
		if json.Unmarshal(output.Bytes(), &out) != nil {
			t.Fatal("invalid CLI JSON")
		}
		return out
	}
	admit := func(file deliverymeta.Closed) map[string]json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(file)
		return invoke("enqueue-result", "", raw)
	}
	admitPage := func(files []deliverymeta.Closed) {
		t.Helper()
		raw, _ := json.Marshal(deliverymeta.ClosedPage{Version: 1, Files: files})
		out := invoke("enqueue-results", "", raw)
		var items []transferclient.ProducerPageFile
		if json.Unmarshal(out["files"], &items) != nil || len(items) != len(files) {
			t.Fatal("page registration result")
		}
		for i, item := range items {
			if !item.Registered || item.DeliveryID != files[i].DeliveryID() {
				t.Fatal("page changed original delivery")
			}
		}
	}
	admitPage(files[:1])
	child := command("advance-results", "", nil)
	var childOut, childErr bytes.Buffer
	child.Stdout, child.Stderr = &childOut, &childErr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	var stopped bool
	defer func() {
		if !stopped {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	var durable media.Upload
	select {
	case durable = <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("CLI did not reach second durable PATCH")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("child did not exit abnormally")
	}
	stopped = true
	checkOutput(childOut.Bytes(), childErr.Bytes())
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("interrupted request did not finish")
	}
	firstStatus := invoke("inspect", files[0].DeliveryID(), nil)
	var checkpoint int64
	if json.Unmarshal(firstStatus["offset"], &checkpoint) != nil || checkpoint <= 0 || checkpoint >= durable.Offset || durable.Offset >= files[0].SizeBytes || string(firstStatus["associated"]) != "false" {
		t.Fatal("fault did not separate client checkpoint and durable server offset")
	}
	if reader, _, err := assets.OpenSeekable(ctx, id.Binding.BotID, files[0].SHA256); err == nil {
		_ = reader.Close()
		t.Fatal("partial upload was visible as an original")
	}
	// Reopen the actual NAS stores/handlers on the same disk/PG and API base.
	active.Close()
	if err := assets.Uploads().Close(); err != nil {
		t.Fatal(err)
	}
	start()
	reopened, err := assets.Uploads().Get(ctx, id.Binding.UserID, id.Binding.BotID, durable.ID)
	if err != nil || reopened.Offset != durable.Offset || reopened.State != "uploading" {
		t.Fatal("server reopen lost the confirmed receipt/offset", err)
	}
	pauseFirst.Store(true)
	if out := invoke("advance-results", "", nil); string(out["state"]) != `"waiting"` || rejected.Load() != 1 {
		t.Fatal("temporary failure did not retain a retryable original delivery")
	}
	admitPage(files[1:])
	admitPage(files[:1]) // Lost page output/restart uses the original receipt.
	admitPage(files[1:])
	waitDue := func() {
		t.Helper()
		summary := invoke("lifecycle-summary", "", nil)
		if string(summary["state"]) != `"waiting"` {
			return
		}
		var due time.Time
		if json.Unmarshal(summary["next_due_at"], &due) != nil || time.Until(due) > 15*time.Second {
			t.Fatal("unexpected retry budget")
		}
		timer := time.NewTimer(max(time.Until(due), 0) + 20*time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	associated := func(file deliverymeta.Closed) bool {
		return string(invoke("inspect", file.DeliveryID(), nil)["associated"]) == "true"
	}
	for n := 0; n < 12 && (!associated(files[1]) || !associated(files[2])); n++ {
		waitDue()
		invoke("advance-results", "", nil)
	}
	if !associated(files[1]) || !associated(files[2]) || associated(files[0]) {
		t.Fatal("one temporarily failed file blocked independent batch members")
	}
	pauseFirst.Store(false)
	for n := 0; n < 8 && !associated(files[0]); n++ {
		waitDue()
		invoke("advance-results", "", nil)
	}
	if !associated(files[0]) {
		t.Fatal("original interrupted delivery did not resume")
	}
	assertLedger := func() map[string]json.RawMessage {
		t.Helper()
		op, err := store.ReadComputerOperation(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		entries := make(map[string]json.RawMessage)
		for _, file := range files {
			stored, err := store.ReadComputerDeliveryEntry(ctx, op, file.DeliveryID())
			if err != nil {
				t.Fatal(err)
			}
			entries[file.DeliveryID()] = json.RawMessage(stored.Data)
			var proof struct {
				Outcome   string                       `json:"outcome"`
				Reference computertask.DeliveryRequest `json:"reference"`
				Transfer  struct {
					ID    string       `json:"upload_id"`
					Ready media.Upload `json:"ready"`
				} `json:"transfer"`
			}
			factsMu.Lock()
			upload := facts[file.DeliveryID()].upload
			factsMu.Unlock()
			if json.Unmarshal(entries[file.DeliveryID()], &proof) != nil || proof.Outcome != "committed" || proof.Reference.ContentHash != file.SHA256 || proof.Reference.SizeBytes != file.SizeBytes || proof.Transfer.ID != upload.ID || proof.Transfer.Ready.ContentHash != file.SHA256 || proof.Transfer.Ready.SizeBytes != file.SizeBytes {
				t.Fatal("committed original ID/reference/receipt not preserved")
			}
		}
		return entries
	}
	proofs := assertLedger()
	for i, file := range files {
		for range 2 {
			if string(admit(file)["associated"]) != "true" {
				t.Fatal("duplicate hook did not replay confirmed history")
			}
		}
		job := transferclient.Job{Version: 1, DeliveryID: fmt.Sprintf("batch-download-%d", i), OperationID: id.OperationID, BotID: id.Binding.BotID, SnapshotID: fmt.Sprintf("batch-download-%d", i), LocatorRevision: "native-profile-v1", Phase: "queued", Task: pgDeviceTask(id), Spec: transferclient.Spec{SHA256: file.SHA256, SizeBytes: file.SizeBytes, Mime: file.Artifact.Mime}}
		client, err := transferclient.NewDeviceTask(https.URL+"/api", job, "download", job.Task, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + verifier.token, nil })
		if err != nil {
			t.Fatal(err)
		}
		// Reuse one staging file, never install a Linux publication binding.
		download, err := os.OpenFile(filepath.Join(root, "download.part"), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600) //nolint:gosec // Fixed fixture-only private path.
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Download(ctx, job.BotID, file.SHA256, file.SizeBytes, 0, download, func(int64) error { return nil }); err != nil {
			_ = download.Close()
			t.Fatal("download committed reference", err)
		}
		assertHash := func(reader io.ReadSeeker) {
			t.Helper()
			if _, err := reader.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			hash := sha256.New()
			n, err := io.Copy(hash, reader)
			if err != nil || n != file.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
				t.Fatal("size/full SHA256 mismatch", err)
			}
		}
		assertHash(download)
		_ = download.Close()
		src, err := os.Open(filepath.Join(source, file.Artifact.SourceRef)) //nolint:gosec // Fixed synthetic producer path.
		if err != nil {
			t.Fatal(err)
		}
		assertHash(src)
		after, err := src.Stat()
		_ = src.Close()
		if err != nil || !os.SameFile(sourceInfos[i], after) || sourceInfos[i].ModTime() != after.ModTime() {
			t.Fatal("source file replaced or modified")
		}
	}
	afterProofs := assertLedger()
	for key, before := range proofs {
		if !bytes.Equal(before, afterProofs[key]) {
			t.Fatal("replay/download changed existing ledger entry")
		}
	}
	factsMu.Lock()
	defer factsMu.Unlock()
	for i, file := range files {
		fact := facts[file.DeliveryID()]
		if fact.creates != 1 || fact.completes != 1 || fact.commits != 1 || len(fact.accepted) < 3 || fact.upload.Offset != file.SizeBytes {
			t.Fatalf("file %d duplicated or incomplete: Create=%d Complete=%d Commit=%d accepted=%v", i, fact.creates, fact.completes, fact.commits, fact.accepted)
		}
		for j, offset := range fact.accepted {
			if (j == 0 && offset != 0) || (j > 0 && offset <= fact.accepted[j-1]) {
				t.Fatal("an acknowledged chunk was applied twice")
			}
		}
		if i == 0 {
			for _, offset := range fact.attempted[2:] {
				if offset < durable.Offset {
					t.Fatal("restart retransmitted a server-confirmed chunk")
				}
			}
		}
		t.Logf("file=%d bytes=%d accepted PATCH offsets=%v Create/Complete/Commit=1 source+download SHA256=match", i, file.SizeBytes, fact.accepted)
	}
	t.Logf("CLI killed after durable offset=%d, local checkpoint=%d; reopened NAS modules; transient rejections=%d; two independent files committed before recovery; original receipt and ledger replay preserved", durable.Offset, checkpoint, rejected.Load())
}
