package core

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/felinics/memoh/internal/config"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/imageworkflow"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/schedule"
)

// provideImageWorkflowService wires the workflow surface to the existing
// postgres store, media service and scheduler. Nothing here is a second
// execution engine: a trigger creates the same one-shot schedule a user would,
// and the existing batch/endpoint/write-back chain runs it.
func provideImageWorkflowService(log *slog.Logger, cfg config.Config, store *postgresstore.Store, mediaService *media.Service, schedules *schedule.Service) *imageworkflow.Service {
	if store == nil {
		// No database means no archive to serve; the handler answers 503 and the
		// delivery sink is not registered.
		return nil
	}
	return imageworkflow.NewService(log, imageworkflow.Deps{
		Workflows:     store,
		Runs:          store,
		Pairs:         store,
		Lineage:       store,
		Graph:         store,
		Schedules:     imageWorkflowSchedules{schedules: schedules},
		Assets:        mediaService,
		InputRoot:     filepath.Join(cfg.Workspace.DataRootPath(), "workflows"),
		OriginalsRoot: cfg.Files.OriginalsRoot,
	})
}

// imageWorkflowSchedules translates the workflow one-shot request onto the
// existing schedule service, field for field.
type imageWorkflowSchedules struct {
	schedules *schedule.Service
}

func (a imageWorkflowSchedules) CreateOneShot(ctx context.Context, botID string, req imageworkflow.ScheduleRequest) (string, error) {
	if a.schedules == nil {
		return "", context.Canceled
	}
	maxCalls := req.MaxCalls
	created, err := a.schedules.Create(ctx, botID, schedule.CreateRequest{
		Name:        req.Name,
		Description: req.Description,
		Pattern:     req.Pattern,
		MaxCalls:    schedule.NullableInt{Value: &maxCalls, Set: true},
		Command:     req.Command,
		ExecutionConfig: schedule.ExecutionConfig{
			RunTarget: req.RunTarget,
		},
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}
