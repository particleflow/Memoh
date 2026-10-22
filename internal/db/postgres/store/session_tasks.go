package postgresstore

import (
	"context"

	"github.com/google/uuid"
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
