//go:build linux || darwin

package handlers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/transferclient"
)

func TestDeviceTransferRevalidationOutageResumesOriginalReceipt(t *testing.T) {
	v := &deviceVerifierFixture{}
	var creates, unexpected atomic.Int64
	f, _, https, _ := taskTransferHTTPFixture(t, false, taskTransferFixtureOptions{
		device: func(_ computertask.DeliveryIdentity, target agenvo.Target) DeviceTransferVerifier {
			v.device = computertask.VerifiedTransferDevice{CredentialID: "stable-device", ConnectionID: target.ConnectionID, DeviceID: target.DeviceID, InstanceID: target.InstanceID}
			return v
		},
		request: func(r *http.Request) {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media/uploads") {
				creates.Add(1)
			}
			if !strings.HasPrefix(r.URL.Path, ComputerTransferPrefix+"/") {
				unexpected.Add(1)
			}
		},
	})
	job := transferclient.Job{Version: 1, DeliveryID: "revalidation-outage", BotID: f.id.Binding.BotID, OperationID: f.id.OperationID, SnapshotID: "revalidation-outage", LocatorRevision: "fixture", Phase: "queued", Spec: transferclient.Spec{SizeBytes: f.ref.SizeBytes, SHA256: f.ref.ContentHash, Mime: f.ref.Mime}}
	task := transferclient.DeviceTask{UserID: f.id.Binding.UserID, SessionID: f.id.Binding.SessionID, TargetID: f.id.Binding.TargetID, RunID: f.id.Binding.RunID, Attempt: f.id.Attempt, Generation: f.id.Generation}
	client, err := transferclient.NewDeviceTask(https.URL, job, "upload", task, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer synthetic-reusable-device", nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	root := t.TempDir()
	open := func() *transferclient.DiskQueue {
		t.Helper()
		q, err := transferclient.OpenHerdrQueue(func(string) string { return root }, "fixture", transferclient.QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = q.Close() })
		return q
	}
	q := open()
	source, err := os.CreateTemp(t.TempDir(), "closed-result")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	if _, err = source.WriteString("fixture"); err != nil {
		t.Fatal(err)
	}
	if err = q.Admit(t.Context(), "upload", job, source); err != nil {
		t.Fatal(err)
	}
	inspect := func() transferclient.QueueStatus {
		t.Helper()
		s, err := q.Inspect(job.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	step := func() transferclient.QueueStatus {
		t.Helper()
		s := inspect()
		now := time.Now()
		if s.Job.NextAttempt.After(now) {
			now = s.Job.NextAttempt
		}
		if s.AssociationDue.After(now) {
			now = s.AssociationDue
		}
		if err = q.RunOne(t.Context(), job.DeliveryID, client, client.DeliveryBinding(nil), now); err != nil {
			t.Fatal(err)
		}
		return inspect()
	}
	before := step().Job
	if before.ReceiptID == "" || before.Phase != "uploading" {
		t.Fatal("missing initial durable receipt")
	}
	// The initial VerifyDevice still succeeds. Only the later guard recheck fails.
	v.revalidationUnavailable.Store(true)
	_, err = client.Get(t.Context(), job.BotID, before.ReceiptID)
	var status *transferclient.HTTPError
	if !errors.As(err, &status) || status.Status != http.StatusServiceUnavailable {
		t.Fatal("guard revalidation must return 503", err)
	}
	paused := step()
	if paused.Job.Phase == "blocked" || paused.Job.ErrorCode != "server_unavailable" || paused.Job.NextAttempt.IsZero() || paused.Job.Attempts != 1 || paused.Job.ReceiptID != before.ReceiptID {
		t.Fatal("temporary revalidation lost retry state", paused.Job.Phase, paused.Job.ErrorCode)
	}
	if err = q.Close(); err != nil {
		t.Fatal(err)
	}
	q = open() // Real journal/restart, not an in-memory job copy.
	v.revalidationUnavailable.Store(false)
	for n := 0; n < 12 && !inspect().Associated; n++ {
		step()
		time.Sleep(10 * time.Millisecond)
	}
	after := inspect()
	if !after.Associated || after.AssociationBlocked || after.Job.Phase != "ready" || after.Job.ReceiptID != before.ReceiptID || after.Job.OperationID != before.OperationID || after.Job.DeliveryID != before.DeliveryID || creates.Load() != 1 || unexpected.Load() != 0 {
		t.Fatal("did not resume original delivery/operation/receipt", after.Job.Phase, after.AssociationState, creates.Load(), unexpected.Load())
	}
	v.revoked.Store(true)
	_, err = client.Get(t.Context(), job.BotID, before.ReceiptID)
	if !errors.As(err, &status) || status.Status != http.StatusForbidden {
		t.Fatal("actual revocation became retryable", err)
	}
}
