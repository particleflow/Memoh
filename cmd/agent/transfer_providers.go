package main

import (
	"context"
	"errors"
	"log/slog"

	"go.uber.org/fx"

	coremodule "github.com/felinics/memoh/cmd/internal/core"
	"github.com/felinics/memoh/internal/albumwriteback"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/config"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/imageworkflow"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/userruntime"
)

func provideTransferCredentials(store *postgresstore.Store, cfg config.Config) *userruntime.TransferCredentials {
	return userruntime.NewTransferCredentials(store, cfg.Files.DeviceTransfersEnabled)
}

func provideComputerTransferHandler(lc fx.Lifecycle, cfg config.Config, store *postgresstore.Store, assets *media.Service, files *handlers.MessageHandler, verifier *userruntime.TransferCredentials, delegations *botdelegation.Service, workflows *imageworkflow.Service, writebacks *albumwriteback.Service, log *slog.Logger) (*handlers.ComputerTransferHandler, error) {
	if !cfg.Files.DeviceTransfersEnabled {
		return handlers.NewDeviceComputerTransferHandler(context.Background(), nil, nil, nil, log)
	}
	if cfg.Files.OriginalsRoot == "" {
		return nil, errors.New("device transfers require configured file originals storage")
	}
	ctx, cancel := context.WithCancel(context.Background())
	// A verified endpoint delivery commit is the only idle-time completion
	// signal for a PC task; it wakes the existing result-delivery scan. The sink
	// is separate: it is the one place a finished product is queued for the
	// user's designated phone album, resolved from the delivery's execution
	// lineage and falling back to the bare target, and it never changes the
	// commit result.
	deliveries := computertask.NewPostgresDeliveryService(store, assets).
		WithCommitObserver(coremodule.ComputerDeliveryCommitWake(delegations)).
		WithDeliverySink(coremodule.ImageWorkflowDeliveryCommitSink(log, workflows, writebacks))
	h, err := handlers.NewDeviceComputerTransferHandler(ctx, deliveries, files, handlers.RuntimeDeviceTransferVerifier{Credentials: verifier}, log)
	if err != nil {
		cancel()
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { cancel(); h.Close(); return nil }})
	return h, nil
}
