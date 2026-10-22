//go:build pr8contract

package transferclient

// Run only in an isolated checkout of PR #8 at the pinned SHA, with this
// package overlaid. These tests use the real media store/provider, but a tiny
// synthetic TLS routing/auth fixture, not production JWT, handlers or a NAS.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func TestFrozenPR8RealStoreRestartAndBidirectionalTransfer(t *testing.T) {
	const bot = "11111111-1111-4111-8111-111111111111"
	const owner = "22222222-2222-4222-8222-222222222222"
	root := t.TempDir()
	var mu sync.Mutex
	var svc *media.Service
	start := func() {
		svc = media.NewService(nil, localfs.NewOriginals(root))
		svc.SetRequirePersistence(true)
		if err := svc.EnableUploads(root); err != nil {
			t.Fatal(err)
		}
	}
	start()
	t.Cleanup(func() { _ = svc.Uploads().Close() })
	drop := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != syntheticAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		prefix := "/bots/" + bot + "/media"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		suffix := strings.TrimPrefix(r.URL.Path, prefix)
		if suffix == "/limits" {
			_ = json.NewEncoder(w).Encode(Limits{MaxUploadBytes: svc.MaxUploadBytes(), ChunkMaxBytes: media.UploadChunkBytes, ResumableEnabled: true})
			return
		}
		if !strings.HasPrefix(suffix, "/uploads") {
			reader, asset, err := svc.OpenSeekable(r.Context(), bot, strings.TrimPrefix(suffix, "/"))
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			defer func() { _ = reader.Close() }()
			w.Header().Set("ETag", `"`+asset.ContentHash+`"`)
			http.ServeContent(w, r, "", time.Time{}, reader)
			return
		}
		id := strings.TrimPrefix(suffix, "/uploads/")
		var receipt media.Upload
		var err error
		status := 200
		switch {
		case suffix == "/uploads" && r.Method == http.MethodPost:
			var spec media.UploadInput
			if json.NewDecoder(r.Body).Decode(&spec) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			receipt, err = svc.Uploads().Create(r.Context(), owner, bot, spec)
			status = 201
		case r.Method == http.MethodGet:
			receipt, err = svc.Uploads().Get(r.Context(), owner, bot, id)
		case r.Method == http.MethodPatch:
			offset, _ := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
			length, _ := strconv.ParseInt(r.Header.Get("Upload-Length"), 10, 64)
			receipt, err = svc.Uploads().Append(r.Context(), owner, bot, id, offset, length, r.Header.Get("Upload-SHA256"), r.Body)
		case r.Method == http.MethodPost && strings.HasSuffix(id, "/complete"):
			receipt, err = svc.Uploads().Complete(r.Context(), owner, bot, strings.TrimSuffix(id, "/complete"))
		case r.Method == http.MethodDelete:
			receipt, err = svc.Uploads().Cancel(r.Context(), owner, bot, id)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err != nil {
			switch {
			case errors.Is(err, media.ErrUploadNotFound):
				status = 404
			case errors.Is(err, media.ErrUploadConflict):
				status = 409
			case errors.Is(err, media.ErrUploadQuota), errors.Is(err, media.ErrUploadBusy):
				status = 429
			case errors.Is(err, media.ErrUploadInvalid):
				status = 400
			default:
				status = 500
			}
			w.WriteHeader(status)
			return
		}
		if drop == r.Method {
			drop = ""
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(receipt)
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	src := frozen(bytes.Repeat([]byte{0xff, 0, 0x80, 42, 7}, 250000))
	job := newJob(src)
	job.BotID, job.ChunkBytes = bot, 1<<20
	store := storeJob(t, job)
	advance(t, client, store, src)
	mu.Lock()
	drop = "PATCH"
	mu.Unlock()
	advance(t, client, store, src)
	if store.load().Offset != 0 {
		t.Fatal("lost response acknowledged locally")
	}
	// Reopen the real durable PR #8 store while preserving its originals root.
	mu.Lock()
	if err := svc.Uploads().Close(); err != nil {
		t.Fatal(err)
	}
	start()
	mu.Unlock()
	for store.load().Offset < int64(len(src)) {
		advance(t, client, store, src)
	}
	mu.Lock()
	drop = "POST"
	mu.Unlock()
	advance(t, client, store, src)
	job = advance(t, client, store, src)
	if job.Ready == nil || job.Ready.ContentHash != digest(src) {
		t.Fatalf("not ready: %+v", job)
	}
	r, err := client.Complete(t.Context(), bot, job.ReceiptID)
	if err != nil || r.ContentHash != job.Ready.ContentHash {
		t.Fatal("idempotent complete failed", err)
	}
	file, err := os.CreateTemp(t.TempDir(), "download")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	_, _ = file.Write(src[:12345])
	var durable int64
	if err := client.Download(t.Context(), bot, job.Ready.ContentHash, int64(len(src)), 12345, file, func(n int64) error { durable = n; return nil }); err != nil {
		t.Fatal(err)
	}
	_, _ = file.Seek(0, io.SeekStart)
	got, _ := io.ReadAll(file)
	if durable != int64(len(src)) || !bytes.Equal(got, src) {
		t.Fatal("reverse Range transfer integrity")
	}
	if _, err := client.Cancel(t.Context(), bot, job.ReceiptID); err == nil {
		t.Fatal("ready receipt canceled")
	}
}
