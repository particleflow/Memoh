//go:build integration && linux

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
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/transferclient"
)

// Opt-in resource acceptance: actual bytes through the production Server/PG/CLI.
// These fixture sizes bound this test, not the product's queue or file limits.
func exerciseLargeByteResources(parent context.Context, t *testing.T, store *postgresstore.Store, pool *pgxpool.Pool, original computertask.DeliveryIdentity) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	root := t.TempDir()
	var initialSpace syscall.Statfs_t
	if err := syscall.Statfs(root, &initialSpace); err != nil {
		t.Fatal(err)
	}
	initialFree := int64(initialSpace.Bavail) * initialSpace.Bsize //nolint:gosec // Local fixture filesystem size is below MaxInt64.
	// Source + in-flight payload + final copy, plus a 1 GiB reserve and metadata.
	if initialFree < (4<<30)+(64<<20) {
		t.Fatalf("large-byte fixture requires 4 GiB + 64 MiB free; available=%d", initialFree)
	}
	t.Logf("PREFLIGHT free_bytes=%d large_file_bytes=%d small_files=512 small_file_bytes=65536 reserve_bytes=%d filesystem_type=%x", initialFree, int64(1<<30), int64(1<<30), initialSpace.Type)
	config, state, source, originals := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source"), filepath.Join(root, "originals")
	for _, p := range []string{config, state, source, originals} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p string, b []byte) {
		t.Helper()
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var sampleMu sync.Mutex
	var peakAllocated, peakLogical, peakFiles, minFree, clientRSS int64
	minFree = 1 << 62
	var sampleErr error
	sample := func() {
		var allocated, logical, count int64
		_ = filepath.WalkDir(root, func(_ string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return nil
			}
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				allocated += st.Blocks * 512
			}
			logical += info.Size()
			count++
			return nil
		})
		var space syscall.Statfs_t
		err := syscall.Statfs(root, &space)
		free := int64(space.Bavail) * space.Bsize //nolint:gosec // Local fixture filesystem size is below MaxInt64.
		sampleMu.Lock()
		defer sampleMu.Unlock()
		peakAllocated = max(peakAllocated, allocated)
		peakLogical = max(peakLogical, logical)
		peakFiles = max(peakFiles, count)
		minFree = min(minFree, free)
		if err != nil || free < 1<<30 {
			sampleErr = fmt.Errorf("fixture space reserve violated: free=%d stat=%w", free, err)
			cancel()
		}
	}
	sample()
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				sample()
			case <-done:
				return
			}
		}
	}()
	defer func() { close(done); <-stopped }()
	var baselineDB int64
	if err := pool.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&baselineDB); err != nil {
		t.Fatal(err)
	}
	scope := dbstore.ComputerOperationRecord{ID: original.OperationID, BotID: original.Binding.BotID, UserID: original.Binding.UserID, SessionID: original.Binding.SessionID}
	template, err := store.ReadComputerOperation(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	template.State = "completed"
	if _, err := store.SaveComputerOperation(ctx, template); err != nil {
		t.Fatal("complete fixture business execution", err)
	}
	verifier := newRegisteredTransferVerifier(ctx, t, store, original)
	log := slog.New(slog.DiscardHandler)
	assets := media.NewService(nil, localfs.NewOriginals(originals))
	assets.SetRequirePersistence(true)
	// Use the existing server configuration setter only on this isolated instance.
	assets.SetMaxUploadBytes(1 << 30)
	if err := assets.EnableUploads(originals); err != nil { //nolint:contextcheck // Startup initializer; HTTP requests carry their own contexts.
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
	endpoint := server.NewServer(log, ":0", "isolated-scale", active) //nolint:contextcheck // Actual HTTP requests supply their own contexts.
	type fact struct {
		creates, completes, commits, gets, rejects int
		next, bytesSent, killAt                    int64
		offsets                                    []int64
		uploadID                                   string
		kill                                       bool
		reject                                     bool
	}
	var factsMu sync.Mutex
	facts := map[string]*fact{}
	killedAt, requestDone := make(chan media.Upload, 1), make(chan struct{})
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
		if len(parts) < 4 || parts[0] != "internal" || parts[1] != "computer-transfers" {
			t.Error("API base escaped")
			http.NotFound(w, r)
			return
		}
		key := parts[3]
		factsMu.Lock()
		f := facts[key]
		if f != nil {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media/uploads") {
				f.creates++
			}
			if strings.HasSuffix(r.URL.Path, "/complete") {
				f.completes++
			}
			if strings.HasSuffix(r.URL.Path, "/commit") {
				f.commits++
			}
			if r.Method == http.MethodGet {
				f.gets++
			}
		}
		factsMu.Unlock()
		serve := http.StripPrefix("/api", endpoint)
		if r.Method != http.MethodPatch || f == nil {
			serve.ServeHTTP(w, r)
			return
		}
		offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
		if err != nil {
			t.Error(err)
		}
		factsMu.Lock()
		if offset != f.next {
			t.Errorf("PATCH retransmitted/skipped confirmed data: got=%d want=%d", offset, f.next)
		}
		f.offsets = append(f.offsets, offset)
		if f.reject {
			f.reject = false
			f.rejects++
			factsMu.Unlock()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		factsMu.Unlock()
		rec := httptest.NewRecorder()
		serve.ServeHTTP(interruptedRouteResponse{rec, w}, r)
		var receipt media.Upload
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &receipt) != nil {
			t.Error("PATCH handler", rec.Code)
		} else {
			factsMu.Lock()
			f.next = receipt.Offset
			f.bytesSent += r.ContentLength
			f.uploadID = receipt.ID
			kill := f.kill && receipt.Offset >= f.killAt
			if kill {
				f.kill = false
			}
			factsMu.Unlock()
			if kill {
				killedAt <- receipt
				<-r.Context().Done()
				close(requestDone)
				return
			}
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	defer https.Close()
	raw, _ := json.Marshal(map[string]any{"version": 1, "origin": https.URL + "/api", "source_root": source})
	write(filepath.Join(config, "transfer.json"), raw)
	write(filepath.Join(config, "device.credential"), []byte(verifier.token))
	cert := filepath.Join(root, "isolated-ca.pem")
	write(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: https.Certificate().Raw}))
	binary := filepath.Join(root, "transferqueue")
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, "../../cmd/transferqueue") //nolint:gosec // Fixed local source, isolated output; no downloaded artifacts.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	command := func(action, id string, input []byte) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, "-action", action, "-delivery", id) //nolint:gosec // Fixed fixture actions/IDs; no shell or secrets in argv.
		cmd.Env = append(os.Environ(), "HERDR_PLUGIN_CONFIG_DIR="+config, "HERDR_PLUGIN_STATE_DIR="+state, "SSL_CERT_FILE="+cert)
		cmd.Stdin = bytes.NewReader(input)
		return cmd
	}
	check := func(cmd *exec.Cmd, out, stderr []byte) {
		t.Helper()
		for _, s := range []string{source, https.URL, verifier.token} {
			if bytes.Contains(out, []byte(s)) || bytes.Contains(stderr, []byte(s)) {
				t.Fatal("private fixture data in output")
			}
		}
		if cmd.ProcessState != nil {
			if r, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
				clientRSS = max(clientRSS, r.Maxrss*1024)
			}
		}
	}
	invoke := func(action, id string, input []byte) map[string]json.RawMessage {
		t.Helper()
		cmd := command(action, id, input)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		check(cmd, out.Bytes(), stderr.Bytes())
		if err != nil {
			t.Fatalf("CLI %s: %v %s %s", action, err, out.String(), stderr.String())
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(out.Bytes(), &result) != nil {
			t.Fatal("bad CLI JSON")
		}
		return result
	}
	admit := func(file deliverymeta.Closed) map[string]json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(file)
		return invoke("enqueue-result", "", raw)
	}
	associated := func(file deliverymeta.Closed) bool {
		return string(invoke("inspect", file.DeliveryID(), nil)["associated"]) == "true"
	}
	waitDue := func() {
		t.Helper()
		out := invoke("lifecycle-summary", "", nil)
		if string(out["state"]) != `"waiting"` {
			return
		}
		var due time.Time
		if json.Unmarshal(out["next_due_at"], &due) != nil || time.Until(due) > 15*time.Second {
			t.Fatal("unbounded retry")
		}
		timer := time.NewTimer(max(0, time.Until(due)) + 20*time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var originalsExpected, filesDone, sourceWritten, sourceAllocated, patchAttempts, patchRejected int64
	var largeUploadSecs, largeDownloadSecs, smallSecs float64
	overallStart := time.Now()
	for round := 0; round < 65; round++ {
		id := original
		id.OperationID = uuid.NewString()
		id.Binding.RunID = fmt.Sprintf("scale-%d", round)
		id.Attempt = fmt.Sprintf("%032x", round+1)
		plan := deliverymeta.Plan{Version: 2, Task: deliverymeta.Task{OperationID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID, TargetID: id.Binding.TargetID, RunID: id.Binding.RunID, Attempt: id.Attempt, Generation: id.Generation, RootHash: deliverymeta.RootHash(source)}}
		count, size := 8, int64(64<<10)
		if round == 0 {
			count = 1
			size = 1 << 30
		}
		files := make([]deliverymeta.Closed, 0, count)
		sourceInfo := map[string]os.FileInfo{}
		for index := 0; index < count; index++ {
			artifact := deliverymeta.Artifact{SourceRef: fmt.Sprintf("scale-%d-%d", round, index), Mime: "application/octet-stream"}
			plan.Artifacts = append(plan.Artifacts, artifact)
			path := filepath.Join(source, artifact.SourceRef)
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // Synthetic fixed fixture path below private temporary root.
			if err != nil {
				t.Fatal(err)
			}
			block := make([]byte, 64<<10)
			for j := range block {
				block[j] = byte((j*31 + round*19 + index*7) % 251)
			}
			copy(block, []byte(fmt.Sprintf("scale-%d-%d", round, index)))
			hash := sha256.New()
			writer := io.MultiWriter(f, hash)
			for n := int64(0); n < size; {
				length := min(int64(len(block)), size-n)
				w, err := writer.Write(block[:length])
				if err != nil {
					t.Fatal(err)
				}
				n += int64(w)
			}
			if err := f.Sync(); err != nil {
				t.Fatal(err)
			}
			info, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			allocated := info.Sys().(*syscall.Stat_t).Blocks * 512
			if allocated < size || info.Size() != size {
				t.Fatal("source is sparse or incomplete", allocated, info.Size(), size)
			}
			sourceWritten += size
			sourceAllocated += allocated
			sourceInfo[artifact.SourceRef] = info
			file := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: plan.Task, Artifact: artifact}, Closed: true, SHA256: hex.EncodeToString(hash.Sum(nil)), SizeBytes: size}
			files = append(files, file)
			factsMu.Lock()
			facts[file.DeliveryID()] = &fact{kill: round == 0, killAt: 256 << 20, reject: round > 0 && round%8 == 0 && index == 0}
			factsMu.Unlock()
		}
		details, _ := json.Marshal(computertask.Details{Kind: "herdr_pi_task", Batch: &computertask.BatchDetails{Progress: agenvo.BatchProgress{Attempt: id.Attempt, Generation: id.Generation, Producer: &plan}}, Phase: "submitted"})
		op, claimed, err := store.ClaimComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID, TargetID: id.Binding.TargetID, RunID: id.Binding.RunID, Fingerprint: id.Binding.RunID, TargetFingerprint: template.TargetFingerprint, Details: details})
		if err != nil || !claimed {
			t.Fatal("claim", claimed, err)
		}
		op.State = "completed"
		if op, err = store.SaveComputerOperation(ctx, op); err != nil {
			t.Fatal("close fixture execution", err)
		}
		attempt, ok := deliverymeta.AttemptDirectory(id.Attempt)
		if !ok {
			t.Fatal("attempt")
		}
		attemptPath := filepath.Join(source, filepath.FromSlash(attempt))
		if err := os.MkdirAll(attemptPath, 0o700); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(plan)
		write(filepath.Join(attemptPath, deliverymeta.ProducerContextName), raw)
		started := time.Now()
		for _, file := range files {
			if string(admit(file)["registered"]) != "true" {
				t.Fatal("admission")
			}
		}
		if round == 0 {
			var receipt media.Upload
			interrupted := false
			// Each finite action may first checkpoint an indexing prefix. Start a
			// fresh process until upload reaches the intentional lost-response barrier.
			for step := 0; step < 32 && !interrupted; step++ {
				child := command("advance-results", "", nil)
				var out, stderr bytes.Buffer
				child.Stdout, child.Stderr = &out, &stderr
				if err := child.Start(); err != nil {
					t.Fatal(err)
				}
				exited := make(chan error, 1)
				go func() { exited <- child.Wait() }()
				select {
				case receipt = <-killedAt:
					if err := child.Process.Kill(); err != nil {
						t.Fatal(err)
					}
					if err := <-exited; err == nil {
						t.Fatal("child not killed")
					}
					interrupted = true
				case err := <-exited:
					check(child, out.Bytes(), stderr.Bytes())
					if err != nil {
						t.Fatalf("finite pre-upload action: %v %s %s", err, out.String(), stderr.String())
					}
				case <-time.After(90 * time.Second):
					_ = child.Process.Kill()
					<-exited
					t.Fatal("large upload did not reach kill barrier")
				}
				check(child, out.Bytes(), stderr.Bytes())
			}
			if !interrupted {
				t.Fatal("finite actions did not reach upload interruption")
			}
			select {
			case <-requestDone:
			case <-time.After(5 * time.Second):
				t.Fatal("killed request did not close")
			}
			outStatus := invoke("inspect", files[0].DeliveryID(), nil)
			var offset int64
			_ = json.Unmarshal(outStatus["offset"], &offset)
			if offset != receipt.Offset-transferclient.MaxChunkBytes || receipt.Offset != 256<<20 {
				t.Fatal("kill/checkpoint mismatch", offset, receipt.Offset)
			}
			t.Logf("large interruption: server_offset=%d client_checkpoint=%d original_receipt_preserved=true", receipt.Offset, offset)
		}
		for steps := 0; steps < 96; steps++ {
			remaining := 0
			for _, file := range files {
				if !associated(file) {
					remaining++
				}
			}
			if remaining == 0 {
				break
			}
			if steps == 95 {
				t.Fatal("finite batch did not finish")
			}
			waitDue()
			invoke("advance-results", "", nil)
		}
		uploadSecs := time.Since(started).Seconds()
		if round == 0 {
			largeUploadSecs = uploadSecs
		}
		current, err := store.ReadComputerOperation(ctx, op)
		if err != nil {
			t.Fatal(err)
		}
		type proofEntry struct {
			Outcome   string                       `json:"outcome"`
			Reference computertask.DeliveryRequest `json:"reference"`
			Transfer  struct {
				ID    string       `json:"upload_id"`
				Ready media.Upload `json:"ready"`
			} `json:"transfer"`
		}
		beforeEntries := make(map[string]json.RawMessage, len(files))
		for _, file := range files {
			entry, err := store.ReadComputerDeliveryEntry(ctx, current, file.DeliveryID())
			if err != nil {
				t.Fatal("read committed entry", err)
			}
			beforeEntries[file.DeliveryID()] = json.RawMessage(entry.Data)
		}
		downloadStarted := time.Now()
		for index, file := range files {
			var proof proofEntry
			if err := json.Unmarshal(beforeEntries[file.DeliveryID()], &proof); err != nil {
				t.Fatal(err)
			}
			factsMu.Lock()
			f := *facts[file.DeliveryID()]
			factsMu.Unlock()
			if f.creates != 1 || f.completes != 1 || f.commits != 1 || f.next != size || f.bytesSent != size || proof.Outcome != "committed" || proof.Reference.ContentHash != file.SHA256 || proof.Reference.SizeBytes != size || proof.Transfer.ID != f.uploadID || proof.Transfer.Ready.ContentHash != file.SHA256 {
				t.Fatalf("duplicate/missing outcome: create=%d complete=%d commit=%d accepted=%d", f.creates, f.completes, f.commits, f.bytesSent)
			}
			job := transferclient.Job{Version: 1, DeliveryID: fmt.Sprintf("scale-download-%d-%d", round, index), OperationID: id.OperationID, BotID: id.Binding.BotID, SnapshotID: "download", LocatorRevision: "native-profile-v1", Phase: "queued", Task: pgDeviceTask(id), Spec: transferclient.Spec{SizeBytes: size, SHA256: file.SHA256, Mime: file.Artifact.Mime}}
			client, err := transferclient.NewDeviceTask(https.URL+"/api", job, "download", job.Task, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + verifier.token, nil })
			if err != nil {
				t.Fatal(err)
			}
			dst, err := os.OpenFile(filepath.Join(root, "download.part"), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600) //nolint:gosec // Reusable synthetic download staging, not a user destination.
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Download(ctx, job.BotID, file.SHA256, size, 0, dst, func(int64) error { return nil }); err != nil {
				_ = dst.Close()
				t.Fatal("download", err)
			}
			if _, err := dst.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			hash := sha256.New()
			n, err := io.Copy(hash, dst)
			_ = dst.Close()
			if err != nil || n != size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
				t.Fatal("download full hash mismatch")
			}
			src, err := os.Open(filepath.Join(source, file.Artifact.SourceRef)) //nolint:gosec // SourceRef is created in this fixture, never untrusted input.
			if err != nil {
				t.Fatal(err)
			}
			hash.Reset()
			n, err = io.Copy(hash, src)
			info, statErr := src.Stat()
			_ = src.Close()
			before := sourceInfo[file.Artifact.SourceRef]
			if err != nil || statErr != nil || n != size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 || !os.SameFile(before, info) || before.ModTime() != info.ModTime() {
				t.Fatal("source modified")
			}
			if string(admit(file)["associated"]) != "true" {
				t.Fatal("repeat hook not confirmed")
			}
			factsMu.Lock()
			f = *facts[file.DeliveryID()]
			factsMu.Unlock()
			if f.creates != 1 || f.completes != 1 || f.commits != 1 {
				t.Fatal("replay issued new work")
			}
			patchAttempts += int64(len(f.offsets))
			patchRejected += int64(f.rejects)
			originalsExpected += size
			filesDone++
		}
		if round == 0 {
			largeDownloadSecs = time.Since(downloadStarted).Seconds()
		} else {
			smallSecs += time.Since(started).Seconds()
		}
		after, err := store.ReadComputerOperation(ctx, op)
		if err != nil {
			t.Fatal("read replay ledger", err)
		}
		for key, before := range beforeEntries {
			entry, err := store.ReadComputerDeliveryEntry(ctx, after, key)
			if err != nil || !bytes.Equal(before, entry.Data) {
				t.Fatal("repeat/download changed prior upload proof", err)
			}
		}
		entries, err := os.ReadDir(filepath.Join(originals, ".uploads"))
		if err != nil {
			t.Fatal(err)
		}
		receipts, payloads := 0, 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				receipts++
			}
			if strings.HasSuffix(e.Name(), ".data") {
				payloads++
			}
		}
		if receipts != 0 || payloads != 0 {
			t.Fatal("confirmed receipts/payloads did not retire", receipts, payloads)
		}
		sample()
		t.Logf("round=%02d files=%d bytes_each=%d upload_seconds=%.3f total_seconds=%.3f upload_MiB_s=%.3f all_hashes_match=true create_complete_commit_each=1 retained_upload_receipts=%d", round, count, size, uploadSecs, time.Since(started).Seconds(), float64(int64(count)*size)/(1<<20)/uploadSecs, receipts)
	}
	sample()
	var rss syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &rss); err != nil {
		t.Fatal(err)
	}
	var finalDB int64
	if err := pool.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&finalDB); err != nil {
		t.Fatal(err)
	}
	sampleMu.Lock()
	defer sampleMu.Unlock()
	if sampleErr != nil {
		t.Fatal(sampleErr)
	}
	t.Logf("INTEGRITY source_written_bytes=%d source_allocated_bytes=%d accepted_upload_bytes=%d verified_download_bytes=%d patch_attempts=%d rejected_before_apply=%d duplicate_confirmed_patch_bytes=0 create_complete_commit_each=1 original_sources_unchanged=true", sourceWritten, sourceAllocated, originalsExpected, originalsExpected, patchAttempts, patchRejected)
	t.Logf("SUMMARY files=%d bytes=%d wall_seconds=%.3f large_upload_seconds=%.3f large_download_verify_replay_seconds=%.3f small_batch_seconds=%.3f small_files_per_second=%.3f", filesDone, originalsExpected, time.Since(overallStart).Seconds(), largeUploadSecs, largeDownloadSecs, smallSecs, 512/smallSecs)
	t.Logf("RESOURCES test_server_peak_rss_bytes=%d max_cli_peak_rss_bytes=%d fixture_sampled_peak_allocated_bytes=%d fixture_sampled_peak_logical_bytes=%d fixture_peak_entries=%d min_tmp_free_bytes=%d pg_database_before_bytes=%d pg_database_after_bytes=%d interval_ms=100", rss.Maxrss*1024, clientRSS, peakAllocated, peakLogical, peakFiles, minFree, baselineDB, finalDB)
}

