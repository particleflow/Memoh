package main

import (
	"context"

	"go.uber.org/fx"

	"github.com/felinics/memoh/internal/config"
	"github.com/felinics/memoh/internal/providers"
)

// provideCatalogTargetResolver binds the worker to the single explicit,
// persisted non-secret UUID. It checks the current provider row each time.
func provideCatalogTargetResolver(cfg config.Config, service *providers.Service) providers.CatalogTargetResolver {
	return func(ctx context.Context) (string, error) {
		return service.ResolveCatalogSyncTarget(ctx, cfg.ModelCatalogSync.ProviderID)
	}
}

type catalogSyncLifecycleParams struct {
	fx.In
	Lifecycle fx.Lifecycle
	Service   *providers.Service
	// The production graph supplies a resolver backed by the persisted UUID.
	// Optional supports minimal Fx graphs that do not configure this worker.
	Resolver providers.CatalogTargetResolver `optional:"true"`
}

func startCatalogSync(p catalogSyncLifecycleParams) {
	if p.Resolver == nil {
		return
	}
	installCatalogSyncLifecycle(p.Lifecycle, p.Service.RunModelCatalogSync, p.Resolver)
}

// installCatalogSyncLifecycle owns cancellation and joins the worker before
// returning from shutdown. The startup hook's context is deliberately not used
// as the worker's lifetime: Fx may cancel it as soon as startup completes.
func installCatalogSyncLifecycle(lc fx.Lifecycle, run func(context.Context, providers.CatalogTargetResolver), resolver providers.CatalogTargetResolver) {
	var cancel context.CancelFunc
	var done chan struct{}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			workerCtx, stop := context.WithCancel(context.Background())
			cancel = stop
			done = make(chan struct{})
			go func() { defer close(done); run(workerCtx, resolver) }()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel == nil {
				return nil
			}
			cancel()
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
}
