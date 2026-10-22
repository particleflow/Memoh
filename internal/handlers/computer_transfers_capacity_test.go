//go:build linux || darwin

package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/transferclient"
)

type heldTransferAssets struct {
	*media.Service
	started chan struct{}
	release chan struct{}
}

func (a *heldTransferAssets) OpenSeekable(ctx context.Context, bot, hash string) (media.ReadSeekCloser, media.Asset, error) {
	a.started <- struct{}{}
	select {
	case <-a.release:
		return a.Service.OpenSeekable(ctx, bot, hash)
	case <-ctx.Done():
		return nil, media.Asset{}, ctx.Err()
	}
}

func TestTaskTransferCapacityQueueRecovery(t *testing.T) {
	gate := &heldTransferAssets{started: make(chan struct{}, 3), release: make(chan struct{})}
	var creates, unexpected atomic.Int64
	f, verifier, https, _ := taskTransferHTTPFixture(t, false, taskTransferFixtureOptions{
		assets: func(s *media.Service) computertask.DeliveryAssets { gate.Service = s; return gate },
		request: func(r *http.Request) {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media/uploads") {
				creates.Add(1)
			}
			if !strings.HasPrefix(r.URL.Path, ComputerTransferPrefix+"/") {
				unexpected.Add(1)
			}
		},
	})
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate.release) }) }
	t.Cleanup(unblock) // Unblock before the handler's Close waits for workers.
	verifier.mu.Lock()
	third := verifier.grants["one"]
	third.DeliveryID, third.CredentialID = "three", "fixture-three"
	verifier.grants["three"] = third
	verifier.mu.Unlock()

	root := t.TempDir()
	openQueue := func() *transferclient.DiskQueue {
		q, err := transferclient.OpenHerdrQueue(func(string) string { return root }, "revision-1", transferclient.QueueLimits{Jobs: 3, Bytes: 32 << 20, JournalBytes: 1 << 20, Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = q.Close() })
		return q
	}
	q := openQueue()
	inspect := func(id string) transferclient.QueueStatus {
		r, err := q.Inspect(id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	clients := make(map[string]*transferclient.Client)
	bindings := make(map[string]*transferclient.AssociationBinding)
	run := func(id string) transferclient.QueueStatus {
		r := inspect(id)
		now := time.Now()
		if r.AssociationDue.After(now) {
			now = r.AssociationDue
		}
		if r.Job.NextAttempt.After(now) {
			now = r.Job.NextAttempt
		}
		if err := q.RunOne(t.Context(), id, clients[id], bindings[id], now); err != nil {
			t.Fatal(err)
		}
		return inspect(id)
	}
	for _, id := range []string{"one", "two", "three"} {
		client, err := transferclient.NewTask(https.URL, f.id.OperationID, id, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + id, nil })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.CloseIdleConnections)
		clients[id], bindings[id] = client, client.DeliveryBinding(nil)
		source, err := os.CreateTemp(t.TempDir(), "closed-output")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = source.Close() })
		if _, err := source.WriteString("fixture"); err != nil {
			t.Fatal(err)
		}
		job := transferclient.Job{Version: 1, DeliveryID: id, BotID: f.id.Binding.BotID, OperationID: f.id.OperationID, SnapshotID: id, LocatorRevision: "revision-1", Phase: "queued", Spec: transferclient.Spec{SizeBytes: f.ref.SizeBytes, SHA256: f.ref.ContentHash, Mime: f.ref.Mime}}
		if err := q.Admit(t.Context(), "upload", job, source); err != nil {
			t.Fatal(err)
		}
		for n := 0; n < 8 && inspect(id).Job.Phase != "ready"; n++ {
			run(id)
		}
		if inspect(id).Job.Phase != "ready" {
			t.Fatal("upload did not become ready", id)
		}
	}
	for _, id := range []string{"one", "two"} {
		run(id)
		select {
		case <-gate.started:
		case <-time.After(5 * time.Second):
			t.Fatal("verification did not acquire slot", id)
		}
	}
	for _, id := range []string{"one", "two"} {
		outcome, err := bindings[id].Resolve(t.Context(), "upload", *inspect(id).Job.Ready, false)
		if err != nil || outcome != transferclient.OutcomePending {
			t.Fatal("capacity hid an existing pending delivery", outcome, err)
		}
	}

	before := inspect("three").Job
	commit := bindings["three"].Commit
	var commitErr error
	bindings["three"].Commit = func(ctx context.Context, direction string, ref transferclient.Reference, content io.Reader) error {
		commitErr = commit(ctx, direction, ref, content)
		return commitErr
	}
	r := run("three")
	var status *transferclient.HTTPError
	if !errors.As(commitErr, &status) || status.Status != http.StatusTooManyRequests || r.AssociationBlocked || r.AssociationState != transferclient.AssociationUnknown || r.AssociationDue.IsZero() {
		t.Fatalf("capacity refusal must remain retryable: err=%v blocked=%v state=%s due=%v", commitErr, r.AssociationBlocked, r.AssociationState, r.AssociationDue)
	}
	// A process restart must preserve the unknown result and finite retry budget.
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	q = openQueue()
	r = run("three") // Reserved resolves absent, so the same delivery can retry Commit.
	if r.AssociationState != transferclient.AssociationRetryCommit || r.AssociationBlocked {
		t.Fatal("reserved delivery did not reconcile to a retry", r.AssociationState)
	}
	unblock()
	waitCommitted := func(id string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			outcome, err := bindings[id].Resolve(t.Context(), "upload", *inspect(id).Job.Ready, false)
			if err != nil {
				t.Fatal(err)
			}
			if outcome == transferclient.OutcomeCommitted {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("verification did not finish", id)
	}
	waitCommitted("one")
	waitCommitted("two")
	for n := 0; n < 6 && !inspect("three").Associated; n++ {
		r = run("three")
		if r.AssociationBlocked {
			t.Fatal("capacity recovery blocked")
		}
		if r.AssociationState == transferclient.AssociationUnknown && errors.Is(commitErr, transferclient.ErrTransport) {
			waitCommitted("three")
		}
	}
	r = inspect("three")
	if !r.Associated || r.Job.ReceiptID != before.ReceiptID || r.Job.OperationID != before.OperationID || r.Job.DeliveryID != before.DeliveryID || *r.Job.Ready != *before.Ready || r.AssociationAttempts >= 12 {
		t.Fatal("did not automatically recover the original receipt and operation")
	}
	if creates.Load() != 3 || unexpected.Load() != 0 {
		t.Fatalf("unexpected receipt creation or business dispatch: creates=%d other=%d", creates.Load(), unexpected.Load())
	}
}
