package core

import (
	"context"
	"errors"
	"log/slog"

	"github.com/felinics/memoh/internal/albumpairs"
	"github.com/felinics/memoh/internal/albumwriteback"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

// provideAlbumPairService wires the pairing archive to the same database the
// write-back queue uses. The archive stores intent only: the album itself stays
// on the phone.
func provideAlbumPairService(log *slog.Logger, store *postgresstore.Store) *albumpairs.Service {
	return albumpairs.NewService(log, store)
}

// AlbumDeliveryCommitSink queues a freshly committed product for the paired
// phone album. It is the whole automatic handoff: the delivery commit already
// knows the Bot, the user and a fully verified content hash, so one pairing
// lookup decides whether anything is queued.
//
// No pairing means no work: nothing is written, no placeholder row is created
// and no user-visible text is produced. A lookup or write failure is logged and
// dropped — the commit itself already succeeded and must not be affected.
func AlbumDeliveryCommitSink(log *slog.Logger, pairs *albumpairs.Service, writebacks *albumwriteback.Service) computertask.DeliverySink {
	if pairs == nil || writebacks == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String("component", "album_delivery_sink"))
	return func(ctx context.Context, identity computertask.DeliveryIdentity, ref computertask.DeliveryRequest) {
		// A download delivery writes onto the device, not onto the NAS: its bytes
		// are not a processing product, so it is never queued for an album.
		if ref.Direction != "upload" {
			return
		}
		userID, botID := identity.Binding.UserID, identity.Binding.BotID
		pair, err := pairs.Resolve(ctx, userID, botID)
		if err != nil {
			if !errors.Is(err, albumpairs.ErrNotFound) {
				log.Warn("album pairing lookup failed", slog.String("operation_id", identity.OperationID))
			}
			return
		}
		if _, err := writebacks.EnqueueVerified(ctx, userID, botID, pair.PairKey, albumwriteback.VerifiedItem{
			ContentHash: ref.ContentHash,
			SizeBytes:   ref.SizeBytes,
			Mime:        ref.Mime,
		}); err != nil {
			log.Warn("album write-back enqueue failed", slog.String("operation_id", identity.OperationID))
		}
	}
}
