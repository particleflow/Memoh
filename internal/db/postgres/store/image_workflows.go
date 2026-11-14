package postgresstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/imageworkflow"
)

// The image workflow stores: workflow rows, run rows, the workflow-role side of
// album_pairs, and the two lineage lookups product attribution needs. Every
// query is owner- or team-scoped inside SQL, so a store call cannot read across
// the boundary by forgetting a filter.

func imageWorkflowRecord(row dbsqlc.ImageWorkflow) imageworkflow.WorkflowRecord {
	return imageworkflow.WorkflowRecord{
		ID:            row.ID.String(),
		TeamID:        row.TeamID.String(),
		UserID:        row.UserID.String(),
		BotID:         row.BotID.String(),
		Name:          row.Name,
		Instruction:   row.Instruction,
		InputPairKey:  row.InputPairKey,
		OutputPairKey: row.OutputPairKey,
		State:         row.State,
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
}

func imageWorkflowRunRecord(row dbsqlc.ImageWorkflowRun) imageworkflow.RunRecord {
	record := imageworkflow.RunRecord{
		ID:                  row.ID.String(),
		TeamID:              row.TeamID.String(),
		WorkflowID:          row.WorkflowID.String(),
		ScheduleID:          uuidOrEmpty(row.ScheduleID),
		SessionID:           uuidOrEmpty(row.SessionID),
		InstructionSnapshot: row.InstructionSnapshot,
		Fingerprint:         row.Fingerprint,
		State:               row.State,
		CreatedAt:           row.CreatedAt.Time,
		UpdatedAt:           row.UpdatedAt.Time,
	}
	if row.Result.Valid {
		record.Result = row.Result.String
	}
	if row.Error.Valid {
		record.Error = row.Error.String
	}
	if len(row.Files) > 0 {
		_ = json.Unmarshal(row.Files, &record.Files)
	}
	return record
}

func uuidOrEmpty(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return value.String()
}

// isConstraintViolation reports the named CHECK or UNIQUE violation, which is
// how a name conflict and a structural refusal reach the service as one package
// error instead of a driver type.
func isConstraintViolation(err error, name string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return (pgErr.Code == "23505" || pgErr.Code == "23514") && pgErr.ConstraintName == name
}

func (s *Store) CreateWorkflow(ctx context.Context, record imageworkflow.WorkflowRecord) (imageworkflow.WorkflowRecord, error) {
	owner, err := db.ParseUUID(record.UserID)
	if err != nil {
		return imageworkflow.WorkflowRecord{}, imageworkflow.ErrInvalid
	}
	botID, err := db.ParseUUID(record.BotID)
	if err != nil {
		return imageworkflow.WorkflowRecord{}, imageworkflow.ErrInvalid
	}
	row, err := s.queries.CreateImageWorkflow(ctx, dbsqlc.CreateImageWorkflowParams{
		UserID:        owner,
		BotID:         botID,
		Name:          record.Name,
		Instruction:   record.Instruction,
		InputPairKey:  record.InputPairKey,
		OutputPairKey: record.OutputPairKey,
	})
	if err != nil {
		if isConstraintViolation(err, "image_workflows_name_key") {
			return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNameTaken
		}
		return imageworkflow.WorkflowRecord{}, mapQueryErr(err)
	}
	return imageWorkflowRecord(row), nil
}

func (s *Store) ListWorkflows(ctx context.Context, userID string) ([]imageworkflow.WorkflowRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return nil, imageworkflow.ErrInvalid
	}
	rows, err := s.queries.ListImageWorkflows(ctx, owner)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	out := make([]imageworkflow.WorkflowRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, imageWorkflowRecord(row))
	}
	return out, nil
}

func (s *Store) GetWorkflow(ctx context.Context, userID, workflowID string) (imageworkflow.WorkflowRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return imageworkflow.WorkflowRecord{}, imageworkflow.ErrInvalid
	}
	id, err := db.ParseUUID(workflowID)
	if err != nil {
		return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
	}
	row, err := s.queries.GetImageWorkflow(ctx, dbsqlc.GetImageWorkflowParams{UserID: owner, ID: id})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
		}
		return imageworkflow.WorkflowRecord{}, mapQueryErr(err)
	}
	return imageWorkflowRecord(row), nil
}

func (s *Store) GetWorkflowByName(ctx context.Context, userID, name string) (imageworkflow.WorkflowRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return imageworkflow.WorkflowRecord{}, imageworkflow.ErrInvalid
	}
	row, err := s.queries.GetImageWorkflowByName(ctx, dbsqlc.GetImageWorkflowByNameParams{UserID: owner, Name: name})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
		}
		return imageworkflow.WorkflowRecord{}, mapQueryErr(err)
	}
	return imageWorkflowRecord(row), nil
}

