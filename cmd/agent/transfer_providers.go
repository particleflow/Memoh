package main

import (
	"context"
	"errors"
	"log/slog"

	"go.uber.org/fx"

	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/config"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/userruntime"
)

func provideTransferCredentials(store *postgresstore.Store, cfg config.Config) *userruntime.TransferCredentials {
	return userruntime.NewTransferCredentials(store, cfg.Files.DeviceTransfersEnabled)
}

func provideComputerTransferHandler(lc fx.Lifecycle, cfg config.Config, store *postgresstore.Store, assets *media.Service, files *handlers.MessageHandler, verifier *userruntime.TransferCredentials, log *slog.Logger) (*handlers.ComputerTransferHandler, error) {
	if !cfg.Files.DeviceTransfersEnabled {
		return handlers.NewDeviceComputerTransferHandler(context.Background(), nil, nil, nil, log)
	}
	if cfg.Files.OriginalsRoot == "" {
		return nil, errors.New("device transfers require configured file originals storage")
	}
	ctx, cancel := context.WithCancel(context.Background())
	h, err := handlers.NewDeviceComputerTransferHandler(ctx, computertask.NewPostgresDeliveryService(store, assets), files, handlers.RuntimeDeviceTransferVerifier{Credentials: verifier}, log)
	if err != nil {
		cancel()
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { cancel(); h.Close(); return nil }})
	return h, nil
}
