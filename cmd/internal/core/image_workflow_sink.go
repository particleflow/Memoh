package core

import (
	"context"
	"errors"
	"log/slog"

	"github.com/felinics/memoh/internal/albumwriteback"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/imageworkflow"
)

// ImageWorkflowDeliveryCommitSink is the workflow-aware replacement for
// AlbumDeliveryCommitSink: instead of resolving the owner's single bare target,
// it resolves the output pairing from the delivery's execution lineage and
// falls back to the bare target when no run owns the session.
//
// Only upload deliveries are products, as in AlbumDeliveryCommitSink, and every
// failure is logged and dropped: the commit already succeeded and must not be
// affected.
func ImageWorkflowDeliveryCommitSink(log *slog.Logger, workflows *imageworkflow.Service, writebacks *albumwriteback.Service) computertask.DeliverySink {
	if workflows == nil || writebacks == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String("component", "image_workflow_delivery_sink"))
	return func(ctx context.Context, identity computertask.DeliveryIdentity, ref computertask.DeliveryRequest) {
		// A download delivery writes onto the device, not onto the NAS: its bytes
		// are not a processing product, so it is never queued for an album.
		if ref.Direction != "upload" {
			return
		}
		userID, botID := identity.Binding.UserID, identity.Binding.BotID
		pairKey, err := workflows.ResolveOutputPair(ctx, userID, botID, identity.Binding.SessionID)
		if err != nil {
			if !errors.Is(err, imageworkflow.ErrPairUnavailable) {
				log.Warn("image workflow attribution failed", slog.String("operation_id", identity.OperationID))
			}
			return
		}
		if _, err := writebacks.EnqueueVerified(ctx, userID, botID, pairKey, albumwriteback.VerifiedItem{
			ContentHash: ref.ContentHash,
			SizeBytes:   ref.SizeBytes,
			Mime:        ref.Mime,
		}); err != nil {
			log.Warn("album write-back enqueue failed", slog.String("operation_id", identity.OperationID))
		}
	}
}
