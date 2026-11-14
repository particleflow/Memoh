// Package imageworkflow defines the server-side contract for image workflows:
// a named, user-owned pipeline of one input album pairing, one natural-language
// instruction and one output album pairing.
//
// This file and the stubs beside it are the frozen contract for the test-first
// round. Real behavior is deliberately absent: every exported operation returns
// ErrNotImplemented, so the contract tests in this branch fail for exactly one
// reason (the feature is missing) and turn green only when the feature lands.
//
// Frozen contract: docs/contracts/image-workflows.json.
// Design inputs: docs/field-issues/research/2026-11-14-image-workflow-design-*.md.
package imageworkflow

import (
	"context"
	"errors"
	"time"

	"github.com/felinics/memoh/internal/media"
)

// ErrNotImplemented is the sentinel this test-first round returns from every
// stub. An implementation replaces each stub body; it does not keep this error
// on any success or contract path.
var ErrNotImplemented = errors.New("image workflow: not implemented")

// Package errors. Callers translate them into public codes (see the contract's
// error_codes block); the service never constructs apperror values itself.
var (
	// ErrInvalid reports a malformed name, instruction, bot id, file list or path.
	ErrInvalid = errors.New("invalid image workflow request")
	// ErrNotFound reports a workflow the caller does not own.
	ErrNotFound = errors.New("image workflow not found")
	// ErrNameTaken reports a name the caller already holds, revoked rows included.
	ErrNameTaken = errors.New("image workflow name already taken")
	// ErrNotActive reports a revoked workflow asked to trigger, pause or resume.
	ErrNotActive = errors.New("image workflow is not active")
	// ErrPairUnavailable reports an input or output pairing the phone has not designated.
	ErrPairUnavailable = errors.New("image workflow pairing unavailable")
	// ErrCycle reports labels that would close a cycle in the pairing graph.
	ErrCycle = errors.New("image workflow pairing cycle")
)

// Workflow states.
const (
	StateActive  = "active"
	StatePaused  = "paused"
	StateRevoked = "revoked"
)

// Run states. held is the state a paused workflow gives an accepted batch:
// the directory watcher has already consumed the files, so refusing would lose
// them; arming later uses the run's own hash list.
const (
	RunHeld      = "held"
	RunArmed     = "armed"
	RunRunning   = "running"
	RunCompleted = "completed"
	RunFailed    = "failed"
)

// Trigger sources. An upload trigger names content hashes the media store
// already holds; a directory trigger names files the server reads and ingests.
const (
	SourceUpload    = "upload"
	SourceDirectory = "directory"
)

// Pairing roles inside one workflow.
const (
	RoleInput  = "input"
	RoleOutput = "output"
)

// Limits shared by validation and the contract tests.
const (
	// MaxNameBytes bounds a workflow name; the label is a conversation anchor.
	MaxNameBytes = 128
	// MaxInstructionBytes bounds the natural-language instruction.
	MaxInstructionBytes = 16 * 1024
	// MaxBatchFiles bounds one trigger batch, matching the directory watcher's
	// existing batch ceiling. It is a chunk size, not a total quota.
	MaxBatchFiles = 500
	// MaxLineageDepth bounds the parent-session walk in product attribution.
	MaxLineageDepth = 8
)

