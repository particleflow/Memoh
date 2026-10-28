package computertask

import (
	"testing"

	"github.com/felinics/memoh/internal/deliverymeta"
)

func TestExplicitFilenameSubmitRetainsOriginalPendingDelivery(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	service.assets = nil
	in := Submit{Kind: "herdr_pi_task", Task: "synthetic task writing exactly windows_photo_a0_result.png", Artifact: &deliverymeta.Artifact{SourceRef: "windows_photo_a0_result.png", Mime: "image/png"}}
	first, err := service.submit(ctx, f.binding, in)
	if err != nil {
		t.Fatal(err)
	}
	plan := f.batch.progress.Producer
	if plan == nil || !plan.Valid() || plan.Artifact != *in.Artifact || plan.Task.OperationID != first.ID || plan.Task.RootHash != deliverymeta.RootHash(f.profile.WorkDir) {
		t.Fatal("task entry did not preserve explicit filename/original root")
	}
	originalPlanHash, originalDeliveryID := deliverymeta.PlanHash(*plan), plan.DeliveryID()
	var result Result
	for range 5 {
		result, err = service.observe(ctx, f.binding, first.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if result.PiResult != nil {
			break
		}
	}
	_, details, err := service.load(ctx, f.binding, first.ID)
	if err != nil || details.Batch.Progress.Producer == nil {
		t.Fatal("original context not retained", err)
	}
	retained := details.Batch.Progress.Producer
	if deliverymeta.PlanHash(*retained) != originalPlanHash || retained.DeliveryID() != originalDeliveryID || retained.Task != plan.Task {
		t.Fatal("completion replaced declaration, context digest or original identity")
	}
	if result.PiResult == nil || result.PiResult.Outcome != "succeeded" || result.Delivery == nil || result.Delivery.State != "pending" || result.Delivery.Reference != nil || result.ArtifactHash != "" || len(f.assets.data) != 0 || f.batch.runs != 1 {
		t.Fatal("fixture execution success bypassed missing committed ledger or reran task")
	}
	t.Log("native task entry fixture: exact .png declaration retained through completed receipt; original PlanHash/deliveryID unchanged; empty ledger remains pending; dispatch=1; actual Pi/HTTP/model calls=0")
}
