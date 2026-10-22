//go:build windows

package transferclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestWindowsNativePublicationReceiptBoundary(t *testing.T) {
	q := nativeQueue(t, nativeFixtureRoot(t, false))
	authorize := func(ctx context.Context, _ Reference) error { return ctx.Err() }
	p, err := q.OpenNativePublication(authorize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	j := newJob([]byte("receipt-boundary"))
	ref := Reference{OperationID: j.OperationID, BotID: j.BotID, ContentHash: j.Spec.SHA256, SizeBytes: j.Spec.SizeBytes, Mime: j.Spec.Mime}
	for i := 1; i <= 128; i++ {
		ref.DeliveryID = fmt.Sprintf("fence-%03d", i)
		if out, err := p.Resolve(t.Context(), ref, true); err != nil || out != OutcomeCanceled {
			t.Fatalf("receipt %d: %s %v", i, out, err)
		}
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p, err = q.OpenNativePublication(authorize)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := p.Resolve(t.Context(), ref, true); err != nil || out != OutcomeCanceled {
		t.Fatal("128th replay after reopen at capacity", out, err)
	}
	ref.DeliveryID = "fence-129"
	if out, err := p.Resolve(t.Context(), ref, true); !errors.Is(err, ErrCapacity) || out != "" {
		t.Fatal("129th fence must fail without acknowledgment", out, err)
	}
	if _, err := p.root.Lstat(publicationKey(ref) + ".journal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected fence consumed a receipt", err)
	}
	if out, err := p.Resolve(t.Context(), ref, false); err != nil || out != OutcomeAbsent {
		t.Fatal("rejected fence fabricated cancellation", out, err)
	}
}

func TestWindowsNativePublicationByteBoundarySurvivesQueueRetire(t *testing.T) {
	limits := nativeLimits()
	limits.Bytes = 2 << 20
	root := nativeFixtureRoot(t, false)
	q, err := OpenHerdrQueue(func(string) string { return root }, "revision-1", limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	p, err := q.OpenNativePublication(func(ctx context.Context, _ Reference) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	var retained []Reference
	for i, size := range []int{1 << 20, 1 << 20, 1} {
		data := make([]byte, size)
		data[0] = byte(i + 1)
		j := newJob(data)
		j.DeliveryID = fmt.Sprintf("byte-boundary-%d", i)
		j.SnapshotID = j.DeliveryID
		if err := q.Admit(t.Context(), "download", j, nil); err != nil {
			t.Fatal("queue admission must be independent of retained destination", err)
		}
		client := publicationNetworkFixture(t, data)
		for range 2 {
			if err := q.RunOne(t.Context(), j.DeliveryID, client, client.DeliveryBinding(p), time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		status, err := q.Inspect(j.DeliveryID)
		if err != nil || status.Job.Ready == nil {
			t.Fatal("download not verified", err)
		}
		ref := *status.Job.Ready
		if i == 2 {
			if status.Associated || !status.AssociationBlocked || status.AssociationState != AssociationUnknown {
				t.Fatalf("one byte beyond retained budget acknowledged: %+v", status)
			}
			if out, err := p.Resolve(t.Context(), ref, false); err != nil || out != OutcomeAbsent {
				t.Fatal("rejected byte created publication", out, err)
			}
			if _, err := p.root.Lstat(publicationKey(ref) + ".object"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("over-budget object exists", err)
			}
			continue
		}
		if !status.Associated {
			t.Fatalf("publication at or below exact budget rejected: %+v", status)
		}
		if err := q.Retire(j.DeliveryID); err != nil {
			t.Fatal(err)
		}
		retained = append(retained, ref)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p, err = q.OpenNativePublication(func(ctx context.Context, _ Reference) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range retained {
		if out, err := p.Resolve(t.Context(), ref, true); err != nil || out != OutcomeCommitted {
			t.Fatal("retire/capacity failure removed acknowledged bytes", out, err)
		}
	}
}