// Workflow is one workflow as a user endpoint returns it.
type Workflow struct {
	ID            string `json:"id"`
	UserID        string `json:"user_id"`
	BotID         string `json:"bot_id"`
	Name          string `json:"name"`
	Instruction   string `json:"instruction"`
	InputPairKey  string `json:"input_pair_key"`
	OutputPairKey string `json:"output_pair_key"`
	State         string `json:"state"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// WorkflowRecord is the store-level shape of one workflow row.
type WorkflowRecord struct {
	ID            string
	TeamID        string
	UserID        string
	BotID         string
	Name          string
	Instruction   string
	InputPairKey  string
	OutputPairKey string
	State         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// InputFile is one file of a trigger batch.
type InputFile struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	Mime        string `json:"mime"`
}

// RunRecord is the store-level shape of one run row.
type RunRecord struct {
	ID                  string
	TeamID              string
	WorkflowID          string
	ScheduleID          string
	SessionID           string
	InstructionSnapshot string
	Files               []InputFile
	Fingerprint         string
	State               string
	Result              string
	Error               string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// RunSummary is the narrow run shape a detail response carries.
type RunSummary struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	Fingerprint string `json:"fingerprint"`
	ItemCount   int    `json:"item_count"`
	CreatedAt   string `json:"created_at"`
}

// ListResponse is GET /users/me/image-workflows.
type ListResponse struct {
	Workflows []Workflow `json:"workflows"`
}

// RoutesResponse is GET /internal/image-workflow-routes.
type RoutesResponse struct {
	Routes []Route `json:"routes"`
}

// WorkflowDetail is GET /users/me/image-workflows/{id}.
type WorkflowDetail struct {
	Workflow  Workflow       `json:"workflow"`
	Runs      []RunSummary   `json:"runs"`
	RunCounts map[string]int `json:"run_counts"`
}

// CreateInput is the create request body.
type CreateInput struct {
	Name        string `json:"name"`
	Instruction string `json:"instruction"`
	BotID       string `json:"bot_id"`
}

// UpdateInput is the update request body. The pairing labels are not editable:
// an album change is a phone picker action on the album-pairs surface.
type UpdateInput struct {
	Name        *string `json:"name,omitempty"`
	Instruction *string `json:"instruction,omitempty"`
}

// TriggerRequest is the trigger body shared by both sources.
type TriggerRequest struct {
	Source   string      `json:"source"`
	Files    []InputFile `json:"files"`
	BatchKey string      `json:"batch_key,omitempty"`
}

// TriggerResult is the trigger response.
type TriggerResult struct {
	RunID      string `json:"run_id"`
	State      string `json:"state"`
	Duplicate  bool   `json:"duplicate"`
	ScheduleID string `json:"schedule_id,omitempty"`
}

// ResumeResult reports how many held runs the resume armed.
type ResumeResult struct {
	Workflow  Workflow `json:"workflow"`
	ArmedRuns int      `json:"armed_runs"`
}

// Route is one entry of the directory watcher's route table.
type Route struct {
	InputDir     string `json:"input_dir"`
	InputPairKey string `json:"input_pair_key"`
	WorkflowID   string `json:"workflow_id"`
	BotID        string `json:"bot_id"`
	State        string `json:"state"`
}

// PairRecord is one pairing row the service reads.
type PairRecord struct {
	PairKey    string
	BotID      string
	State      string
	WorkflowID string
	Role       string
}

// Edge is one live workflow as a directed pairing-graph edge.
type Edge struct {
	WorkflowID    string
	InputPairKey  string
	OutputPairKey string
}

// ScheduleRequest is the one-shot schedule a trigger creates. It mirrors the
// existing schedule create payload field for field, so the adapter is a
// translation and not a second scheduler.
type ScheduleRequest struct {
	Name        string
	Description string
	Pattern     string
	MaxCalls    int
	Command     string
	RunTarget   string
}

// ScheduleCreator creates the one-shot schedule and returns its id.
type ScheduleCreator interface {
	CreateOneShot(ctx context.Context, botID string, req ScheduleRequest) (string, error)
}

// AssetResolver resolves a content hash against the existing content store.
type AssetResolver interface {
	Resolve(ctx context.Context, botID, contentHash string) (media.Asset, error)
}

// WorkflowStore owns workflow rows.
type WorkflowStore interface {
	CreateWorkflow(ctx context.Context, record WorkflowRecord) (WorkflowRecord, error)
	ListWorkflows(ctx context.Context, userID string) ([]WorkflowRecord, error)
	GetWorkflow(ctx context.Context, userID, workflowID string) (WorkflowRecord, error)
	GetWorkflowByName(ctx context.Context, userID, name string) (WorkflowRecord, error)
	UpdateWorkflow(ctx context.Context, record WorkflowRecord) (WorkflowRecord, error)
	SetWorkflowState(ctx context.Context, userID, workflowID, state string) (WorkflowRecord, error)
}

// RunStore owns run rows.
type RunStore interface {
	CreateRun(ctx context.Context, record RunRecord) (RunRecord, error)
	RunByFingerprint(ctx context.Context, workflowID, fingerprint string) (RunRecord, error)
	ListRuns(ctx context.Context, workflowID string, limit int) ([]RunRecord, error)
	ListHeldRuns(ctx context.Context, workflowID string) ([]RunRecord, error)
	SetRunState(ctx context.Context, runID, state string) (RunRecord, error)
}

// PairStore owns the workflow-role side of album_pairs.
type PairStore interface {
	PairForWorkflow(ctx context.Context, userID, workflowID, role string) (PairRecord, error)
	RevokeWorkflowPairs(ctx context.Context, userID, workflowID string) error
}

// LineageStore answers product attribution: which run, and which bare target
// when no run does.
type LineageStore interface {
	// RunBySession returns the run whose workflow session is this session.
	RunBySession(ctx context.Context, sessionID string) (RunRecord, bool, error)
	// ParentSession returns the parent of a session, false at a root.
	ParentSession(ctx context.Context, sessionID string) (string, bool, error)
	// BarePair returns the active pairing with workflow_id IS NULL for the bot.
	BarePair(ctx context.Context, userID, botID string) (string, bool, error)
}

// GraphStore answers the live pairing-graph edges used by cycle detection.
type GraphStore interface {
	ListLiveWorkflowEdges(ctx context.Context, userID string) ([]Edge, error)
}
