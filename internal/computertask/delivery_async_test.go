package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/media"
)

type delayedDeliveryAsset struct {
	fixture *deliveryFixture
	started chan struct{}
	release chan struct{}
}

func (a *delayedDeliveryAsset) OpenSeekable(ctx context.Context, bot, hash string) (media.ReadSeekCloser, media.Asset, error) {
	a.started <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, media.Asset{}, ctx.Err()
	case <-a.release:
	}
	return a.fixture.OpenSeekable(ctx, bot, hash)
}

func asyncFixture(t *testing.T) (*deliveryFixture, *AsyncDeliveryService, *delayedDeliveryAsset, context.Context, DeliveryRequest) {
	t.Helper()
	f, base, ctx, ref := newDeliveryFixture(t)
	a := &delayedDeliveryAsset{fixture: f, started: make(chan struct{}, 8), release: make(chan struct{})}
	base.assets = a
	s, err := NewAsyncDeliveryService(context.Background(), base, func(ctx context.Context, id DeliveryIdentity) error {
		if id != f.id {
			return ErrDeliveryBinding
		}
		return ctx.Err()
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return f, s, a, ctx, ref
}

func awaitDelivery(t *testing.T, s *AsyncDeliveryService, ctx context.Context, id, key string, ref DeliveryRequest, want string) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		out, err := s.Resolve(ctx, id, key, ref, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if out.Outcome == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("want %s, got %+v", want, out)
		case <-tick.C:
		}
	}
}

func expireDelivery(t *testing.T, f *deliveryFixture, key string, attempts int) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(f.record.Details, &fields); err != nil {
		t.Fatal(err)
	}
	var ledger deliveryLedger
	if err := json.Unmarshal(fields["delivery_ledger"], &ledger); err != nil {
		t.Fatal(err)
	}
	e := ledger.Entries[key]
	e.Until, e.Attempts = time.Now().Add(-time.Second), attempts
	ledger.Entries[key] = e
	fields["delivery_ledger"], _ = json.Marshal(ledger)
	f.record.Details, _ = json.Marshal(fields)
	f.record.Revision++
}

func TestAsyncDeliveryOutlivesRequestAndReconcilesLostFinish(t *testing.T) {
	f, s, a, ctx, ref := asyncFixture(t)
	request, cancel := context.WithCancel(ctx)
	out, err := s.Commit(request, f.id.OperationID, "result", ref, "")
	if err != nil || out.Outcome != "pending" {
		t.Fatalf("submit: %+v %v", out, err)
	}
	<-a.started
	cancel() // The accepted finite scan is independent of the HTTP budget.
	if out, err = s.Resolve(ctx, f.id.OperationID, "result", ref, "", false); err != nil || out.Outcome != "pending" {
		t.Fatalf("pending: %+v %v", out, err)
	}
	f.mu.Lock()
	f.loseResponse = context.DeadlineExceeded
	f.mu.Unlock()
	close(a.release)
	s.wg.Wait() // Finish committed but its acknowledgement was lost.
	awaitDelivery(t, s, ctx, f.id.OperationID, "result", ref, "committed")
}

