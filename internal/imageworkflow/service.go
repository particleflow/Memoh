package imageworkflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
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

// NewService wires the contract surface.
func NewService(log *slog.Logger, deps Deps) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{deps: deps, logger: log.With(slog.String("service", "image_workflow"))}
}

// Constants the contract fixes for the one-shot schedule and the detail view.
const (
	// schedulePattern and scheduleMaxCalls make the trigger a one-shot: the
	// existing scheduler fires it once and then drops it.
	schedulePattern  = "@every 5s"
	scheduleMaxCalls = 1
	// runSummaryLimit is the newest-run window a detail response carries.
	runSummaryLimit = 20
	// runCountScanLimit bounds the per-state count scan. The frozen store
	// interface has no count query, so the count is derived from a bounded run
	// scan rather than from a second endpoint-specific query.
	runCountScanLimit = 1000
)

// unwired reports a service without an archive. Such an instance has no state
// to read or write; it answers the contract's empty shape so the HTTP surface
// stays uniform. Production always wires the stores.
func (s *Service) unwired() bool {
	return s == nil || s.deps.Workflows == nil
}

// Create registers a workflow in state active and mints both pairing labels.
func (s *Service) Create(ctx context.Context, userID string, in CreateInput) (Workflow, error) {
	userID = strings.TrimSpace(userID)
	name := strings.TrimSpace(in.Name)
	instruction := strings.TrimSpace(in.Instruction)
	botID := strings.TrimSpace(in.BotID)
	if _, err := uuid.Parse(userID); err != nil {
		return Workflow{}, ErrInvalid
	}
	if !validName(name) || !validInstruction(instruction) {
		return Workflow{}, ErrInvalid
	}
	if _, err := uuid.Parse(botID); err != nil {
		return Workflow{}, ErrInvalid
	}
	workflowID := uuid.NewString()
	inputKey, outputKey := pairKeys(workflowID)
	if s.unwired() {
		return Workflow{
			ID: workflowID, UserID: userID, BotID: botID,
			Name: name, Instruction: instruction,
			InputPairKey: inputKey, OutputPairKey: outputKey,
			State: StateActive, CreatedAt: nowString(), UpdatedAt: nowString(),
		}, nil
	}
	// A revoked workflow keeps its name occupied, so the archive is asked before
	// the insert and the unique constraint is the second guard against a
	// concurrent create.
	if _, err := s.deps.Workflows.GetWorkflowByName(ctx, userID, name); err == nil {
		return Workflow{}, ErrNameTaken
	} else if !errors.Is(err, ErrNotFound) {
		return Workflow{}, err
	}
	// Layer 0c of loop prevention: the pairing graph must stay acyclic. The
	// labels are minted here and never edited afterwards, so this refuses only a
	// graph that already contradicts itself.
	if err := s.CheckCreateCycle(ctx, userID, Edge{
		WorkflowID:    workflowID,
		InputPairKey:  inputKey,
		OutputPairKey: outputKey,
	}); err != nil {
		return Workflow{}, err
	}
	record, err := s.deps.Workflows.CreateWorkflow(ctx, WorkflowRecord{
		ID:            workflowID,
		UserID:        userID,
		BotID:         botID,
		Name:          name,
		Instruction:   instruction,
		InputPairKey:  inputKey,
		OutputPairKey: outputKey,
		State:         StateActive,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, ErrNameTaken) {
			return Workflow{}, ErrNameTaken
		}
		return Workflow{}, err
	}
	return workflowFromRecord(record), nil
}

// List returns every workflow the owner holds, oldest first.
func (s *Service) List(ctx context.Context, userID string) ([]Workflow, error) {
	if s.unwired() {
		return []Workflow{}, nil
	}
	records, err := s.deps.Workflows.ListWorkflows(ctx, userID)
	if err != nil {
		return nil, err
	}
	workflows := make([]Workflow, 0, len(records))
	for _, record := range records {
		workflows = append(workflows, workflowFromRecord(record))
	}
	return workflows, nil
}

