package computertask

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/deliverymeta"
)

func bindFixtureProducer(t *testing.T, f *deliveryFixture) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var d Details
	if json.Unmarshal(f.record.Details, &d) != nil {
		t.Fatal("fixture details")
	}
	b := f.id.Binding
	p := &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: f.id.OperationID, BotID: b.BotID, UserID: b.UserID, SessionID: b.SessionID, TargetID: b.TargetID, RunID: b.RunID, Attempt: f.id.Attempt, Generation: f.id.Generation, RootHash: deliverymeta.RootHash(`C:\Fixture`)}, Artifact: deliverymeta.Artifact{SourceRef: "result", Mime: f.asset.Mime}}
	d.Batch.Progress.Producer = p
	f.record.Details, _ = json.Marshal(d)
	return p.DeliveryID()
}
func cancelFixtureProducer(f *deliveryFixture) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record.CancelRequested = true
	f.record.Revision++
}

func TestProducerCancellationFencesExitHookAndQueuedUpload(t *testing.T) {
	for _, stage := range []string{"before-close", "already-enqueued"} {
		t.Run(stage, func(t *testing.T) {
			f, base, ctx, _ := principalFixture(t, "upload")
			bindFixtureProducer(t, f)
			if stage == "already-enqueued" {
				if err := base.ReserveTransfer(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := base.ClaimTransferUpload(ctx); err != nil {
					t.Fatal(err)
				}
				if err := base.FinishTransferUpload(ctx, "original-receipt"); err != nil {
					t.Fatal(err)
				}
			}
			cancelFixtureProducer(f)
			before := f.record.Revision
			if err := base.ReserveTransfer(ctx); !errors.Is(err, ErrDenied) {
				t.Fatal("canceled producer exit hook may reserve upload", err)
			}
			if err := base.CheckTransfer(ctx, true); !errors.Is(err, ErrDenied) {
				t.Fatal("queued producer may continue mutation", err)
			}
			if _, err := base.ClaimTransferUpload(ctx); !errors.Is(err, ErrDenied) {
				t.Fatal("canceled producer may create receipt", err)
			}
			if f.record.Revision != before {
				t.Fatal("canceled producer changed operation")
			}
		})
	}
}

func TestProducerCancellationCompetesWithVerifiedCommit(t *testing.T) {
	for _, winner := range []string{"cancel", "commit"} {
		t.Run(winner, func(t *testing.T) {
			f, s, a, ctx, ref := asyncFixture(t)
			id := bindFixtureProducer(t, f)
			out, err := s.Commit(ctx, f.id.OperationID, id, ref, "")
			if err != nil || out.Outcome != "pending" {
				t.Fatal(out, err)
			}
			<-a.started
			if winner == "cancel" {
				cancelFixtureProducer(f)
			}
			close(a.release)
			s.wg.Wait()
			if winner == "commit" {
				cancelFixtureProducer(f)
			}
			for range 2 {
				out, err = s.Resolve(ctx, f.id.OperationID, id, ref, "", false)
				if winner == "cancel" && !errors.Is(err, ErrDenied) {
					t.Fatal("scan committed after cancellation", out, err)
				}
				if winner == "commit" && (err != nil || out.Outcome != "committed") {
					t.Fatal("cancellation retracted committed result", out, err)
				}
			}
			var d Details
			_ = json.Unmarshal(f.record.Details, &d)
			result := producerResult(f.record, d)
			if winner == "cancel" && (result.State != "canceled" || result.Reference != nil) {
				t.Fatal("canceled projection claimed delivery", result.State)
			}
			if winner == "commit" && (result.State != "delivered" || result.Reference == nil) {
				t.Fatal("committed reference lost")
			}
		})
	}
}