func TestAsyncDeliveryLostAdmissionRecoversAfterRestart(t *testing.T) {
	f, s, a, ctx, ref := asyncFixture(t)
	f.loseResponse = db.ErrCommitOutcomeUnknown
	if _, err := s.Commit(ctx, f.id.OperationID, "result", ref, ""); !errors.Is(err, db.ErrCommitOutcomeUnknown) {
		t.Fatal(err)
	}
	out, err := s.Resolve(ctx, f.id.OperationID, "result", ref, "", false)
	if err != nil || out.Outcome != "pending" {
		t.Fatalf("unknown is not absent: %+v %v", out, err)
	}
	select {
	case <-a.started:
		t.Fatal("unknown claim blindly started work")
	default:
	}
	s.Close()
	expireDelivery(t, f, "result", 1)
	restarted, err := NewAsyncDeliveryService(context.Background(), s.base, s.check, s.log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	out, err = restarted.Resolve(ctx, f.id.OperationID, "result", ref, "", false)
	if err != nil || out.Outcome != "pending" {
		t.Fatalf("reclaim: %+v %v", out, err)
	}
	<-a.started
	close(a.release)
	awaitDelivery(t, restarted, ctx, f.id.OperationID, "result", ref, "committed")
}

func TestAsyncDeliveryCancelAndRevokeDuringScan(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "revoke"}[revoke], func(t *testing.T) {
			f, s, a, ctx, ref := asyncFixture(t)
			if _, err := s.Commit(ctx, f.id.OperationID, "result", ref, ""); err != nil {
				t.Fatal(err)
			}
			<-a.started
			if revoke {
				f.mu.Lock()
				f.revoked = true
				f.mu.Unlock()
			} else {
				out, err := s.Resolve(ctx, f.id.OperationID, "result", ref, "", true)
				if err != nil || out.Outcome != "canceled" {
					t.Fatalf("fence: %+v %v", out, err)
				}
			}
			close(a.release)
			s.wg.Wait()
			out, err := s.Resolve(ctx, f.id.OperationID, "result", ref, "", false)
			if revoke {
				if !errors.Is(err, ErrDenied) {
					t.Fatal(err)
				}
			} else if err != nil || out.Outcome != "canceled" {
				t.Fatalf("late finish crossed fence: %+v %v", out, err)
			}
		})
	}
}

func TestAsyncDeliveryBackpressureAndExhaustion(t *testing.T) {
	f, s, a, ctx, ref := asyncFixture(t)
	for _, key := range []string{"first", "second"} {
		if _, err := s.Commit(ctx, f.id.OperationID, key, ref, ""); err != nil {
			t.Fatal(err)
		}
		<-a.started
	}
	if _, err := s.Commit(ctx, f.id.OperationID, "third", ref, ""); !errors.Is(err, ErrDeliveryLimit) {
		t.Fatal(err)
	}
	close(a.release)
	s.wg.Wait()
	f.mu.Lock()
	f.loseResponse = db.ErrCommitOutcomeUnknown
	f.mu.Unlock()
	if _, err := s.Commit(ctx, f.id.OperationID, "exhausted", ref, ""); !errors.Is(err, db.ErrCommitOutcomeUnknown) {
		t.Fatal(err)
	}
	expireDelivery(t, f, "exhausted", 3)
	out, err := s.Resolve(ctx, f.id.OperationID, "exhausted", ref, "", false)
	if err != nil || out.Outcome != "failed" {
		t.Fatalf("budget: %+v %v", out, err)
	}
	out, err = s.Commit(ctx, f.id.OperationID, "exhausted", ref, "")
	if err != nil || out.Outcome != "failed" {
		t.Fatalf("blind resubmit reset budget: %+v %v", out, err)
	}
}

func TestAsyncDownloadRequiresLocalPublicationOutcome(t *testing.T) {
	f, s, a, ctx, ref := asyncFixture(t)
	ref.Direction = "download"
	if _, err := s.Commit(ctx, f.id.OperationID, "result", ref, ""); !errors.Is(err, ErrDeliveryBinding) {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, f.id.OperationID, "result", ref, "", true); !errors.Is(err, ErrDeliveryBinding) {
		t.Fatal(err)
	}
	out, err := s.Commit(ctx, f.id.OperationID, "result", ref, "published")
	if err != nil || out.Outcome != "pending" {
		t.Fatalf("published: %+v %v", out, err)
	}
	<-a.started
	if _, err := s.Resolve(ctx, f.id.OperationID, "result", ref, "fenced", true); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("NAS retracted local publication: %v", err)
	}
	close(a.release)
	awaitDelivery(t, s, ctx, f.id.OperationID, "result", ref, "committed")
	out, err = s.Resolve(ctx, f.id.OperationID, "other", ref, "fenced", true)
	if err != nil || out.Outcome != "canceled" {
		t.Fatalf("local fence: %+v %v", out, err)
	}
	if _, err := s.Commit(ctx, f.id.OperationID, "other", ref, "published"); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatal(err)
	}
}