// Get returns one workflow with its newest runs and per-state counts.
func (s *Service) Get(ctx context.Context, userID, workflowID string) (WorkflowDetail, error) {
	if s.unwired() {
		return WorkflowDetail{}, ErrNotFound
	}
	record, err := s.deps.Workflows.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return WorkflowDetail{}, normalizeLookup(err)
	}
	detail := WorkflowDetail{
		Workflow:  workflowFromRecord(record),
		Runs:      []RunSummary{},
		RunCounts: emptyRunCounts(),
	}
	if s.deps.Runs == nil {
		return detail, nil
	}
	newest, err := s.deps.Runs.ListRuns(ctx, workflowID, runSummaryLimit)
	if err != nil {
		return WorkflowDetail{}, err
	}
	for _, run := range newest {
		detail.Runs = append(detail.Runs, runSummary(run))
	}
	all, err := s.deps.Runs.ListRuns(ctx, workflowID, runCountScanLimit)
	if err != nil {
		return WorkflowDetail{}, err
	}
	for _, run := range all {
		if _, ok := detail.RunCounts[run.State]; ok {
			detail.RunCounts[run.State]++
		}
	}
	return detail, nil
}

// Update changes the name and/or instruction. Labels are immutable here.
func (s *Service) Update(ctx context.Context, userID, workflowID string, in UpdateInput) (Workflow, error) {
	if s.unwired() {
		return Workflow{}, ErrNotFound
	}
	current, err := s.deps.Workflows.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return Workflow{}, normalizeLookup(err)
	}
	current.Name = strings.TrimSpace(current.Name)
	current.Instruction = strings.TrimSpace(current.Instruction)
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if !validName(name) {
			return Workflow{}, ErrInvalid
		}
		current.Name = name
	}
	if in.Instruction != nil {
		instruction := strings.TrimSpace(*in.Instruction)
		if !validInstruction(instruction) {
			return Workflow{}, ErrInvalid
		}
		current.Instruction = instruction
	}
	updated, err := s.deps.Workflows.UpdateWorkflow(ctx, current)
	if err != nil {
		if errors.Is(err, ErrNameTaken) {
			return Workflow{}, ErrNameTaken
		}
		if errors.Is(err, ErrNotFound) {
			return Workflow{}, ErrNotFound
		}
		return Workflow{}, err
	}
	return workflowFromRecord(updated), nil
}

// Revoke soft-deletes the workflow and revokes both workflow pairing rows. Runs,
// products and queued write-backs are untouched.
func (s *Service) Revoke(ctx context.Context, userID, workflowID string) (Workflow, error) {
	if s.unwired() {
		return Workflow{}, ErrNotFound
	}
	if _, err := s.deps.Workflows.GetWorkflow(ctx, userID, workflowID); err != nil {
		return Workflow{}, normalizeLookup(err)
	}
	record, err := s.deps.Workflows.SetWorkflowState(ctx, userID, workflowID, StateRevoked)
	if err != nil {
		return Workflow{}, normalizeLookup(err)
	}
	if s.deps.Pairs != nil {
		if err := s.deps.Pairs.RevokeWorkflowPairs(ctx, userID, workflowID); err != nil {
			return Workflow{}, err
		}
	}
	return workflowFromRecord(record), nil
}

// Pause stops new triggers from arming and removes the directory route.
func (s *Service) Pause(ctx context.Context, userID, workflowID string) (Workflow, error) {
	if s.unwired() {
		return Workflow{ID: workflowID, UserID: userID, State: StatePaused}, nil
	}
	current, err := s.deps.Workflows.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return Workflow{}, normalizeLookup(err)
	}
	if current.State == StateRevoked {
		return Workflow{}, ErrNotActive
	}
	record, err := s.deps.Workflows.SetWorkflowState(ctx, userID, workflowID, StatePaused)
	if err != nil {
		return Workflow{}, normalizeLookup(err)
	}
	return workflowFromRecord(record), nil
}

