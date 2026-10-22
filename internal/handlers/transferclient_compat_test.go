package handlers

// Compatibility fixture: real handlers, synthetic accounts/grants/identity,
// temporary local storage and a trusted test TLS certificate. No PC/NAS wiring.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/transferclient"
)

type silentCompatibilityWriter struct{ http.ResponseWriter }

func (w silentCompatibilityWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (silentCompatibilityWriter) WriteHeader(int)               {}
func (silentCompatibilityWriter) Write(p []byte) (int, error)   { return len(p), nil }

type compatibilitySnapshot []byte

func (s compatibilitySnapshot) ReadAt(_ context.Context, _, _ string, spec transferclient.Spec, dst []byte, offset int64) error {
	sum := sha256.Sum256(s)
	if int64(len(s)) != spec.SizeBytes || hex.EncodeToString(sum[:]) != spec.SHA256 {
		return transferclient.ErrIntegrity
	}
	if copy(dst, s[offset:]) != len(dst) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func TestPR10ClientAgainstIntegratedPR8Handlers(t *testing.T) {
	// Reuse PR8's real NewServer/JWT/MessageHandler test setup; only database
	// account/grant reads and the signing secret are synthetic fixtures.
	f := newUploadHTTPFixture(t)
	f.server.Close()
	handler := f.server.Config.Handler
	var drop atomic.Int32
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind := int32(0)
		switch {
		case r.Method == http.MethodPatch:
			kind = 1
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			kind = 2
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/uploads"):
			kind = 3
		}
		if kind != 0 && drop.CompareAndSwap(kind, 0) {
			handler.ServeHTTP(silentCompatibilityWriter{w}, r)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer tlsServer.Close()
	client, err := transferclient.New(tlsServer.URL, tlsServer.Client(), func(context.Context) (string, error) { return "Bearer " + f.token, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	limits, err := client.Limits(t.Context(), invocationTestBotID)
	if err != nil || !limits.ResumableEnabled || limits.ChunkMaxBytes != 8<<20 {
		t.Fatal("real limits mismatch", limits, err)
	}
	src := compatibilitySnapshot{0xff, 0, 1, 2, 0x80, 4, 5, 6, 7}
	sum := sha256.Sum256(src)
	job := transferclient.Job{Version: 1, DeliveryID: "fixture-delivery", BotID: invocationTestBotID, OperationID: "fixture-operation", SnapshotID: "fixture-snapshot", LocatorRevision: "fixture-v1", Phase: "queued", ChunkBytes: 4, Spec: transferclient.Spec{SizeBytes: int64(len(src)), SHA256: hex.EncodeToString(sum[:]), Mime: "application/octet-stream"}}
	save := func(j transferclient.Job) error { job = j; return nil }
	step := func() {
		t.Helper()
		now := time.Now()
		if now.Before(job.NextAttempt) {
			now = job.NextAttempt
		}
		if err := client.Advance(t.Context(), job, save, src, now); err != nil {
			t.Fatal(err)
		}
	}
	step()
	if job.ReceiptID == "" {
		t.Fatal("real create failed", job)
	}
	drop.Store(1)
	step()
	if job.Offset != 0 || job.Ready != nil {
		t.Fatal("lost PATCH was locally acknowledged")
	}
	for n := 0; n < 5 && job.Offset < job.Spec.SizeBytes; n++ {
		step()
	}
	if job.Offset != job.Spec.SizeBytes {
		t.Fatal("real PATCH recovery failed", job)
	}
	drop.Store(2)
	step()
	if job.Ready != nil {
		t.Fatal("lost complete marked ready")
	}
	step()
	if job.Phase != "ready" || job.Ready == nil || job.Ready.ContentHash != job.Spec.SHA256 {
		t.Fatal("real complete recovery failed", job)
	}
	ready, err := client.Complete(t.Context(), invocationTestBotID, job.ReceiptID)
	if err != nil || ready.ContentHash != job.Ready.ContentHash {
		t.Fatal("real complete idempotency", err)
	}
	var status *transferclient.HTTPError
	_, err = client.Get(t.Context(), "44444444-4444-4444-4444-444444444444", job.ReceiptID)
	if !errors.As(err, &status) || status.Status != 404 {
		t.Fatal("cross-Bot receipt access", err)
	}
	file, err := os.CreateTemp(t.TempDir(), "fixture-download")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	_, _ = file.Write(src[:3])
	offset := int64(3)
	if err := client.Download(t.Context(), job.BotID, job.Spec.SHA256, job.Spec.SizeBytes, offset, file, func(n int64) error { offset = n; return nil }); err != nil {
		t.Fatal("real Range download", err)
	}
	_, _ = file.Seek(0, 0)
	actual, _ := io.ReadAll(file)
	if !bytes.Equal(actual, src) {
		t.Fatal("download bytes differ")
	}
	f.queries.revoked.Store(true)
	err = client.Download(t.Context(), job.BotID, job.Spec.SHA256, job.Spec.SizeBytes, offset, file, func(int64) error { return nil })
	if !errors.As(err, &status) || status.Status != 403 {
		t.Fatal("completed checkpoint bypassed revoked permission", err)
	}
	_, err = client.Get(t.Context(), job.BotID, job.ReceiptID)
	if !errors.As(err, &status) || status.Status != 403 {
		t.Fatal("receipt grant not rechecked", err)
	}
	f.queries.revoked.Store(false)
	_, _ = file.Seek(0, 0)
	_, _ = file.Write([]byte("!"))
	err = client.Download(t.Context(), job.BotID, job.Spec.SHA256, job.Spec.SizeBytes, offset, file, func(int64) error { return nil })
	if !errors.Is(err, transferclient.ErrIntegrity) {
		t.Fatal("offset==size with corrupt hash was accepted", err)
	}
	pending, err := client.Create(t.Context(), job.BotID, job.Spec)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		canceled, err := client.Cancel(t.Context(), job.BotID, pending.ID)
		if err != nil || canceled.State != "canceled" {
			t.Fatal("real cancel idempotency", err)
		}
	}
	job.Phase = "queued"
	job.Ready = nil
	job.ReceiptID = ""
	job.Offset = 0
	job.NextAttempt = time.Time{}
	drop.Store(3)
	step()
	if job.Phase != "create_unknown" {
		t.Fatal("lost real create was blindly retryable", job)
	}
	step()
	if job.Phase != "create_unknown" {
		t.Fatal("unknown create changed without reconciliation")
	}
}

func TestHerdrActionAgainstMergedUploadHandlers(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("native queue durability binding is not enabled on this platform")
	}
	f := newUploadHTTPFixture(t)
	f.server.Close()
	srv := httptest.NewTLSServer(f.server.Config.Handler)
	defer srv.Close()
	client, err := transferclient.New(srv.URL, srv.Client(), func(context.Context) (string, error) { return "Bearer " + f.token, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	state := t.TempDir()
	getenv := func(key string) string {
		if key != "HERDR_PLUGIN_STATE_DIR" {
			t.Fatal("unexpected locator")
		}
		return state
	}
	limits := transferclient.QueueLimits{Jobs: 4, Bytes: 32 << 20, JournalBytes: 1 << 20, Workers: 1}
	job := transferclient.Job{Version: 1, DeliveryID: "fixture-delivery", BotID: invocationTestBotID, OperationID: "fixture-operation", SnapshotID: "fixture-delivery", LocatorRevision: "fixture-v1", Phase: "queued", ChunkBytes: 4, Spec: transferclient.Spec{SizeBytes: int64(len(f.payload)), SHA256: f.hash, Mime: "application/octet-stream"}}
	source, err := os.CreateTemp(t.TempDir(), "producer")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	if _, err := source.Write(f.payload); err != nil {
		t.Fatal(err)
	}
	action := transferclient.HerdrAction{Kind: "enqueue-upload", DeliveryID: job.DeliveryID, Job: job}
	if _, err := transferclient.InvokeHerdr(t.Context(), getenv, "fixture-v1", limits, action, source, nil, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	action.Kind = "advance"
	var status transferclient.QueueStatus
	for n := 0; n < 6 && status.Job.Phase != "ready"; n++ {
		status, err = transferclient.InvokeHerdr(t.Context(), getenv, "fixture-v1", limits, action, nil, client, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	if status.Job.Phase != "ready" || status.Associated {
		t.Fatal("ready must remain unassociated")
	}
	// Explicit registrar fixture rechecks current authorization. It does not
	// stand in for a production task-binding endpoint.
	calls := 0
	register := func(_ context.Context, direction string, ref transferclient.Reference, content io.Reader) error {
		calls++
		if f.queries.revoked.Load() {
			return transferclient.ErrAuthorization
		}
		if direction != "upload" || content != nil || ref.ContentHash != f.hash {
			t.Fatal("wrong outbox")
		}
		return nil
	}
	f.queries.revoked.Store(true)
	status, err = transferclient.InvokeHerdr(t.Context(), getenv, "fixture-v1", limits, action, nil, client, register, time.Now())
	if err != nil || status.Associated || !status.AssociationBlocked || calls != 1 {
		t.Fatal("revoked ready association accepted", err)
	}
	f.queries.revoked.Store(false)
	job.DeliveryID = "fixture-download"
	job.SnapshotID = job.DeliveryID
	action = transferclient.HerdrAction{Kind: "enqueue-download", DeliveryID: job.DeliveryID, Job: job}
	if _, err := transferclient.InvokeHerdr(t.Context(), getenv, "fixture-v1", limits, action, nil, nil, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	action.Kind = "advance"
	if _, err := transferclient.InvokeHerdr(t.Context(), getenv, "fixture-v1", limits, action, nil, client, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	downloaded := false
	accept := func(_ context.Context, direction string, ref transferclient.Reference, content io.Reader) error {
		if direction != "download" || ref.ContentHash != f.hash || content == nil {
			return transferclient.ErrInvalid
		}
		data, err := io.ReadAll(content)
		if err != nil || !bytes.Equal(data, f.payload) {
			return transferclient.ErrIntegrity
		}
		downloaded = true
		return nil
	}
	status, err = transferclient.InvokeHerdr(t.Context(), getenv, "fixture-v1", limits, action, nil, client, accept, time.Now())
	if err != nil || !downloaded || !status.Associated {
		t.Fatal("reverse delivery failed", err)
	}
}
