package imageworkflow

import (
	"context"
	"log/slog"
)

// Deps is what the service needs. Small interfaces, so a contract test can
// supply one fake per concern and an implementation can satisfy them from the
// existing postgres store.
type Deps struct {
	Workflows WorkflowStore
	Runs      RunStore
	Pairs     PairStore
	Lineage   LineageStore
	Graph     GraphStore
	Schedules ScheduleCreator
	Assets    AssetResolver
	// InputRoot is the absolute directory the input pairing labels are created
	// under; the input directory of one workflow is InputRoot/<input_pair_key>.
	InputRoot string
	// OriginalsRoot is the operator-owned canonical original root, used as the
	// hardlink source prefix.
	OriginalsRoot string
}

// Service owns workflow configuration, triggers, routing and attribution.
type Service struct {
	deps   Deps
	logger *slog.Logger
}

// NewService wires the contract surface. A stub service is still constructible
// so the tests can assert routing and validation before any feature exists.
func NewService(log *slog.Logger, deps Deps) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{deps: deps, logger: log.With(slog.String("service", "image_workflow"))}
}

// Create registers a workflow in state active and mints both pairing labels.
func (s *Service) Create(ctx context.Context, userID string, in CreateInput) (Workflow, error) {
	// TODO(F3): validate the name and instruction, mint the two labels, insert
	// the row, and translate a name conflict into ErrNameTaken.
	return Workflow{}, ErrNotImplemented
}

// List returns every workflow the owner holds, oldest first.
func (s *Service) List(ctx context.Context, userID string) ([]Workflow, error) {
	// TODO(F3): list rows and project them.
	return nil, ErrNotImplemented
}

// Get returns one workflow with its newest runs and per-state counts.
func (s *Service) Get(ctx context.Context, userID, workflowID string) (WorkflowDetail, error) {
	// TODO(F3): read the row, the newest runs and the counts.
	return WorkflowDetail{}, ErrNotImplemented
}

// Update changes the name and/or instruction. Labels are immutable here.
func (s *Service) Update(ctx context.Context, userID, workflowID string, in UpdateInput) (Workflow, error) {
	// TODO(F3): validate, update, translate a name conflict.
	return Workflow{}, ErrNotImplemented
}

// Revoke soft-deletes the workflow and revokes both workflow pairing rows in
// the same transaction. Runs, products and queued write-backs are untouched.
func (s *Service) Revoke(ctx context.Context, userID, workflowID string) (Workflow, error) {
	// TODO(F3/F7): set state revoked and revoke the two pairing rows atomically.
	return Workflow{}, ErrNotImplemented
}

// Pause stops new triggers from arming and removes the directory route.
func (s *Service) Pause(ctx context.Context, userID, workflowID string) (Workflow, error) {
	// TODO(F7): set state paused, idempotently.
	return Workflow{}, ErrNotImplemented
}

// Resume returns the workflow to active and arms every held run.
func (s *Service) Resume(ctx context.Context, userID, workflowID string) (ResumeResult, error) {
	// TODO(F7): arm every held run in one transaction, then set state active.
	return ResumeResult{}, ErrNotImplemented
}

// Trigger accepts one batch. It ingests directory files, resolves upload
// hashes, applies the fingerprint idempotency, materializes the input
// directory entries, and creates the one-shot schedule unless the workflow is
// paused (held) or the pairing is missing (error, no run).
func (s *Service) Trigger(ctx context.Context, userID, botID, workflowID string, req TriggerRequest) (TriggerResult, error) {
	// TODO(F4/F5/F8): the whole trigger path.
	return TriggerResult{}, ErrNotImplemented
}

// Routes is the directory watcher's route table.
func (s *Service) Routes(ctx context.Context, userID string) ([]Route, error) {
	// TODO(F9): one route per live workflow plus paused ones with their state.
	return nil, ErrNotImplemented
}

// ResolveOutputPair attributes one committed product to an output pairing.
// It walks the delivery session up the parent chain (bounded by
// MaxLineageDepth) to a run, then reads the workflow's output label; without a
// lineage hit it falls back to the bare target for (user, bot).
func (s *Service) ResolveOutputPair(ctx context.Context, userID, botID, sessionID string) (string, error) {
	// TODO(F6): the lineage walk and the bare-target fallback.
	return "", ErrNotImplemented
}
