package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func downloadSpaceQueue(t *testing.T, limits QueueLimits) (*DiskQueue, func() *DiskQueue) {
	t.Helper()
	root := configFixtureRoot(t)
	open := func() *DiskQueue {
		q, err := OpenHerdrQueue(func(string) string { return root }, "revision-1", limits)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = q.Close() })
		return q
	}
	return open(), open
}

func TestDownloadMetadataDoesNotReservePayloadOrBacklog(t *testing.T) {
	limits := DefaultHostQueueLimits()
	q, reopen := downloadSpaceQueue(t, limits)
	before, err := q.root.ReadFile("locator")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < limits.Jobs+1; i++ {
		j := newJob([]byte("metadata-only"))
		j.DeliveryID = fmt.Sprintf("download-%d", i)
		j.SnapshotID = j.DeliveryID
		j.Spec.SizeBytes = 1 << 30
		if err := q.Admit(t.Context(), "download", j, nil); err != nil {
			t.Fatal("total bytes/jobs blocked metadata", i, err)
		}
		st, err := q.root.Stat(queueName(j.DeliveryID + ".part"))
		if err != nil || st.Size() != 0 {
			t.Fatal("payload allocated at registration", err)
		}
	}
	_ = q.Close()
	q = reopen()
	after, err := q.root.ReadFile("locator")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("locator changed or demanded migration", err)
	}
	for i := 0; i < limits.Jobs+1; i++ {
		r, err := q.Inspect(fmt.Sprintf("download-%d", i))
		if err != nil || r.Job.Spec.SizeBytes != 1<<30 || r.Job.Offset != 0 {
			t.Fatal("reopen lost metadata", err)
		}
	}
}

func TestDownloadDiskPressureRetainsCheckpointAndWorkerBound(t *testing.T) {
	data := bytes.Repeat([]byte("bounded-stream"), int(2*MaxChunkBytes/14)+17)
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	var ranges []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		w.Header().Set("ETag", `"`+digest+`"`)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	client, err := New(srv.URL, srv.Client(), func(context.Context) (string, error) { return "synthetic", nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	q, reopen := downloadSpaceQueue(t, DefaultHostQueueLimits())
	job := newJob(data)
	job.SnapshotID = job.DeliveryID
	job.Task = DeviceTask{UserID: "u", SessionID: "s", TargetID: "t", RunID: "r", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	job.HostBinding = "fixture"
	if err := q.Admit(t.Context(), "download", job, nil); err != nil {
		t.Fatal(err)
	}
	calls := 0
	q.downloadSpaceCheck = func(n int64) error {
		if n < 1 || n > MaxChunkBytes {
			t.Fatal("non-window space check", n)
		}
		calls++
		if calls > 2 {
			return ErrCapacity
		}
		return nil
	}
	now := time.Now()
	if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, now); err != nil {
		t.Fatal(err)
	}
	r, err := q.Inspect(job.DeliveryID)
	if err != nil || r.Job.Offset == 0 || r.Job.Offset >= job.Spec.SizeBytes || r.Job.ErrorCode != "capacity_wait" || r.Job.Phase == "blocked" || r.Job.Attempts != 0 {
		t.Fatal("disk pressure lost progress", r, err)
	}
	offset := r.Job.Offset
	if summary := recordLifecycle(r, job.HostBinding, now); summary.State != "waiting" || summary.ErrorClass != "capacity" || summary.NextDueAt == nil {
		t.Fatal("missing capacity wake", summary)
	}
	_ = q.Close()
	q = reopen()
	q.downloadSpaceCheck = func(int64) error { return ErrCapacity }
	for range 13 {
		now = r.Job.NextAttempt
		if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, now); err != nil {
			t.Fatal(err)
		}
		r, err = q.Inspect(job.DeliveryID)
		if err != nil || r.Job.Offset != offset || r.Job.Attempts != 0 || r.Job.Phase == "blocked" {
			t.Fatal("space pressure consumed network retries", r, err)
		}
	}
	if len(ranges) != 1 {
		t.Fatal("no-space wake sent network requests", ranges)
	}
	q.downloadSpaceCheck = nil
	worker, err := q.worker()
	if err != nil {
		t.Fatal(err)
	}
	if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, r.Job.NextAttempt); !errors.Is(err, ErrBusy) {
		t.Fatal("worker bound removed", err)
	}
	unlockClose(worker)
	if err := q.RunOne(t.Context(), job.DeliveryID, client, nil, r.Job.NextAttempt); err != nil {
		t.Fatal(err)
	}
	r, err = q.Inspect(job.DeliveryID)
	if err != nil || r.Job.Phase != "ready" || r.Job.Offset != job.Spec.SizeBytes || r.Job.ErrorCode != "" || !r.Job.NextAttempt.IsZero() || r.Job.Task != job.Task {
		t.Fatal("resume failed", r, err)
	}
	if len(ranges) != 2 || ranges[1] != fmt.Sprintf("bytes=%d-", offset) {
		t.Fatal("confirmed prefix retransmitted", ranges, offset)
	}
	f, err := q.file(job.DeliveryID+".part", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil || hex.EncodeToString(h.Sum(nil)) != digest {
		t.Fatal("resume mixed content", err)
	}
}

func TestDownloadDiskPressureKeepsUnknownAssociation(t *testing.T) {
	q, _ := downloadSpaceQueue(t, DefaultHostQueueLimits())
	job := newJob([]byte("fixture"))
	job.SnapshotID = job.DeliveryID
	if err := q.Admit(t.Context(), "download", job, nil); err != nil {
		t.Fatal(err)
	}
	r, f, err := q.load(job.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	r.Job.Phase = "ready"
	r.Job.Ready = &Reference{DeliveryID: job.DeliveryID, OperationID: job.OperationID, BotID: job.BotID, ContentHash: job.Spec.SHA256, SizeBytes: job.Spec.SizeBytes, Mime: job.Spec.Mime}
	r.AssociationState = AssociationUnknown
	for range 13 {
		r.AssociationAttempts++
		if err := q.waitAssociation(f, r, errDiskSpace, time.Now()); err != nil {
			t.Fatal(err)
		}
		r, err = q.Inspect(job.DeliveryID)
		if err != nil || r.AssociationBlocked || r.AssociationAttempts != 0 || r.AssociationState != AssociationUnknown || r.AssociationError != "capacity" {
			t.Fatal("space pressure lost unknown", r, err)
		}
	}
	binding := &AssociationBinding{Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		return OutcomeCommitted, nil
	}}
	if err := q.resolveAssociation(t.Context(), f, r, binding, r.AssociationDue); err != nil {
		t.Fatal(err)
	}
	got, err := q.Inspect(job.DeliveryID)
	if err != nil || !got.Associated {
		t.Fatal("original unknown did not reconcile", got, err)
	}
}
