package core

import (
	"context"
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
//
// TODO(F6): resolve the output pair through imageworkflow.Service.ResolveOutputPair
// and enqueue the verified hash into that pairing label, then register this sink
// in place of AlbumDeliveryCommitSink in transfer_providers.go.
func ImageWorkflowDeliveryCommitSink(log *slog.Logger, workflows *imageworkflow.Service, writebacks *albumwriteback.Service) computertask.DeliverySink {
	if workflows == nil || writebacks == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String("component", "image_workflow_delivery_sink"))
	return func(ctx context.Context, identity computertask.DeliveryIdentity, ref computertask.DeliveryRequest) {
		if ref.Direction != "upload" {
			return
		}
		// Stub: the lineage resolution is not implemented yet, so nothing is
		// queued. The contract test in this package fails until this body exists.
	}
}