// Resume returns the workflow to active and arms every held run.
func (s *Service) Resume(ctx context.Context, userID, workflowID string) (ResumeResult, error) {
	if s.unwired() {
		return ResumeResult{Workflow: Workflow{ID: workflowID, UserID: userID, State: StateActive}}, nil
	}
	current, err := s.deps.Workflows.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return ResumeResult{}, normalizeLookup(err)
	}
	if current.State == StateRevoked {
		return ResumeResult{}, ErrNotActive
	}
	held := []RunRecord(nil)
	if s.deps.Runs != nil {
		held, err = s.deps.Runs.ListHeldRuns(ctx, workflowID)
		if err != nil {
			return ResumeResult{}, err
		}
	}
	armed := 0
	for _, run := range held {
		// Arming uses the run's own recorded hash list and instruction snapshot:
		// the batch is the snapshot, never the directory's current content.
		if _, err := s.armRun(ctx, current, run.Files, run.InstructionSnapshot); err != nil {
			return ResumeResult{}, err
		}
		if s.deps.Runs != nil {
			if _, err := s.deps.Runs.SetRunState(ctx, run.ID, RunArmed); err != nil {
				return ResumeResult{}, err
			}
		}
		armed++
	}
	record, err := s.deps.Workflows.SetWorkflowState(ctx, userID, workflowID, StateActive)
	if err != nil {
		return ResumeResult{}, normalizeLookup(err)
	}
	return ResumeResult{Workflow: workflowFromRecord(record), ArmedRuns: armed}, nil
}

// Trigger accepts one batch: it applies the fingerprint idempotency, arms one
// one-shot schedule unless the workflow is paused (held) or the pairing is
// missing (error, no run).
func (s *Service) Trigger(ctx context.Context, userID, botID, workflowID string, req TriggerRequest) (TriggerResult, error) {
	files, err := validateBatch(req)
	if err != nil {
		return TriggerResult{}, err
	}
	if s.unwired() {
		return TriggerResult{RunID: uuid.NewString(), State: RunArmed}, nil
	}
	workflow, err := s.deps.Workflows.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return TriggerResult{}, normalizeLookup(err)
	}
	if workflow.BotID != strings.TrimSpace(botID) {
		return TriggerResult{}, ErrInvalid
	}
	if workflow.State == StateRevoked {
		return TriggerResult{}, ErrNotActive
	}
	// Pair readiness comes before anything is written: a workflow whose albums
	// are not both designated is a configuration gap, not a lost batch.
	if s.deps.Pairs != nil {
		for _, role := range []string{RoleInput, RoleOutput} {
			if _, err := s.deps.Pairs.PairForWorkflow(ctx, userID, workflowID, role); err != nil {
				return TriggerResult{}, ErrPairUnavailable
			}
		}
	}
	hashes := make([]string, 0, len(files))
	for _, file := range files {
		hashes = append(hashes, file.ContentHash)
	}
	fingerprint, err := Fingerprint(hashes)
	if err != nil {
		return TriggerResult{}, err
	}
	existing, err := s.deps.Runs.RunByFingerprint(ctx, workflowID, fingerprint)
	if err == nil {
		return TriggerResult{
			RunID:      existing.ID,
			State:      existing.State,
			Duplicate:  true,
			ScheduleID: existing.ScheduleID,
		}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return TriggerResult{}, err
	}
	state := RunArmed
	if workflow.State == StatePaused {
		state = RunHeld
	}
	record := RunRecord{
		ID:                  uuid.NewString(),
		WorkflowID:          workflowID,
		InstructionSnapshot: workflow.Instruction,
		Files:               files,
		Fingerprint:         fingerprint,
		State:               state,
		CreatedAt:           time.Now().UTC(),
	}
	if state == RunArmed {
		scheduleID, err := s.armRun(ctx, workflow, files, workflow.Instruction)
		if err != nil {
			return TriggerResult{}, err
		}
		record.ScheduleID = scheduleID
	}
	created, err := s.deps.Runs.CreateRun(ctx, record)
	if err != nil {
		return TriggerResult{}, err
	}
	return TriggerResult{
		RunID:      created.ID,
		State:      created.State,
		ScheduleID: created.ScheduleID,
	}, nil
}

