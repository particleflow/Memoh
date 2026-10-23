package postgresstore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func (s *Store) ListSessionTaskCandidates(ctx context.Context, f dbstore.SessionTaskFilter) ([]dbstore.SessionTaskCandidate, error) {
	values := []string{f.BotID, f.UserID, f.ParentID, f.Before.ID, f.ID}
	ids := make([]pgtype.UUID, len(values))
	for i, value := range values {
		if value == "" {
			value = uuid.Nil.String()
		}
		id, err := db.ParseUUID(value)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	rows, err := s.queries.ListSessionTaskCandidates(ctx, dbsqlc.ListSessionTaskCandidatesParams{
		BotID: ids[0], UserID: ids[1], ParentID: ids[2],
		SnapshotAt: pgtype.Timestamptz{Time: f.SnapshotAt, Valid: true},
		UseCursor:  f.Before.ID != "", BeforeTime: pgtype.Timestamptz{Time: f.Before.CreatedAt, Valid: true}, BeforeType: f.Before.Type, BeforeID: ids[3],
		UseTask: f.ID != "", TaskType: f.Type, TaskID: ids[4], PageSize: f.Limit,
	})
	if err != nil {
		return nil, mapQueryErr(err)
	}
	items := make([]dbstore.SessionTaskCandidate, 0, len(rows))
	for _, row := range rows {
		items = append(items, dbstore.SessionTaskCandidate{Type: row.TaskType, ID: row.TaskID.String(), SessionID: row.SessionID.String(), CreatedAt: row.CreatedAt.Time})
	}
	return items, nil
}

func (s *Store) ReadSubagentRun(ctx context.Context, bot, session string) (dbstore.SubagentRun, error) {
	bid, err := db.ParseUUID(bot)
	if err != nil {
		return dbstore.SubagentRun{}, err
	}
	sid, err := db.ParseUUID(session)
	if err != nil {
		return dbstore.SubagentRun{}, err
	}
	// Reuse the official latest-run query, including its reset/deletion fence.
	run, err := s.queries.GetLatestSessionRun(ctx, sid)
	if err != nil {
		return dbstore.SubagentRun{}, mapQueryErr(err)
	}
	if run.BotID != bid || run.SessionID != sid {
		return dbstore.SubagentRun{}, db.ErrNotFound
	}
	out := dbstore.SubagentRun{BotID: bot, SessionID: session, RunID: run.RunID.String(), State: run.State, UpdatedAt: run.UpdatedAt.Time}
	out.Content, err = s.queries.GetSubagentRunVisibleMessage(ctx, dbsqlc.GetSubagentRunVisibleMessageParams{BotID: bid, SessionID: sid, RunID: run.RunID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return dbstore.SubagentRun{}, mapQueryErr(err)
	}
	return out, nil
}