func TestComputerLargeByteResourceAcceptance(t *testing.T) {
	if os.Getenv("MEMOH_TEST_LARGE_TRANSFER") != "1" {
		t.Skip("opt-in actual-byte acceptance: requires at least 4 GiB + 64 MiB free temporary space")
	}
	ctx := t.Context()
	ownerPool := freshMigratedDB(t) // Creates and drops a separate synthetic database.
	config := ownerPool.Config().Copy()
	config.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := postgresstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner, user, bot, session, connection := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO users(id) VALUES($1),($2)", owner, user)
	exec("INSERT INTO team_members(user_id) VALUES($1),($2)", owner, user)
	exec("INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'delivery-fixture')", bot, owner)
	exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id) VALUES($1,$2,$3)", session, bot, user)
	exec(`INSERT INTO bot_user_grants(bot_id,subject_type,user_id,permissions) VALUES($1,'user',$2,'["chat","workspace_read","workspace_write","workspace_exec"]')`, bot, user)
	exec("INSERT INTO mcp_connections(id,bot_id,name,type) VALUES($1,$2,'delivery-fixture','http')", connection, bot)
	target := agenvo.Target{ConnectionID: connection, DeviceID: "synthetic-device", InstanceID: "synthetic-instance", Session: "fixture", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\config`}}
	raw, _ := json.Marshal(target)
	runtime, err := store.CreateAgenvoRuntime(ctx, owner, "delivery-fixture", raw)
	if err != nil {
		t.Fatal(err)
	}
	mount, err := store.CreateOrUpdateMount(ctx, bot, runtime.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := computertask.DeliveryIdentity{Binding: toolcontext.ComputerBinding{BotID: bot, UserID: user, SessionID: session, TargetID: mount.ID, RunID: "synthetic-run"}, OperationID: uuid.NewString(), DeviceID: target.DeviceID, InstanceID: target.InstanceID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	details, _ := json.Marshal(computertask.Details{Kind: "herdr_pi_task", Batch: &computertask.BatchDetails{Progress: agenvo.BatchProgress{Attempt: id.Attempt, Generation: id.Generation}}, Phase: "submitted"})
	_, claimed, err := store.ClaimComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: bot, UserID: user, SessionID: session, TargetID: mount.ID, RunID: id.Binding.RunID, Fingerprint: "synthetic-fingerprint", TargetFingerprint: agenvo.TargetFingerprint(target), Details: details})
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	ctx, err = toolcontext.WithComputerBinding(ctx, id.Binding)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = computertask.WithDeliveryIdentity(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the production adapter under a non-superuser, NOBYPASSRLS
	// fixture role. Keep the admin pool only for synthetic revoke operations.
	rls := rlsConn(t, pool, teamMigrationDSN(t))
	var role string
	if err := rls.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := pool.Config().Copy()
	runtimeConfig.ConnConfig.RuntimeParams["role"] = role
	runtimePool, err := pgxpool.NewWithConfig(ctx, runtimeConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtimePool.Close)
	store, err = postgresstore.New(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	exerciseLargeByteResources(ctx, t, store, pool, id)
}
