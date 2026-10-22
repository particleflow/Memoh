package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/transferclient"
)

func TestProducerCancelStopsRealUploadHandler(t *testing.T) {
	for _, stage := range []string{"before-close", "already-enqueued"} {
		t.Run(stage, func(t *testing.T) {
			f, _, _, client := taskTransferHTTPFixture(t, false)
			var d computertask.Details
			_ = json.Unmarshal(f.r.Details, &d)
			b := f.id.Binding
			d.Batch.Progress.Producer = &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: f.id.OperationID, BotID: b.BotID, UserID: b.UserID, SessionID: b.SessionID, TargetID: b.TargetID, RunID: b.RunID, Attempt: f.id.Attempt, Generation: f.id.Generation, RootHash: deliverymeta.RootHash(`C:\Fixture`)}, Artifact: deliverymeta.Artifact{SourceRef: "result", Mime: f.ref.Mime}}
			f.r.Details, _ = json.Marshal(d)
			spec := transferclient.Spec{SizeBytes: f.ref.SizeBytes, SHA256: f.ref.ContentHash, Mime: f.ref.Mime}
			receipt := ""
			if stage == "already-enqueued" {
				got, err := client.Create(t.Context(), b.BotID, spec)
				if err != nil {
					t.Fatal(err)
				}
				receipt = got.ID
			}
			// No request or async validator is active while this fixture persists cancel.
			f.r.CancelRequested = true
			f.r.Revision++
			if _, err := client.Create(t.Context(), b.BotID, spec); !producerDenied(err) {
				t.Fatal("exit hook created upload after cancel", err)
			}
			if receipt != "" {
				if _, err := client.Patch(t.Context(), b.BotID, receipt, 0, []byte("fixture")); !producerDenied(err) {
					t.Fatal("queued upload appended after cancel", err)
				}
			}
			var stored map[string]json.RawMessage
			_ = json.Unmarshal(f.r.Details, &stored)
			if stage == "before-close" && len(stored["delivery_ledger"]) != 0 {
				t.Fatal("canceled exit hook registered delivery")
			}
		})
	}
}
func producerDenied(err error) bool {
	var h *transferclient.HTTPError
	return errors.As(err, &h) && h.Status == http.StatusForbidden
}
