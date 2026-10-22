package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func TestImageTaskClaimSurvivesReopenAndConcurrentSubmission(t *testing.T) {
	root := t.TempDir()
	j, err := OpenImageTaskJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Close() }()
	j2, err := OpenImageTaskJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j2.Close() }()
	fp := sha256.Sum256([]byte("synthetic request"))
	r := ImageTaskReceipt{ID: "one", Scope: "trusted-session", Fingerprint: hex.EncodeToString(fp[:])}
	var created atomic.Int32
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			journal := j
			if i%2 == 1 {
				journal = j2
			}
			_, won, err := journal.Claim(context.Background(), r)
			if err != nil && !errors.Is(err, ErrUploadBusy) {
				t.Error(err)
			}
			if won && err == nil {
				created.Add(1)
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("claimed=%d", created.Load())
	}
	got, won, err := j2.Claim(context.Background(), r)
	if err != nil || won || got.State != "dispatching" {
		t.Fatalf("read=%+v won=%v err=%v", got, won, err)
	}
	r.ID = "two"
	blocked, won, err := j2.Claim(context.Background(), r)
	if !errors.Is(err, ErrUploadBusy) || won || blocked.ID != "one" {
		t.Fatalf("blocked=%+v won=%v err=%v", blocked, won, err)
	}
	if _, err := j2.Read(context.Background(), "other-session", "one"); !errors.Is(err, ErrUploadNotFound) {
		t.Fatal("cross-scope read accepted")
	}
}

func TestImageTaskClaimFsyncFailureDoesNotAuthorizeDispatch(t *testing.T) {
	j, err := OpenImageTaskJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Close() }()
	fp := sha256.Sum256([]byte("synthetic request"))
	r := ImageTaskReceipt{ID: "one", Scope: "session", Fingerprint: hex.EncodeToString(fp[:])}
	j.files.syncFile = func(f *os.File) error {
		info, e := f.Stat()
		if e != nil {
			return e
		}
		if info.IsDir() {
			return errors.New("synthetic directory sync failure")
		}
		return f.Sync()
	}
	_, _, err = j.Claim(context.Background(), r)
	if err == nil {
		t.Fatal("claim acknowledged without durable directory entry")
	}
	j.files.syncFile = (*os.File).Sync
	got, created, err := j.Claim(context.Background(), r)
	if err != nil || created || got.State != "dispatching" {
		t.Fatalf("visible claim must recover as uncertain: %+v %v %v", got, created, err)
	}
}

func TestGeneratedResultStagingSurvivesUploadTTL(t *testing.T) {
	root := t.TempDir()
	m := NewService(nil, localfs.NewOriginals(root))
	m.SetRequirePersistence(true)
	if err := m.EnableUploads(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Uploads().Close() }()
	data := []byte("synthetic provider result")
	hash := sha256.Sum256(data)
	u, err := m.Uploads().CreateResult(context.Background(), "owner", "bot", uuid.NewString(), UploadInput{SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), Mime: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Uploads().Append(context.Background(), "owner", "bot", u.ID, 0, int64(len(data)), hex.EncodeToString(hash[:]), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * UploadTTL)
	m.Uploads().now = func() time.Time { return future }
	if err := m.Uploads().Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	ready, err := m.Uploads().Complete(context.Background(), "owner", "bot", u.ID)
	if err != nil || ready.ContentHash != hex.EncodeToString(hash[:]) {
		t.Fatalf("result=%+v err=%v", ready, err)
	}
	if err := m.Uploads().ReleaseResult(context.Background(), "owner", "bot", u.ID); err != nil {
		t.Fatal(err)
	}
	future = future.Add(2 * UploadTTL)
	if err := m.Uploads().Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Uploads().Get(context.Background(), "owner", "bot", u.ID); !errors.Is(err, ErrUploadNotFound) {
		t.Fatal("released receipt should expire normally")
	}
	r, _, err := m.Open(context.Background(), "bot", ready.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
}
