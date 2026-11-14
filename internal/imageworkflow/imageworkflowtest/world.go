// Package imageworkflowtest provides an in-memory world for the image workflow
// contract tests. It is test support only: production wires the postgres store,
// the schedule service and the media service instead.
//
// It deliberately implements the frozen interfaces with the least behavior that
// makes an implementation possible — no assertions live here, and a test that
// wants to check something reads the world's exported state after the call.
package imageworkflowtest

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/felinics/memoh/internal/imageworkflow"
	"github.com/felinics/memoh/internal/media"
)

// Schedule is one one-shot schedule the trigger created.
type Schedule struct {
	BotID string
	Req   imageworkflow.ScheduleRequest
	ID    string
}

// World is the in-memory backing of every store interface.
type World struct {
	mu        sync.Mutex
	workflows []imageworkflow.WorkflowRecord
	runs      []imageworkflow.RunRecord
	pairs     []imageworkflow.PairRecord
	sessions  map[string]string // session id -> parent session id
	runBySess map[string]string // session id -> run id
	schedules []Schedule
	revoked   []string          // workflow ids whose pairing rows were revoked
	bare      map[string]string // user|bot -> bare pair key
	assets    map[string]media.Asset
}

// New returns an empty world.
func New() *World {
	return &World{
		sessions:  map[string]string{},
		runBySess: map[string]string{},
		bare:      map[string]string{},
		assets:    map[string]media.Asset{},
	}
}

// Deps wires the world into the service contract surface.
func (w *World) Deps() imageworkflow.Deps {
	return imageworkflow.Deps{
		Workflows:     w,
		Runs:          w,
		Pairs:         w,
		Lineage:       w,
		Graph:         w,
		Schedules:     w,
		Assets:        w,
		InputRoot:     "/srv/workflows",
		OriginalsRoot: "/srv/originals",
	}
}

// AddAsset lets a test state that a content hash resolves for a bot.
func (w *World) AddAsset(botID, contentHash string, size int64, mime string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.assets[contentHash] = media.Asset{ContentHash: contentHash, BotID: botID, SizeBytes: size, Mime: mime, StorageKey: contentHash[:2] + "/" + contentHash + extForMime(mime)}
}

// SeedWorkflow inserts a workflow row directly, for tests that only need a
// target to act on.
func (w *World) SeedWorkflow(id, userID, botID, name, state, instruction string) imageworkflow.WorkflowRecord {
	record := imageworkflow.WorkflowRecord{
		ID:            id,
		UserID:        userID,
		BotID:         botID,
		Name:          name,
		Instruction:   instruction,
		InputPairKey:  "wf-" + id + "-in",
		OutputPairKey: "wf-" + id + "-out",
		State:         state,
		CreatedAt:     time.Unix(0, 0).UTC(),
		UpdatedAt:     time.Unix(0, 0).UTC(),
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.workflows = append(w.workflows, record)
	return record
}

// SeedPair registers one pairing row of one workflow, as the phone would after
// designating an album.
func (w *World) SeedPair(workflowID, botID, role, state string) imageworkflow.PairRecord {
	record := imageworkflow.PairRecord{
		PairKey:    "wf-" + workflowID + "-" + roleSuffix(role),
		BotID:      botID,
		State:      state,
		WorkflowID: workflowID,
		Role:       role,
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pairs = append(w.pairs, record)
	return record
}

func roleSuffix(role string) string {
	if role == imageworkflow.RoleInput {
		return "in"
	}
	return "out"
}

// SeedActivePairs registers both pairing rows as active.
func (w *World) SeedActivePairs(workflowID, botID string) {
	w.SeedPair(workflowID, botID, imageworkflow.RoleInput, "active")
	w.SeedPair(workflowID, botID, imageworkflow.RoleOutput, "active")
}

// SeedBarePair registers the owner's bare automatic write-back target.
func (w *World) SeedBarePair(userID, botID, pairKey string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bare[userID+"|"+botID] = pairKey
}

// SeedParentSession links a session to its parent.
func (w *World) SeedParentSession(sessionID, parentSessionID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sessions[sessionID] = parentSessionID
}

// SeedRunSession points a run at the session a schedule fire created.
func (w *World) SeedRunSession(runID, sessionID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.runBySess[sessionID] = runID
	for i := range w.runs {
		if w.runs[i].ID == runID {
			w.runs[i].SessionID = sessionID
		}
	}
}

// Workflows returns a copy of the workflow rows.
func (w *World) Workflows() []imageworkflow.WorkflowRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]imageworkflow.WorkflowRecord(nil), w.workflows...)
}