func (s *Store) UpdateWorkflow(ctx context.Context, record imageworkflow.WorkflowRecord) (imageworkflow.WorkflowRecord, error) {
	owner, err := db.ParseUUID(record.UserID)
	if err != nil {
		return imageworkflow.WorkflowRecord{}, imageworkflow.ErrInvalid
	}
	id, err := db.ParseUUID(record.ID)
	if err != nil {
		return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
	}
	row, err := s.queries.UpdateImageWorkflow(ctx, dbsqlc.UpdateImageWorkflowParams{
		Name:        record.Name,
		Instruction: record.Instruction,
		UserID:      owner,
		ID:          id,
	})
	if err != nil {
		if isConstraintViolation(err, "image_workflows_name_key") {
			return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNameTaken
		}
		if errors.Is(err, db.ErrNotFound) {
			return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
		}
		return imageworkflow.WorkflowRecord{}, mapQueryErr(err)
	}
	return imageWorkflowRecord(row), nil
}

func (s *Store) SetWorkflowState(ctx context.Context, userID, workflowID, state string) (imageworkflow.WorkflowRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return imageworkflow.WorkflowRecord{}, imageworkflow.ErrInvalid
	}
	id, err := db.ParseUUID(workflowID)
	if err != nil {
		return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
	}
	row, err := s.queries.SetImageWorkflowState(ctx, dbsqlc.SetImageWorkflowStateParams{State: state, UserID: owner, ID: id})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return imageworkflow.WorkflowRecord{}, imageworkflow.ErrNotFound
		}
		return imageworkflow.WorkflowRecord{}, mapQueryErr(err)
	}
	return imageWorkflowRecord(row), nil
}

func (s *Store) ListLiveWorkflowEdges(ctx context.Context, userID string) ([]imageworkflow.Edge, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return nil, imageworkflow.ErrInvalid
	}
	rows, err := s.queries.ListLiveImageWorkflowEdges(ctx, owner)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	out := make([]imageworkflow.Edge, 0, len(rows))
	for _, row := range rows {
		out = append(out, imageworkflow.Edge{
			WorkflowID:    row.ID.String(),
			InputPairKey:  row.InputPairKey,
			OutputPairKey: row.OutputPairKey,
		})
	}
	return out, nil
}

func (s *Store) CreateRun(ctx context.Context, record imageworkflow.RunRecord) (imageworkflow.RunRecord, error) {
	workflowID, err := db.ParseUUID(record.WorkflowID)
	if err != nil {
		return imageworkflow.RunRecord{}, imageworkflow.ErrNotFound
	}
	files, err := json.Marshal(record.Files)
	if err != nil {
		return imageworkflow.RunRecord{}, err
	}
	row, err := s.queries.CreateImageWorkflowRun(ctx, dbsqlc.CreateImageWorkflowRunParams{
		ScheduleID:          db.ParseUUIDOrEmpty(record.ScheduleID),
		SessionID:           db.ParseUUIDOrEmpty(record.SessionID),
		InstructionSnapshot: record.InstructionSnapshot,
		Files:               files,
		Fingerprint:         record.Fingerprint,
		State:               record.State,
		WorkflowID:          workflowID,
	})
	if err != nil {
		return imageworkflow.RunRecord{}, mapQueryErr(err)
	}
	return imageWorkflowRunRecord(row), nil
}

func (s *Store) RunByFingerprint(ctx context.Context, workflowID, fingerprint string) (imageworkflow.RunRecord, error) {
	id, err := db.ParseUUID(workflowID)
	if err != nil {
		return imageworkflow.RunRecord{}, imageworkflow.ErrNotFound
	}
	row, err := s.queries.GetImageWorkflowRunByFingerprint(ctx, dbsqlc.GetImageWorkflowRunByFingerprintParams{
		WorkflowID:  id,
		Fingerprint: fingerprint,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return imageworkflow.RunRecord{}, imageworkflow.ErrNotFound
		}
		return imageworkflow.RunRecord{}, mapQueryErr(err)
	}
	return imageWorkflowRunRecord(row), nil
}

func (s *Store) ListRuns(ctx context.Context, workflowID string, limit int) ([]imageworkflow.RunRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	id, err := db.ParseUUID(workflowID)
	if err != nil {
		return nil, imageworkflow.ErrNotFound
	}
	rows, err := s.queries.ListImageWorkflowRuns(ctx, dbsqlc.ListImageWorkflowRunsParams{
		WorkflowID: id,
		RowLimit:   int32(limit), //nolint:gosec // G115: bounded by the service's own window constants.
	})
	if err != nil {
		return nil, mapQueryErr(err)
	}
	out := make([]imageworkflow.RunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, imageWorkflowRunRecord(row))
	}
	return out, nil
}

