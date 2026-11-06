package computertask

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/media"
)

type takeoverDeliveryAssets struct {
	fixture     *deliveryFixture
	calls       atomic.Int32
	oldStarted  chan struct{}
	oldRelease  chan struct{}
	oldFinished chan struct{}
	oldFails    bool
}

func (a *takeoverDeliveryAssets) OpenSeekable(ctx context.Context, bot, hash string) (media.ReadSeekCloser, media.Asset, error) {
	old := a.calls.Add(1) == 1
	if old {
		close(a.oldStarted)
		defer close(a.oldFinished)
		select {
		case <-a.oldRelease:
		case <-ctx.Done():
			return nil, media.Asset{}, ctx.Err()
		}
	}
	if old == a.oldFails {
		return nil, media.Asset{}, ErrDeliveryAsset
	}
	return a.fixture.OpenSeekable(ctx, bot, hash)
}

func TestAsyncDeliveryLateLeaseCannotOverwriteTakeover(t *testing.T) {
	for _, oldFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "old-success-new-failure", true: "old-failure-new-success"}[oldFails], func(t *testing.T) {
			f, base, ctx, ref := newDeliveryFixture(t)
			a := &takeoverDeliveryAssets{fixture: f, oldStarted: make(chan struct{}), oldRelease: make(chan struct{}), oldFinished: make(chan struct{}), oldFails: oldFails}
			base.assets = a
			check := func(ctx context.Context, id DeliveryIdentity) error {
				if id != f.id {
					return ErrDeliveryBinding
				}
				return ctx.Err()
			}
			first, err := NewAsyncDeliveryService(ctx, base, check, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			second, err := NewAsyncDeliveryService(ctx, base, check, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			if _, err := first.Commit(ctx, f.id.OperationID, "takeover", ref, ""); err != nil {
				t.Fatal(err)
			}
			select {
			case <-a.oldStarted:
			case <-time.After(3 * time.Second):
				t.Fatal("old scan did not start")
			}
			// Simulate the persisted lease expiring while the old process remains
			// alive in I/O. Its captured deadline stays live; no sleep/clock race.
			expireDelivery(t, f, "takeover", 1)
			if out, err := second.Resolve(ctx, f.id.OperationID, "takeover", ref, "", false); err != nil || out.Outcome != "pending" {
				t.Fatalf("takeover: %+v %v", out, err)
			}
			second.wg.Wait()
			want := "failed"
			if oldFails {
				want = "committed"
			}
			winner, err := second.Resolve(ctx, f.id.OperationID, "takeover", ref, "", false)
			if err != nil || winner.Outcome != want {
				t.Fatalf("winner: %+v %v", winner, err)
			}
			select {
			case <-a.oldFinished:
				t.Fatal("old verifier was not kept alive")
			default:
			}
			close(a.oldRelease)
			first.wg.Wait()
			after, err := second.Resolve(ctx, f.id.OperationID, "takeover", ref, "", false)
			if err != nil || after != winner || a.calls.Load() != 2 {
				t.Fatalf("late epoch changed outcome/revision: before=%+v after=%+v calls=%d err=%v", winner, after, a.calls.Load(), err)
			}
		})
	}
}