// Runs returns a copy of the run rows.
func (w *World) Runs() []imageworkflow.RunRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]imageworkflow.RunRecord(nil), w.runs...)
}

// Pairs returns a copy of the pairing rows.
func (w *World) Pairs() []imageworkflow.PairRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]imageworkflow.PairRecord(nil), w.pairs...)
}

// Schedules returns a copy of the created one-shot schedules.
func (w *World) Schedules() []Schedule {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Schedule(nil), w.schedules...)
}

// RevokedWorkflows returns the workflow ids whose pairing rows were revoked.
func (w *World) RevokedWorkflows() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.revoked...)
}

// CreateWorkflow implements imageworkflow.WorkflowStore.
func (w *World) CreateWorkflow(_ context.Context, record imageworkflow.WorkflowRecord) (imageworkflow.WorkflowRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, row := range w.workflows {
		if row.UserID == record.UserID && row.Name == record.Name {
			return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNameTaken
		}
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Unix(0, 0).UTC()
	}
	record.UpdatedAt = record.CreatedAt
	w.workflows = append(w.workflows, record)
	return record, nil
}

// ListWorkflows implements imageworkflow.WorkflowStore.
func (w *World) ListWorkflows(_ context.Context, userID string) ([]imageworkflow.WorkflowRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []imageworkflow.WorkflowRecord
	for _, row := range w.workflows {
		if row.UserID == userID {
			out = append(out, row)
		}
	}
	return out, nil
}

// GetWorkflow implements imageworkflow.WorkflowStore.
func (w *World) GetWorkflow(_ context.Context, userID, workflowID string) (imageworkflow.WorkflowRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, row := range w.workflows {
		if row.UserID == userID && row.ID == workflowID {
			return row, nil
		}
	}
	return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
}

// GetWorkflowByName implements imageworkflow.WorkflowStore.
func (w *World) GetWorkflowByName(_ context.Context, userID, name string) (imageworkflow.WorkflowRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, row := range w.workflows {
		if row.UserID == userID && row.Name == name {
			return row, nil
		}
	}
	return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
}

// UpdateWorkflow implements imageworkflow.WorkflowStore.
func (w *World) UpdateWorkflow(_ context.Context, record imageworkflow.WorkflowRecord) (imageworkflow.WorkflowRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.workflows {
		if w.workflows[i].ID == record.ID {
			w.workflows[i] = record
			return record, nil
		}
	}
	return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
}

// SetWorkflowState implements imageworkflow.WorkflowStore.
func (w *World) SetWorkflowState(_ context.Context, userID, workflowID, state string) (imageworkflow.WorkflowRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.workflows {
		if w.workflows[i].UserID == userID && w.workflows[i].ID == workflowID {
			w.workflows[i].State = state
			return w.workflows[i], nil
		}
	}
	return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
}

// CreateRun implements imageworkflow.RunStore.
func (w *World) CreateRun(_ context.Context, record imageworkflow.RunRecord) (imageworkflow.RunRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, row := range w.runs {
		if row.WorkflowID == record.WorkflowID && row.Fingerprint == record.Fingerprint {
			return imageworkflow.RunRecord{}, errors.New("duplicate run fingerprint")
		}
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Unix(0, 0).UTC()
	}
	w.runs = append(w.runs, record)
	return record, nil
}

// RunByFingerprint implements imageworkflow.RunStore.
func (w *World) RunByFingerprint(_ context.Context, workflowID, fingerprint string) (imageworkflow.RunRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, row := range w.runs {
		if row.WorkflowID == workflowID && row.Fingerprint == fingerprint {
			return row, nil
		}
	}
	return imageworkflow.RunRecord{}, imageworkflow.ErrNotFound
}