func (s *Store) ListHeldRuns(ctx context.Context, workflowID string) ([]imageworkflow.RunRecord, error) {
	id, err := db.ParseUUID(workflowID)
	if err != nil {
		return nil, imageworkflow.ErrNotFound
	}
	rows, err := s.queries.ListHeldImageWorkflowRuns(ctx, id)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	out := make([]imageworkflow.RunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, imageWorkflowRunRecord(row))
	}
	return out, nil
}

func (s *Store) SetRunState(ctx context.Context, runID, state string) (imageworkflow.RunRecord, error) {
	id, err := db.ParseUUID(runID)
	if err != nil {
		return imageworkflow.RunRecord{}, imageworkflow.ErrNotFound
	}
	row, err := s.queries.SetImageWorkflowRunState(ctx, dbsqlc.SetImageWorkflowRunStateParams{State: state, ID: id})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return imageworkflow.RunRecord{}, imageworkflow.ErrNotFound
		}
		return imageworkflow.RunRecord{}, mapQueryErr(err)
	}
	return imageWorkflowRunRecord(row), nil
}

// PairForWorkflow answers the label one workflow designated for one role.
func (s *Store) PairForWorkflow(ctx context.Context, userID, workflowID, role string) (imageworkflow.PairRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return imageworkflow.PairRecord{}, imageworkflow.ErrPairUnavailable
	}
	id, err := db.ParseUUID(workflowID)
	if err != nil {
		return imageworkflow.PairRecord{}, imageworkflow.ErrPairUnavailable
	}
	row, err := s.queries.ResolveActiveWorkflowAlbumPair(ctx, dbsqlc.ResolveActiveWorkflowAlbumPairParams{
		UserID:     owner,
		WorkflowID: id,
		Role:       pgtype.Text{String: role, Valid: true},
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return imageworkflow.PairRecord{}, imageworkflow.ErrPairUnavailable
		}
		return imageworkflow.PairRecord{}, mapQueryErr(err)
	}
	return imageworkflow.PairRecord{
		PairKey:    row.PairKey,
		BotID:      row.BotID.String(),
		State:      row.State,
		WorkflowID: uuidOrEmpty(row.WorkflowID),
		Role:       db.TextToString(row.Role),
	}, nil
}

// RevokeWorkflowPairs revokes both role rows of one workflow. The rows stay, so
// the labels stay occupied, and queued write-backs are untouched.
func (s *Store) RevokeWorkflowPairs(ctx context.Context, userID, workflowID string) error {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return imageworkflow.ErrInvalid
	}
	id, err := db.ParseUUID(workflowID)
	if err != nil {
		return imageworkflow.ErrNotFound
	}
	if err := s.queries.RevokeWorkflowAlbumPairs(ctx, dbsqlc.RevokeWorkflowAlbumPairsParams{UserID: owner, WorkflowID: id}); err != nil {
		return mapQueryErr(err)
	}
	return nil
}

// RunBySession answers the run whose execution session this is, which is the
// first step of product attribution.
func (s *Store) RunBySession(ctx context.Context, sessionID string) (imageworkflow.RunRecord, bool, error) {
	id, err := db.ParseUUID(sessionID)
	if err != nil {
		return imageworkflow.RunRecord{}, false, nil
	}
	row, err := s.queries.GetImageWorkflowRunBySession(ctx, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return imageworkflow.RunRecord{}, false, nil
		}
		return imageworkflow.RunRecord{}, false, mapQueryErr(err)
	}
	return imageWorkflowRunRecord(row), true, nil
}

// ParentSession answers one step of the lineage walk.
func (s *Store) ParentSession(ctx context.Context, sessionID string) (string, bool, error) {
	id, err := db.ParseUUID(sessionID)
	if err != nil {
		return "", false, nil
	}
	parent, err := s.queries.GetSessionParentSessionID(ctx, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return "", false, nil
		}
		return "", false, mapQueryErr(err)
	}
	if !parent.Valid {
		return "", false, nil
	}
	return parent.String(), true, nil
}

// BarePair answers the owner's single automatic write-back target, which is the
// existing 1.0.60 semantic: workflow-role rows are never it.
func (s *Store) BarePair(ctx context.Context, userID, botID string) (string, bool, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return "", false, nil
	}
	bot, err := db.ParseUUID(botID)
	if err != nil {
		return "", false, nil
	}
	pairKey, err := s.queries.GetBareAlbumPairKey(ctx, dbsqlc.GetBareAlbumPairKeyParams{UserID: owner, BotID: bot})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return "", false, nil
		}
		return "", false, mapQueryErr(err)
	}
	return pairKey, true, nil
}
