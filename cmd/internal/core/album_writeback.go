package core

import (
	"log/slog"

	"github.com/felinics/memoh/internal/albumwriteback"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/media"
)

// provideAlbumWritebackService wires the phone album write-back queue to the
// existing content-addressed media service: enqueue resolves a hash against
// the Bot's own originals, so the queue never introduces a second identity.
func provideAlbumWritebackService(log *slog.Logger, store *postgresstore.Store, mediaService *media.Service) *albumwriteback.Service {
	return albumwriteback.NewService(log, store, mediaService)
}
