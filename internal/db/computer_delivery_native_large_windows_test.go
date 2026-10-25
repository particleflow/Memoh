//go:build integration && windows

package db_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/felinics/memoh/internal/computertask"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/transferclient"
)

const nativeLargeRevision = "native-large-fixture-v1"

// This is a fixture working-set allowance, not a product backlog limit or a
// change to the installed CLI configuration. Only one 1 GiB download is admitted.
var nativeLargeLimits = transferclient.QueueLimits{Jobs: 2, Bytes: 2 << 30, JournalBytes: 1 << 20, Workers: 1}

type nativeLargeInvocation struct {
	Action transferclient.ConfiguredAction
	Now    time.Time
}

type nativeLargeReply struct {
	Status transferclient.QueueStatus
	Error  string
}

// A fresh test process invokes the existing finite plugin boundary. RootCAs are
// injected only into this process's HTTP client: this is NOT formal EXE/system
// trust validation, and no certificate is installed on the runner.
func TestWindowsNativeLargeByteChild(t *testing.T) {
	if os.Getenv("MEMOH_NATIVE_LARGE_CHILD") != "1" {
		return
	}
	raw, err := os.ReadFile(os.Getenv("MEMOH_NATIVE_LARGE_CA")) //nolint:gosec // Only the parent test supplies this private synthetic CA path.
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		t.Fatal("fixture CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	var in nativeLargeInvocation
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 16384)).Decode(&in); err != nil {
		t.Fatal(err)
	}
	status, err := transferclient.InvokeConfiguredHerdr(t.Context(), os.Getenv, nativeLargeRevision, nativeLargeLimits, in.Action, &http.Client{Transport: transport}, in.Now)
	reply := nativeLargeReply{Status: status}
	if err != nil {
		reply.Error = err.Error()
	}
	if err := json.NewEncoder(os.Stdout).Encode(reply); err != nil {
		t.Fatal(err)
	}
}

// The wrapper pauses a real Media response after a measured prefix. It neither
// synthesizes headers/bodies nor buffers a file, and releases on client death.
type nativeLargePauseWriter struct {
	http.ResponseWriter
	ctx       context.Context
	remaining int64
	held      chan struct{}
	sent      *atomic.Int64
}

func (w *nativeLargePauseWriter) Write(p []byte) (int, error) {
	if w.remaining == 0 {
		close(w.held)
		<-w.ctx.Done()
		return 0, w.ctx.Err()
	}
	p = p[:min(int64(len(p)), w.remaining)]
	n, err := w.ResponseWriter.Write(p)
	w.sent.Add(int64(n))
	w.remaining -= int64(n)
	if err == nil {
		err = http.NewResponseController(w.ResponseWriter).Flush()
	}
	return n, err
}

type nativeLargeCountWriter struct {
	http.ResponseWriter
	sent *atomic.Int64
}

func (w nativeLargeCountWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.sent.Add(int64(n))
	return n, err
}

func nativeLargeFree(t *testing.T, root string) uint64 {
	t.Helper()
	ptr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &free, nil, nil); err != nil {
		t.Fatal(err)
	}
	return free
}

// FILE_STANDARD_INFO reports allocated bytes, unlike logical file length.
// https://learn.microsoft.com/windows/win32/api/winbase/ns-winbase-file_standard_info
func nativeLargeAllocation(t *testing.T, file *os.File) int64 {
	t.Helper()
	var info struct {
		Allocation, End          int64
		Links                    uint32
		DeletePending, Directory uint8
		Padding                  [2]byte
	}
	if err := windows.GetFileInformationByHandleEx(windows.Handle(file.Fd()), windows.FileStandardInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil { //nolint:gosec // Fixed FILE_STANDARD_INFO buffer passed to the read-only Windows API.
		t.Fatal(err)
	}
	var basic windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &basic); err != nil {
		t.Fatal(err)
	}
	if basic.FileAttributes&(windows.FILE_ATTRIBUTE_SPARSE_FILE|windows.FILE_ATTRIBUTE_COMPRESSED) != 0 || info.Allocation < info.End {
		t.Fatal("fixture is sparse/compressed or underallocated")
	}
	var fs [32]uint16
	if err := windows.GetVolumeInformationByHandle(windows.Handle(file.Fd()), nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))); err != nil || windows.UTF16ToString(fs[:]) != "NTFS" {
		t.Fatal("requires actual NTFS", err)
	}
	return info.Allocation
}