// ListRuns implements imageworkflow.RunStore, newest first.
func (w *World) ListRuns(_ context.Context, workflowID string, limit int) ([]imageworkflow.RunRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []imageworkflow.RunRecord
	for i := len(w.runs) - 1; i >= 0 && len(out) < limit; i-- {
		if w.runs[i].WorkflowID == workflowID {
			out = append(out, w.runs[i])
		}
	}
	return out, nil
}

// ListHeldRuns implements imageworkflow.RunStore.
func (w *World) ListHeldRuns(_ context.Context, workflowID string) ([]imageworkflow.RunRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []imageworkflow.RunRecord
	for _, row := range w.runs {
		if row.WorkflowID == workflowID && row.State == imageworkflow.RunHeld {
			out = append(out, row)
		}
	}
	return out, nil
}

// SetRunState implements imageworkflow.RunStore.
func (w *World) SetRunState(_ context.Context, runID, state string) (imageworkflow.RunRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.runs {
		if w.runs[i].ID == runID {
			w.runs[i].State = state
			return w.runs[i], nil
		}
	}
	return imageworkflow.RunRecord{}, imageworkflow.ErrNotFound
}

// PairForWorkflow implements imageworkflow.PairStore.
func (w *World) PairForWorkflow(_ context.Context, _, workflowID, role string) (imageworkflow.PairRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, row := range w.pairs {
		if row.WorkflowID == workflowID && row.Role == role && row.State == "active" {
			return row, nil
		}
	}
	return imageworkflow.PairRecord{}, imageworkflow.ErrPairUnavailable
}

// RevokeWorkflowPairs implements imageworkflow.PairStore.
func (w *World) RevokeWorkflowPairs(_ context.Context, _, workflowID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.revoked = append(w.revoked, workflowID)
	for i := range w.pairs {
		if w.pairs[i].WorkflowID == workflowID {
			w.pairs[i].State = "revoked"
		}
	}
	return nil
}

// RunBySession implements imageworkflow.LineageStore.
func (w *World) RunBySession(_ context.Context, sessionID string) (imageworkflow.RunRecord, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	runID, ok := w.runBySess[sessionID]
	if !ok {
		return imageworkflow.RunRecord{}, false, nil
	}
	for _, row := range w.runs {
		if row.ID == runID {
			return row, true, nil
		}
	}
	return imageworkflow.RunRecord{}, false, nil
}

// ParentSession implements imageworkflow.LineageStore.
func (w *World) ParentSession(_ context.Context, sessionID string) (string, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	parent, ok := w.sessions[sessionID]
	return parent, ok && parent != "", nil
}

// BarePair implements imageworkflow.LineageStore.
func (w *World) BarePair(_ context.Context, userID, botID string) (string, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	pair, ok := w.bare[userID+"|"+botID]
	return pair, ok, nil
}

// ListLiveWorkflowEdges implements imageworkflow.GraphStore.
func (w *World) ListLiveWorkflowEdges(_ context.Context, userID string) ([]imageworkflow.Edge, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []imageworkflow.Edge
	for _, row := range w.workflows {
		if row.UserID == userID && row.State != imageworkflow.StateRevoked {
			out = append(out, imageworkflow.Edge{
				WorkflowID:    row.ID,
				InputPairKey:  row.InputPairKey,
				OutputPairKey: row.OutputPairKey,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkflowID < out[j].WorkflowID })
	return out, nil
}

// CreateOneShot implements imageworkflow.ScheduleCreator.
func (w *World) CreateOneShot(_ context.Context, botID string, req imageworkflow.ScheduleRequest) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := "schedule-" + itoa(len(w.schedules)+1)
	w.schedules = append(w.schedules, Schedule{BotID: botID, Req: req, ID: id})
	return id, nil
}

// Resolve implements imageworkflow.AssetResolver.
func (w *World) Resolve(_ context.Context, _ string, contentHash string) (media.Asset, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	asset, ok := w.assets[contentHash]
	if !ok {
		return media.Asset{}, errors.New("asset not found")
	}
	return asset, nil
}

func extForMime(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/heic":
		return ".heic"
	case "video/mp4":
		return ".mp4"
	default:
		return ".bin"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