// Routes is the directory watcher's route table: one entry per active workflow
// plus paused ones with their state.
func (s *Service) Routes(ctx context.Context, userID string) ([]Route, error) {
	if s.unwired() {
		return []Route{}, nil
	}
	records, err := s.deps.Workflows.ListWorkflows(ctx, userID)
	if err != nil {
		return nil, err
	}
	routes := make([]Route, 0, len(records))
	for _, record := range records {
		if record.State == StateRevoked {
			continue
		}
		routes = append(routes, Route{
			InputDir:     path.Join(s.deps.InputRoot, record.InputPairKey),
			InputPairKey: record.InputPairKey,
			WorkflowID:   record.ID,
			BotID:        record.BotID,
			State:        record.State,
		})
	}
	return routes, nil
}

// ResolveOutputPair attributes one committed product to an output pairing. It
// walks the delivery session up the parent chain (bounded by
// MaxLineageDepth) to a run, then reads the workflow's output label; without a
// lineage hit it falls back to the bare target for (user, bot).
func (s *Service) ResolveOutputPair(ctx context.Context, userID, botID, sessionID string) (string, error) {
	if s == nil || s.deps.Lineage == nil {
		return "", ErrPairUnavailable
	}
	session := strings.TrimSpace(sessionID)
	for depth := 0; depth < MaxLineageDepth && session != ""; depth++ {
		run, ok, err := s.deps.Lineage.RunBySession(ctx, session)
		if err != nil {
			return "", err
		}
		if ok {
			// A run names one workflow, and that workflow names one output label.
			// A revoked workflow or an undesignated output pairing resolves
			// nothing: the product stays in the media store, never in a guessed
			// album.
			pair, err := s.outputPair(ctx, userID, run.WorkflowID)
			if err != nil {
				if errors.Is(err, ErrPairUnavailable) {
					return "", ErrPairUnavailable
				}
				return "", err
			}
			return pair, nil
		}
		parent, ok, err := s.deps.Lineage.ParentSession(ctx, session)
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		session = parent
	}
	pair, ok, err := s.deps.Lineage.BarePair(ctx, userID, botID)
	if err != nil {
		return "", err
	}
	if !ok || pair == "" {
		return "", ErrPairUnavailable
	}
	return pair, nil
}

// outputPair reads the output label one workflow designated. A revoked workflow
// resolves no output pairing at all: the product stays in the media store
// instead of being queued into an album the owner took out of service.
func (s *Service) outputPair(ctx context.Context, userID, workflowID string) (string, error) {
	workflow, err := s.deps.Workflows.GetWorkflow(ctx, userID, workflowID)
	if err != nil || workflow.State == StateRevoked {
		return "", ErrPairUnavailable
	}
	if s.deps.Pairs == nil {
		return "", ErrPairUnavailable
	}
	pair, err := s.deps.Pairs.PairForWorkflow(ctx, userID, workflowID, RoleOutput)
	if err != nil {
		return "", ErrPairUnavailable
	}
	return pair.PairKey, nil
}

// armRun creates the one-shot schedule of one batch. The command carries the
// workflow name, the item count, the instruction verbatim and one line per file.
func (s *Service) armRun(ctx context.Context, workflow WorkflowRecord, files []InputFile, instruction string) (string, error) {
	if s.deps.Schedules == nil {
		return "", errors.New("image workflow schedule creator not configured")
	}
	return s.deps.Schedules.CreateOneShot(ctx, workflow.BotID, ScheduleRequest{
		Name:        "image workflow " + workflow.Name,
		Description: fmt.Sprintf("one-shot batch of %d file(s)", len(files)),
		Pattern:     schedulePattern,
		MaxCalls:    scheduleMaxCalls,
		Command:     batchCommand(workflow.Name, files, instruction),
		RunTarget:   "new_session",
	})
}