func TestWindowsNativeLargeByteRecovery(t *testing.T) {
	const size int64 = 1 << 30
	const interruptedAt int64 = 256 << 20
	started := time.Now()
	store, id, ctx := nativeDeliveryDatabase(t)
	mediaRoot, stateRoot, configRoot := t.TempDir(), nativeDeliveryPrivateRoot(t), nativeDeliveryPrivateRoot(t)
	free := nativeLargeFree(t, mediaRoot)
	if free < 4<<30 || nativeLargeFree(t, stateRoot) < 4<<30 {
		t.Fatalf("insufficient fixture disk: free=%d need=4294967296; no skip or sparse fallback", free)
	}
	t.Logf("preflight free_bytes=%d file_bytes=%d buffer_bytes=65536 filesystem=NTFS-required", free, size)

	source, err := os.CreateTemp(mediaRoot, "non-sparse-original-")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	for written := int64(0); written < size; written += int64(len(buffer)) {
		if _, err := rand.Read(buffer); err != nil {
			t.Fatal(err)
		}
		if _, err := source.Write(buffer); err != nil {
			t.Fatal(err)
		}
		_, _ = hash.Write(buffer)
	}
	if err := source.Sync(); err != nil {
		t.Fatal(err)
	}
	allocated := nativeLargeAllocation(t, source)
	contentHash := hex.EncodeToString(hash.Sum(nil))
	originalName := source.Name()
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(mediaRoot, id.Binding.BotID, contentHash[:2])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	originalNameFinal := filepath.Join(dir, contentHash+".bin")
	if err := os.Rename(originalName, originalNameFinal); err != nil { //nolint:gosec // Both names belong to this test directory; destination derives only from its generated hash.
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(originalNameFinal)
	if err != nil {
		t.Fatal(err)
	}

	assets := media.NewService(nil, localfs.NewOriginals(mediaRoot))
	assets.SetRequirePersistence(true)
	log := slog.New(slog.DiscardHandler)
	verifier := newRegisteredTransferVerifier(ctx, t, store, id)
	messages := handlers.NewMessageHandler(log, nil, nil, nil, nil)
	messages.SetMediaService(assets)
	handler, err := handlers.NewDeviceComputerTransferHandler(ctx, computertask.NewPostgresDeliveryService(store, assets), messages, verifier, log)
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	endpoint := server.NewServer(log, ":0", "synthetic-large-fixture", handler) //nolint:contextcheck // HTTP requests supply their own contexts.
	held := make(chan struct{})
	var gets, heads, commits, sent, expectedOffset, rangeMismatch atomic.Int64
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/commit") {
			commits.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/media/"+contentHash) {
			if r.Method == http.MethodHead {
				heads.Add(1)
			}
			if r.Method == http.MethodGet {
				n := gets.Add(1)
				want := ""
				if offset := expectedOffset.Load(); offset > 0 {
					want = "bytes=" + strconv.FormatInt(offset, 10) + "-"
				}
				if r.Header.Get("Range") != want {
					rangeMismatch.Add(1)
				}
				if n == 1 {
					endpoint.ServeHTTP(&nativeLargePauseWriter{ResponseWriter: w, ctx: r.Context(), remaining: interruptedAt, held: held, sent: &sent}, r)
				} else {
					endpoint.ServeHTTP(nativeLargeCountWriter{ResponseWriter: w, sent: &sent}, r)
				}
				return
			}
		}
		endpoint.ServeHTTP(w, r)
	}))
	defer https.Close()
	configJSON, _ := json.Marshal(map[string]any{"version": 1, "origin": https.URL, "source_root": ""})
	nativeDeliveryPrivateFile(t, filepath.Join(configRoot, "transfer.json"), configJSON)
	nativeDeliveryPrivateFile(t, filepath.Join(configRoot, "device.credential"), []byte(verifier.token))
	caPath := filepath.Join(configRoot, "fixture-ca.pem")
	nativeDeliveryPrivateFile(t, caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: https.Certificate().Raw}))
	job := transferclient.Job{Version: 1, DeliveryID: "native-large-download", SnapshotID: "native-large-download", BotID: id.Binding.BotID, OperationID: id.OperationID, LocatorRevision: nativeLargeRevision, Phase: "queued", Task: pgDeviceTask(id), Spec: transferclient.Spec{SizeBytes: size, SHA256: contentHash, Mime: "application/octet-stream"}}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := func(kind string, status transferclient.QueueStatus) *exec.Cmd {
		now := time.Now()
		if status.Job.NextAttempt.After(now) {
			now = status.Job.NextAttempt
		}
		if status.AssociationDue.After(now) {
			now = status.AssociationDue
		}
		input, _ := json.Marshal(nativeLargeInvocation{Action: transferclient.ConfiguredAction{Kind: kind, DeliveryID: job.DeliveryID, Job: job}, Now: now})
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestWindowsNativeLargeByteChild$", "-test.timeout=150s")
		cmd.Env = append(os.Environ(), "MEMOH_NATIVE_LARGE_CHILD=1", "MEMOH_NATIVE_LARGE_CA="+caPath, "HERDR_PLUGIN_CONFIG_DIR="+configRoot, "HERDR_PLUGIN_STATE_DIR="+stateRoot)
		cmd.Stdin = bytes.NewReader(input)
		return cmd
	}
	invoke := func(kind string, status transferclient.QueueStatus) transferclient.QueueStatus {
		t.Helper()
		output, err := command(kind, status).CombinedOutput()
		if err != nil {
			t.Fatalf("finite child %s: %v %s", kind, err, output)
		}
		var reply nativeLargeReply
		if err := json.NewDecoder(bytes.NewReader(output)).Decode(&reply); err != nil || reply.Error != "" {
			t.Fatalf("finite child %s: decode=%v error=%s output=%s", kind, err, reply.Error, output)
		}
		return reply.Status
	}
	status := invoke("enqueue-download", transferclient.QueueStatus{})
	initial := status.Job
	child := command("advance", status)
	var childOutput bytes.Buffer
	child.Stdout, child.Stderr = &childOutput, &childOutput
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	select {
	case <-held:
	case <-time.After(110 * time.Second):
		t.Fatal("did not reach bounded interruption prefix")
	}
	// Give the existing checkpoint writer time to sync. We assert the actual
	// persisted offset after death, never assume the transmitted offset is durable.
	time.Sleep(500 * time.Millisecond)
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("expected killed child")
	}
	waited = true
	status = invoke("inspect", status)
	checkpoint := status.Job.Offset
	if checkpoint < 128<<20 || checkpoint > interruptedAt || status.Associated || status.Job.Phase == "ready" {
		t.Fatalf("bad interrupted checkpoint: %+v", status)
	}
	if status.Job.ReceiptID != initial.ReceiptID || status.Job.Task != initial.Task || status.Job.HostBinding != initial.HostBinding {
		t.Fatal("interruption changed immutable binding")
	}
	ref := transferclient.Reference{DeliveryID: job.DeliveryID, OperationID: job.OperationID, BotID: job.BotID, ContentHash: contentHash, SizeBytes: size, Mime: job.Spec.Mime}
	openPublication := func() (*transferclient.DiskQueue, *transferclient.NativePublication) {
		q, err := transferclient.OpenHerdrQueue(func(string) string { return stateRoot }, nativeLargeRevision, nativeLargeLimits)
		if err != nil {
			t.Fatal(err)
		}
		p, err := q.OpenNativePublication(func(c context.Context, r transferclient.Reference) error {
			if r != ref {
				return transferclient.ErrAuthorization
			}
			return verifier.RevalidateDevice(c, verifier.device)
		})
		if err != nil {
			_ = q.Close()
			t.Fatal(err)
		}
		return q, p
	}
	q, p := openPublication()
	outcome, err := p.Resolve(ctx, ref, false)
	_ = p.Close()
	_ = q.Close()
	if err != nil || outcome != transferclient.OutcomeAbsent {
		t.Fatal("partial bytes published", outcome, err)
	}
	t.Logf("interrupted sent_bytes=%d durable_offset=%d original_delivery=%s attempt=%s", sent.Load(), checkpoint, job.DeliveryID, job.Task.Attempt)
	for i := 0; i < 80 && !status.Associated; i++ {
		if nativeLargeFree(t, stateRoot) < 1<<30 {
			t.Fatal("fixture free space reserve reached; retained progress, no further allocation")
		}
		expectedOffset.Store(status.Job.Offset)
		status = invoke("advance", status)
		if status.Job.Phase == "blocked" || status.AssociationBlocked {
			t.Fatalf("blocked actual transfer: %+v", status)
		}
		if !status.Associated {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if !status.Associated || status.Job.Offset != size || status.Job.Task != initial.Task || status.Job.ReceiptID != initial.ReceiptID || status.Job.HostBinding != initial.HostBinding {
		t.Fatalf("did not complete original delivery: %+v", status)
	}
	if gets.Load() != 2 || rangeMismatch.Load() != 0 || sent.Load() != interruptedAt+size-checkpoint {
		t.Fatalf("Range/replay mismatch gets=%d range_errors=%d sent=%d checkpoint=%d", gets.Load(), rangeMismatch.Load(), sent.Load(), checkpoint)
	}
	q, p = openPublication()
	reader, err := p.OpenPublished(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	hash.Reset()
	n, err := io.CopyBuffer(hash, reader, buffer)
	object, ok := reader.(*os.File)
	if !ok {
		t.Fatal("native object handle unavailable")
	}
	publishedAllocated := nativeLargeAllocation(t, object)
	_ = reader.Close()
	_ = p.Close()
	_ = q.Close()
	if err != nil || n != size || hex.EncodeToString(hash.Sum(nil)) != contentHash {
		t.Fatal("full published hash/length mismatch", n, err)
	}
	original, err := os.Open(originalNameFinal) //nolint:gosec // Read back only the generated fixture object, never a user-provided path.
	if err != nil {
		t.Fatal(err)
	}
	hash.Reset()
	n, err = io.CopyBuffer(hash, original, buffer)
	afterInfo, statErr := original.Stat()
	_ = original.Close()
	if err != nil || statErr != nil || n != size || hex.EncodeToString(hash.Sum(nil)) != contentHash || !os.SameFile(originalInfo, afterInfo) {
		t.Fatal("original modified or replaced", err, statErr)
	}
	op := dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID}
	current, err := store.ReadComputerOperation(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := store.ReadComputerDeliveryEntry(ctx, current, job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Outcome     string                       `json:"outcome"`
		Reference   computertask.DeliveryRequest `json:"reference"`
		Publication string                       `json:"publication"`
	}
	if json.Unmarshal(entry.Data, &proof) != nil || proof.Outcome != "committed" || proof.Reference.ContentHash != contentHash || proof.Reference.SizeBytes != size {
		t.Fatalf("durable proof mismatch: %+v", proof)
	}
	beforeGets, beforeCommits := gets.Load(), commits.Load()
	for range 2 {
		reopened := invoke("enqueue-download", status)
		if !reopened.Associated || reopened.Job.DeliveryID != status.Job.DeliveryID || reopened.Job.Task != status.Job.Task || reopened.Job.Spec != status.Job.Spec || reopened.Job.ReceiptID != status.Job.ReceiptID {
			t.Fatal("completed process reopen changed identity", reopened)
		}
	}
	currentAfter, err := store.ReadComputerOperation(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	entryAfter, err := store.ReadComputerDeliveryEntry(ctx, currentAfter, job.DeliveryID)
	if err != nil || !bytes.Equal(entry.Data, entryAfter.Data) || gets.Load() != beforeGets || commits.Load() != beforeCommits {
		t.Fatal("restart repeated transfer or registration", err)
	}
	t.Logf("verified bytes=%d source_allocated=%d published_allocated=%d free_after=%d GET=%d HEAD=%d Commit_requests=%d confirmed_prefix_retransmitted=0 unconfirmed_tail_replayed=%d ledger=committed stable_reopen=2 sha256=%s elapsed=%s", size, allocated, publishedAllocated, nativeLargeFree(t, stateRoot), gets.Load(), heads.Load(), commits.Load(), sent.Load()-size, contentHash, time.Since(started).Round(time.Millisecond))
	t.Log("scope=Windows-test-process/HTTPS/real-Server/PG/NTFS direction=download fixture_CA_only=true physical_power_loss=false upload_receipt=not-applicable")
}
