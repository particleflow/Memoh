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
	var refs []Reference
	for i := range 160 {
		ref := metadataReference(fmt.Sprintf("native-fence-%03d", i))
		refs = append(refs, ref)
		if out, err := p.Resolve(t.Context(), ref, true); err != nil || out != OutcomeCanceled {
			t.Fatal(i, out, err)
		}
	}
	_ = p.Close()
	p, err = q.OpenNativePublication(authorize)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	for _, ref := range refs {
		if out, err := p.Resolve(t.Context(), ref, false); err != nil || out != OutcomeCanceled {
			t.Fatal("retained tombstone", out, err)
		}
	}
	conflict := refs[0]
	conflict.OperationID = "different-operation"
	if _, err := p.Resolve(t.Context(), conflict, true); !errors.Is(err, ErrPublicationConflict) {
		t.Fatal("same ID conflict", err)
	}
}

func TestWindowsNativePublicationBucketBudget(t *testing.T) {
	q := nativeQueue(t, nativeFixtureRoot(t, false))
	p, err := q.OpenNativePublication(func(ctx context.Context, _ Reference) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	var refs []Reference
	for n := 0; len(refs) < publicationReceipts+1; n++ {
		ref := metadataReference(fmt.Sprintf("same-bucket-%d", n))
		bucket, _ := publicationBucket(publicationKey(ref) + ".journal")
		if bucket == 0 {
			refs = append(refs, ref)
		}
	}
	for _, ref := range refs[:publicationReceipts] {
		if out, err := p.Resolve(t.Context(), ref, true); err != nil || out != OutcomeCanceled {
			t.Fatal(out, err)
		}
	}
	last := refs[publicationReceipts]
	if out, err := p.Resolve(t.Context(), last, true); !errors.Is(err, ErrCapacity) || out != "" {
		t.Fatal("bucket quota", out, err)
	}
	if out, err := p.Resolve(t.Context(), last, false); err != nil || out != OutcomeAbsent {
		t.Fatal("failed reservation fabricated fence", out, err)
	}
	if out, err := p.Resolve(t.Context(), refs[0], true); err != nil || out != OutcomeCanceled {
		t.Fatal("full budget blocked replay", out, err)
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
