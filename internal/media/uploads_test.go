package media

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func uploadService(t *testing.T, root string, limit int64) *Service {
	t.Helper()
	s := NewService(nil, localfs.NewOriginals(root))
	s.SetRequirePersistence(true)
	s.SetMaxUploadBytes(limit)
	if err := s.EnableUploads(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Uploads().Close() })
	return s
}

func createUpload(ctx context.Context, t *testing.T, s *UploadStore, b []byte) Upload {
	t.Helper()
	u, err := s.Create(ctx, "owner", "bot", UploadInput{int64(len(b)), digest(b), "application/octet-stream"})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func appendBytes(ctx context.Context, t *testing.T, s *UploadStore, u Upload, b []byte) Upload {
	t.Helper()
	v, err := s.Append(ctx, "owner", "bot", u.ID, u.Offset, int64(len(b)), digest(b), bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestUploadsIntegrityIsolationAndRecovery(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	svc := uploadService(t, root, 1024)
	s := svc.Uploads()
	b := []byte("synthetic original")
	u := createUpload(ctx, t, s, b)
	if u.ContentHash != "" {
		t.Fatal("premature content reference")
	}
	if _, err := s.Complete(ctx, "owner", "bot", u.ID); !errors.Is(err, ErrUploadConflict) {
		t.Fatal(err)
	}
	for _, identity := range []struct{ owner, bot string }{{"other", "bot"}, {"owner", "other"}} {
		if _, err := s.Get(ctx, identity.owner, identity.bot, u.ID); !errors.Is(err, ErrUploadNotFound) {
			t.Fatal(err)
		}
		if _, err := s.Append(ctx, identity.owner, identity.bot, u.ID, 0, 1, digest(b[:1]), bytes.NewReader(b[:1])); !errors.Is(err, ErrUploadNotFound) {
			t.Fatal(err)
		}
		if _, err := s.Complete(ctx, identity.owner, identity.bot, u.ID); !errors.Is(err, ErrUploadNotFound) {
			t.Fatal(err)
		}
		if _, err := s.Cancel(ctx, identity.owner, identity.bot, u.ID); !errors.Is(err, ErrUploadNotFound) {
			t.Fatal(err)
		}
	}
	for _, bad := range []struct {
		body []byte
		size int64
		hash string
	}{{b, int64(len(b)), digest([]byte("wrong"))}, {b[:3], int64(len(b)), digest(b)}, {append(b, 'x'), int64(len(b)), digest(b)}} {
		if _, err := s.Append(ctx, "owner", "bot", u.ID, 0, bad.size, bad.hash, bytes.NewReader(bad.body)); !errors.Is(err, ErrUploadInvalid) {
			t.Fatal(err)
		}
		status, err := s.Get(ctx, "owner", "bot", u.ID)
		if err != nil || status.Offset != 0 {
			t.Fatalf("bad chunk advanced offset: %+v %v", status, err)
		}
	}
	u = appendBytes(ctx, t, s, u, b[:5])
	s2 := uploadService(t, root, 1024).Uploads()
	status, err := s2.Get(ctx, "owner", "bot", u.ID)
	if err != nil || status.Offset != 5 {
		t.Fatal(status, err)
	}
	if _, err := s2.Append(ctx, "owner", "bot", u.ID, 0, 5, digest(b[:5]), bytes.NewReader(b[:5])); !errors.Is(err, ErrUploadConflict) {
		t.Fatal(err)
	}
	u = appendBytes(ctx, t, s2, u, b[5:])
	ready, err := s2.Complete(ctx, "owner", "bot", u.ID)
	if err != nil || ready.State != "ready" || ready.ContentHash != digest(b) {
		t.Fatal(ready, err)
	}
	again, err := s.Complete(ctx, "owner", "bot", u.ID)
	if err != nil || again != ready {
		t.Fatal(again, err)
	}
	if _, err := s.Cancel(ctx, "owner", "bot", u.ID); !errors.Is(err, ErrUploadConflict) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".uploads", u.ID+".data")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	// A duplicate pending upload must never delete the shared ready original.
	duplicate := createUpload(ctx, t, s, b)
	appendBytes(ctx, t, s, duplicate, b)
	if _, err := s.Cancel(ctx, "owner", "bot", duplicate.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cancel(ctx, "owner", "bot", duplicate.ID); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(UploadTTL + time.Second) }
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	reader, _, err := svc.Open(ctx, "bot", digest(b))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, b) {
		t.Fatal("original changed")
	}
}

func TestUploadsFinalHashAndPublicationFailure(t *testing.T) {
	ctx := context.Background()
	s := uploadService(t, t.TempDir(), 1024)
	u := createUpload(ctx, t, s.Uploads(), []byte("good"))
	appendBytes(ctx, t, s.Uploads(), u, []byte("evil"))
	if _, err := s.Uploads().Complete(ctx, "owner", "bot", u.ID); !errors.Is(err, ErrUploadInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, "bot", digest([]byte("evil"))); !errors.Is(err, ErrAssetNotFound) {
		t.Fatal(err)
	}
	b := []byte("fault recovery")
	u = createUpload(ctx, t, s.Uploads(), b)
	appendBytes(ctx, t, s.Uploads(), u, b)
	original := s.provider
	s.provider = &failedPublication{Provider: original}
	if _, err := s.Uploads().Complete(ctx, "owner", "bot", u.ID); err == nil {
		t.Fatal("expected publication fault")
	}
	status, _ := s.Uploads().Get(ctx, "owner", "bot", u.ID)
	if status.State != "uploading" || status.ContentHash != "" {
		t.Fatal(status)
	}
	s.provider = original
	if _, err := s.Uploads().Complete(ctx, "owner", "bot", u.ID); err != nil {
		t.Fatal(err)
	}
}

type failedPublication struct{ storage.Provider }

func (p *failedPublication) Put(ctx context.Context, key string, r io.Reader) error {
	if err := p.Provider.Put(ctx, key, r); err != nil {
		return err
	}
	return errors.New("synthetic lost publication acknowledgement")
}

func TestUploadsConcurrentWritersAndQuotas(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := uploadService(t, root, 1024).Uploads()
	s2 := uploadService(t, root, 1024).Uploads()
	b := []byte("parallel")
	u := createUpload(ctx, t, s, b)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, store := range []*UploadStore{s, s2} {
		wg.Add(1)
		go func(store *UploadStore) {
			defer wg.Done()
			_, err := store.Append(ctx, "owner", "bot", u.ID, 0, int64(len(b)), digest(b), bytes.NewReader(b))
			results <- err
		}(store)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrUploadBusy) && !errors.Is(err, ErrUploadConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	for i := 0; i < 3; i++ {
		createUpload(ctx, t, s, b)
	}
	if _, err := s.Create(ctx, "owner", "bot", UploadInput{8, digest(b), "application/octet-stream"}); !errors.Is(err, ErrUploadQuota) {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "other", "bot", UploadInput{1025, digest(b), "application/octet-stream"}); !errors.Is(err, ErrAssetTooLarge) {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "owner", "bot", u.ID, 0, UploadChunkBytes+1, digest(b), bytes.NewReader(b)); !errors.Is(err, ErrUploadInvalid) {
		t.Fatal(err)
	}
}

func TestUploadsPathsAndLinks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := uploadService(t, root, 1024).Uploads()
	for _, id := range []string{"../escape", "/etc/passwd", "a\\b", "..", "%2e%2e"} {
		if _, err := s.Get(ctx, "owner", "bot", id); !errors.Is(err, ErrUploadNotFound) {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"symbolic", "hard"} {
		t.Run(link, func(t *testing.T) {
			u := createUpload(ctx, t, s, []byte("abc"))
			target := filepath.Join(root, ".uploads", u.ID+".data")
			var err error
			if link == "symbolic" {
				err = os.Symlink(outside, target)
			} else {
				err = os.Link(outside, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Append(ctx, "owner", "bot", u.ID, 0, 3, digest([]byte("abc")), strings.NewReader("abc")); err == nil {
				t.Fatal("followed link")
			}
			contents, _ := os.ReadFile(outside) //nolint:gosec // G304: synthetic path under t.TempDir, not external input.
			if string(contents) != "private" {
				t.Fatal("changed outside file")
			}
			if _, err := s.Cancel(ctx, "owner", "bot", u.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
	root2 := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root2, ".uploads")); err != nil {
		t.Fatal(err)
	}
	if err := NewService(nil, nil).EnableUploads(root2); err == nil {
		t.Fatal("accepted staging directory link")
	}
}

type endlessZeros struct{}

func (endlessZeros) Read(b []byte) (int, error) { clear(b); return len(b), nil }
func TestUploadsLarge512MiB(t *testing.T) {
	if testing.Short() {
		t.Skip("512 MiB synthetic resource test")
	}
	const size int64 = 512 << 20
	root := t.TempDir()
	s := uploadService(t, root, size)
	ctx := context.Background()
	start := time.Now()
	h := sha256.New()
	_, _ = io.CopyN(h, endlessZeros{}, size)
	hash := hex.EncodeToString(h.Sum(nil))
	u, err := s.Uploads().Create(ctx, "owner", "bot", UploadInput{size, hash, "application/octet-stream"})
	if err != nil {
		t.Fatal(err)
	}
	chunkHash := sha256.New()
	_, _ = io.CopyN(chunkHash, endlessZeros{}, UploadChunkBytes)
	partHash := hex.EncodeToString(chunkHash.Sum(nil))
	var peak uint64
	for offset := int64(0); offset < size; offset += UploadChunkBytes {
		u, err = s.Uploads().Append(ctx, "owner", "bot", u.ID, offset, UploadChunkBytes, partHash, io.LimitReader(endlessZeros{}, UploadChunkBytes))
		if err != nil {
			t.Fatal(err)
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if m.HeapAlloc > peak {
			peak = m.HeapAlloc
		}
	}
	ready, err := s.Uploads().Complete(ctx, "owner", "bot", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := s.Open(ctx, "bot", ready.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	h.Reset()
	n, err := io.Copy(h, reader)
	_ = reader.Close()
	if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != hash {
		t.Fatal(n, err)
	}
	if peak > 64<<20 {
		t.Fatalf("heap exceeded bounded budget: %d", peak)
	}
	t.Logf("synthetic_bytes=%d chunks=64 elapsed=%s sampled_peak_heap_bytes=%d sha256=%s", size, time.Since(start), peak, hash)
}

// The child acknowledges one chunk, stalls midway through the next, and is
// killed by the parent. No in-memory object survives the recovery boundary.
func TestUploadsProcessRestart(t *testing.T) {
	ctx := t.Context()
	if root := os.Getenv("MEMOH_UPLOAD_TEST_CHILD"); root != "" {
		s := uploadService(t, root, 1024)
		b := []byte("12345678")
		u := createUpload(ctx, t, s.Uploads(), b)
		u = appendBytes(ctx, t, s.Uploads(), u, b[:4])
		_, _ = s.Uploads().Append(context.Background(), "owner", "bot", u.ID, 4, 4, digest(b[4:]), &stallUploadReader{id: u.ID})
		return
	}
	root := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUploadsProcessRestart$") //nolint:gosec // G204: this test binary and a fixed test selector.
	cmd.Env = append(os.Environ(), "MEMOH_UPLOAD_TEST_CHILD="+root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	var id string
	select {
	case id = <-lines:
	case <-time.After(15 * time.Second):
		t.Fatal("child stalled before marker")
	}
	if !validUploadID(id) {
		t.Fatal(id)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	s := uploadService(t, root, 1024).Uploads()
	u, err := s.Get(context.Background(), "owner", "bot", id)
	if err != nil || u.Offset != 4 {
		t.Fatal(u, err)
	}
	appendBytes(ctx, t, s, u, []byte("5678"))
	if _, err := s.Complete(context.Background(), "owner", "bot", id); err != nil {
		t.Fatal(err)
	}
}

type stallUploadReader struct {
	id   string
	sent bool
}

func (r *stallUploadReader) Read(b []byte) (int, error) {
	if !r.sent {
		r.sent = true
		copy(b, "56")
		return 2, nil
	}
	_, _ = os.Stdout.WriteString(r.id + "\n")
	time.Sleep(time.Minute)
	return 0, io.EOF
}

func TestUploadsJournalFailureAndCleanup(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	svc := uploadService(t, root, 1024)
	s := svc.Uploads()
	b := []byte("journal repair")
	u := createUpload(ctx, t, s, b)
	// A filesystem error at the checkpoint must not acknowledge bytes. Remove
	// the synthetic obstruction, reopen the service and resend from GET offset.
	obstruction := filepath.Join(root, ".uploads", u.ID+".tmp")
	if err := os.Mkdir(obstruction, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(obstruction, "occupied"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "owner", "bot", u.ID, 0, int64(len(b)), digest(b), bytes.NewReader(b)); err == nil {
		t.Fatal("expected checkpoint I/O failure")
	}
	status, err := s.Get(ctx, "owner", "bot", u.ID)
	if err != nil || status.Offset != 0 {
		t.Fatal(status, err)
	}
	if err := os.RemoveAll(obstruction); err != nil {
		t.Fatal(err)
	}
	s2 := uploadService(t, root, 1024).Uploads()
	appendBytes(ctx, t, s2, status, b)
	if _, err := s2.Complete(ctx, "owner", "bot", u.ID); err != nil {
		t.Fatal(err)
	}
	// A process can die before the first journal rename. Startup collection
	// removes only the recognized private temporary name, not arbitrary files.
	orphan := filepath.Join(root, ".uploads", "a83959b2-5a5b-4d50-86bc-a9587acdd410.tmp")
	if err := os.WriteFile(orphan, []byte("incomplete json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	pending := createUpload(ctx, t, s, []byte("expire"))
	appendBytes(ctx, t, s, pending, []byte("exp"))
	s.now = func() time.Time { return time.Now().Add(UploadTTL + time.Second) }
	if _, err := s.Get(ctx, "owner", "bot", pending.ID); !errors.Is(err, ErrUploadNotFound) {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".uploads", pending.ID+".data")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if reader, _, err := svc.Open(ctx, "bot", digest(b)); err != nil {
		t.Fatal(err)
	} else {
		_ = reader.Close()
	}
}

func TestUploadsConcurrentCompletionAndCancel(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := uploadService(t, root, 1024).Uploads()
	s2 := uploadService(t, root, 1024).Uploads()
	b := []byte("shared")
	u := createUpload(ctx, t, s, b)
	appendBytes(ctx, t, s, u, b)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; _, err := s.Complete(ctx, "owner", "bot", u.ID); results <- err }()
	go func() { <-start; _, err := s2.Cancel(ctx, "owner", "bot", u.ID); results <- err }()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil && !errors.Is(err, ErrUploadBusy) && !errors.Is(err, ErrUploadConflict) {
			t.Fatal(err)
		}
	}
	status, err := s.Get(ctx, "owner", "bot", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.State == "ready" {
		if _, err := s2.Complete(ctx, "owner", "bot", u.ID); err != nil {
			t.Fatal(err)
		}
	} else if status.State != "canceled" {
		t.Fatal(status)
	}
}

// Inject exactly after a journal rename, not before it. The real rename is
// observable; every subsequent reader must repair the failed directory fsync.
func failJournalDirectorySync(s *UploadStore) error {
	failure := errors.New("injected journal directory fsync failure")
	armed := false
	s.syncFile = func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if strings.HasSuffix(f.Name(), ".tmp") {
			armed = true
		}
		if info.IsDir() && armed {
			return failure
		}
		return f.Sync()
	}
	return failure
}

func TestUploadJournalRenameFailureRequiresReadBarrier(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	s := uploadService(t, root, 1024).Uploads()
	b := []byte("offset durability")
	u := createUpload(ctx, t, s, b)
	failure := failJournalDirectorySync(s)
	if _, err := s.Append(ctx, "owner", "bot", u.ID, 0, int64(len(b)), digest(b), bytes.NewReader(b)); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	// A visible checkpoint alone is not an acknowledged durable offset.
	if got, err := s.Get(ctx, "owner", "bot", u.ID); !errors.Is(err, failure) || got.ID != "" {
		t.Fatal(got, err)
	}
	restarted := uploadService(t, root, 1024).Uploads()
	barriers := 0
	restarted.syncFile = func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() {
			barriers++
		}
		return f.Sync()
	}
	status, err := restarted.Get(ctx, "owner", "bot", u.ID)
	if err != nil || status.Offset != int64(len(b)) || barriers == 0 {
		t.Fatal(status, err, barriers)
	}
	if _, err := restarted.Complete(ctx, "owner", "bot", u.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUploadTerminalJournalSyncBeforeRetryOrCollection(t *testing.T) {
	for _, state := range []string{"ready", "canceled"} {
		t.Run(state, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			s := uploadService(t, root, 1024).Uploads()
			b := []byte("retain until terminal checkpoint is durable")
			u := createUpload(ctx, t, s, b)
			appendBytes(ctx, t, s, u, b)
			failure := failJournalDirectorySync(s)
			var err error
			if state == "ready" {
				_, err = s.Complete(ctx, "owner", "bot", u.ID)
			} else {
				_, err = s.Cancel(ctx, "owner", "bot", u.ID)
			}
			if !errors.Is(err, failure) {
				t.Fatal(err)
			}
			for _, operation := range []func() error{
				func() error { _, err := s.Get(ctx, "owner", "bot", u.ID); return err },
				func() error { _, err := s.Complete(ctx, "owner", "bot", u.ID); return err },
				func() error { return s.Sweep(ctx) },
			} {
				if err := operation(); !errors.Is(err, failure) {
					t.Fatal(err)
				}
			}
			staging := filepath.Join(root, ".uploads", u.ID+".data")
			if _, err := os.Stat(staging); err != nil {
				t.Fatal("terminal journal not durable but staging deleted", err)
			}
			restarted := uploadService(t, root, 1024).Uploads()
			barriers := 0
			restarted.syncFile = func(f *os.File) error {
				info, err := f.Stat()
				if err != nil {
					return err
				}
				if info.IsDir() {
					barriers++
					if barriers == 1 {
						if _, err := os.Stat(staging); err != nil {
							t.Fatal("collection removed payload before recovery barrier", err)
						}
					}
				}
				return f.Sync()
			}
			if err := restarted.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
			if barriers == 0 {
				t.Fatal("missing recovery barrier")
			}
			if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			got, err := restarted.Get(ctx, "owner", "bot", u.ID)
			if err != nil || got.State != state {
				t.Fatal(got, err)
			}
		})
	}
}

func TestUploadProcessDeathAfterJournalRename(t *testing.T) {
	ctx := t.Context()
	if root := os.Getenv("MEMOH_UPLOAD_RENAME_CHILD"); root != "" {
		s := uploadService(t, root, 1024).Uploads()
		b := []byte("crash after rename")
		u := createUpload(ctx, t, s, b)
		armed := false
		s.syncFile = func(f *os.File) error {
			info, err := f.Stat()
			if err != nil {
				return err
			}
			if strings.HasSuffix(f.Name(), ".tmp") {
				armed = true
			}
			if info.IsDir() && armed {
				_, _ = os.Stdout.WriteString(u.ID + "\n")
				os.Exit(42)
			}
			return f.Sync()
		}
		appendBytes(ctx, t, s, u, b)
		t.Fatal("did not reach crash boundary")
	}
	root := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUploadProcessDeathAfterJournalRename$") //nolint:gosec // G204: fixed test binary invocation.
	cmd.Env = append(os.Environ(), "MEMOH_UPLOAD_RENAME_CHILD="+root)
	output, err := cmd.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 42 {
		t.Fatal(string(output), err)
	}
	id := strings.TrimSpace(string(output))
	if !validUploadID(id) {
		t.Fatal(id)
	}
	s := uploadService(t, root, 1024).Uploads()
	barriers := 0
	s.syncFile = func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() {
			barriers++
		}
		return f.Sync()
	}
	status, err := s.Get(ctx, "owner", "bot", id)
	if err != nil || status.Offset != int64(len("crash after rename")) || barriers == 0 {
		t.Fatal(status, err, barriers)
	}
	if _, err := s.Complete(ctx, "owner", "bot", id); err != nil {
		t.Fatal(err)
	}
}

type publicationWithoutBarrier struct {
	storage.Provider
	puts int
}

func (p *publicationWithoutBarrier) Put(ctx context.Context, key string, reader io.Reader) error {
	p.puts++
	return p.Provider.Put(ctx, key, reader)
}

func TestRequiredPersistenceRepublishesWithoutBarrierCapability(t *testing.T) {
	p := &publicationWithoutBarrier{Provider: localfs.NewOriginals(t.TempDir())}
	s := NewService(nil, p)
	s.SetRequirePersistence(true)
	for i := 0; i < 2; i++ {
		if _, err := s.Ingest(t.Context(), IngestInput{BotID: "bot", Mime: "text/plain", Reader: strings.NewReader("transport-backed durable original")}); err != nil {
			t.Fatal(err)
		}
	}
	if p.puts != 2 {
		t.Fatalf("readability was treated as durability without a barrier: puts=%d", p.puts)
	}
}
