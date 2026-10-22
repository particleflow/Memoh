package handlers

import (
	"context"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/media"
)

// readUpload keeps the public upload wire contract. Only task-bound routes
// consult the authorized operation ledger before the expiring staging receipt.
// A proof is the server's prior ready result, not a lookup by content hash and
// not a committed association. Commit still verifies Media and current grants.
func (h *ComputerTransferHandler) readUpload(ctx context.Context, p computertask.VerifiedTransfer, id string, complete bool) (media.Upload, error) {
	proof, found, err := h.base.TransferReady(ctx)
	if err != nil {
		return media.Upload{}, deliveryHTTPError(err)
	}
	if found {
		if proof.ID != id {
			return media.Upload{}, deliveryHTTPError(computertask.ErrDeliveryBinding)
		}
		return proof, nil
	}
	read := h.files.mediaService.Uploads().Get
	if complete {
		read = h.files.mediaService.Uploads().Complete
	}
	receipt, err := read(ctx, p.Identity.Binding.UserID, p.Identity.Binding.BotID, id)
	if err != nil {
		return media.Upload{}, resumableError(err)
	}
	if receipt.State == "ready" {
		// Do not acknowledge ready until its exact task/owner/ref proof is
		// durable. An unknown DB commit result is queried on the next request.
		if err := h.base.RecordTransferReady(ctx, receipt); err != nil {
			return media.Upload{}, deliveryHTTPError(err)
		}
	}
	return receipt, nil
}

func readMediaUpload(c echo.Context, store *media.UploadStore, owner, bot, id string, complete bool) (media.Upload, error) {
	if scope := taskTransfer(c); scope != nil {
		return scope.handler.readUpload(c.Request().Context(), scope.principal, id, complete)
	}
	read := store.Get
	if complete {
		read = store.Complete
	}
	receipt, err := read(c.Request().Context(), owner, bot, id)
	if err != nil {
		return receipt, resumableError(err)
	}
	return receipt, nil
}

func finishTaskDelivery(c echo.Context, out computertask.DeliveryOutcome) error {
	scope := taskTransfer(c)
	if scope == nil {
		return nil
	}
	if err := checkTaskTransferResponse(c); err != nil {
		return err
	}
	if out.Outcome != "committed" || scope.principal.Reference.Direction != "upload" {
		return nil
	}
	proof, found, err := scope.handler.base.CommittedTransferReady(c.Request().Context())
	if err != nil {
		return deliveryHTTPError(err)
	}
	if !found {
		// A legacy committed decision is still authoritative for association,
		// but does not invent missing receipt evidence for filesystem cleanup.
		return nil
	}
	p := scope.principal
	if err := scope.handler.files.mediaService.Uploads().RetireTaskReceipt(c.Request().Context(), p.Identity.Binding.UserID, p.Identity.Binding.BotID, proof); err != nil {
		return resumableError(err)
	}
	return checkTaskTransferResponse(c)
}