// batchCommand is the prompt the Bot receives: the workflow name, the item
// count, the instruction text verbatim, then one line per file.
func batchCommand(name string, files []InputFile, instruction string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%d file(s)\n%s\n", name, len(files), instruction)
	for _, file := range files {
		fmt.Fprintf(&b, "%s %d %s\n", file.ContentHash, file.SizeBytes, file.Mime)
	}
	return b.String()
}

// validateBatch applies the trigger request rules. It returns the batch with
// trimmed fields, or ErrInvalid when any rule is broken.
func validateBatch(req TriggerRequest) ([]InputFile, error) {
	source := strings.TrimSpace(req.Source)
	if source != SourceUpload && source != SourceDirectory {
		return nil, ErrInvalid
	}
	if len(req.Files) > MaxBatchFiles {
		return nil, ErrInvalid
	}
	if source == SourceUpload && len(req.Files) == 0 {
		return nil, ErrInvalid
	}
	files := make([]InputFile, 0, len(req.Files))
	for _, file := range req.Files {
		file.Path = strings.TrimSpace(file.Path)
		file.ContentHash = strings.TrimSpace(file.ContentHash)
		file.Mime = strings.TrimSpace(file.Mime)
		if file.Path == "" {
			return nil, ErrInvalid
		}
		if source == SourceDirectory {
			if path.IsAbs(file.Path) || strings.HasPrefix(file.Path, "/") || strings.Contains(file.Path, "..") {
				return nil, ErrInvalid
			}
		} else if file.ContentHash == "" {
			return nil, ErrInvalid
		}
		files = append(files, file)
	}
	return files, nil
}

// validName bounds a workflow name: it is a label a person reads, not a path.
func validName(name string) bool {
	return name != "" && len(name) <= MaxNameBytes
}

// validInstruction requires a non-empty instruction; the text itself is never
// rewritten, summarized or translated.
func validInstruction(instruction string) bool {
	return strings.TrimSpace(instruction) != "" && len(instruction) <= MaxInstructionBytes
}

// pairKeys mints the two labels from the workflow id, as the contract fixes:
// wf-<first 12 hex of the id>-in and -out.
func pairKeys(workflowID string) (string, string) {
	hex := strings.ReplaceAll(workflowID, "-", "")
	if len(hex) > 12 {
		hex = hex[:12]
	}
	return "wf-" + hex + "-in", "wf-" + hex + "-out"
}

// normalizeLookup keeps a missing row a package error, so handlers translate one
// vocabulary.
func normalizeLookup(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// emptyRunCounts is the per-state map a detail response always carries: a state
// with no run is a zero, not an absent key.
func emptyRunCounts() map[string]int {
	return map[string]int{
		RunHeld:      0,
		RunArmed:     0,
		RunRunning:   0,
		RunCompleted: 0,
		RunFailed:    0,
	}
}

func runSummary(record RunRecord) RunSummary {
	return RunSummary{
		ID:          record.ID,
		State:       record.State,
		Fingerprint: record.Fingerprint,
		ItemCount:   len(record.Files),
		CreatedAt:   formatTime(record.CreatedAt),
	}
}

func workflowFromRecord(record WorkflowRecord) Workflow {
	return Workflow{
		ID:            record.ID,
		UserID:        record.UserID,
		BotID:         record.BotID,
		Name:          record.Name,
		Instruction:   record.Instruction,
		InputPairKey:  record.InputPairKey,
		OutputPairKey: record.OutputPairKey,
		State:         record.State,
		CreatedAt:     formatTime(record.CreatedAt),
		UpdatedAt:     formatTime(record.UpdatedAt),
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}
