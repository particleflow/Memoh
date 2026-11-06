package postgresstore

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func (s *Store) ListSessionActivityCandidates(ctx context.Context, f dbstore.SessionTaskFilter) ([]dbstore.SessionTaskCandidate, error) {
	bot, err := db.ParseUUID(f.BotID)
	if err != nil {
		return nil, err
	}
	user, err := db.ParseUUID(f.UserID)
	if err != nil {
		return nil, err
	}
	before := f.Before.ID
	if before == "" {
		before = uuid.Nil.String()
	}
	id, err := db.ParseUUID(before)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListSessionActivityCandidates(ctx, dbsqlc.ListSessionActivityCandidatesParams{
		BotID: bot, UserID: user, SnapshotAt: pgtype.Timestamptz{Time: f.SnapshotAt, Valid: true},
		UseCursor: f.Before.ID != "", BeforeTime: pgtype.Timestamptz{Time: f.Before.CreatedAt, Valid: true}, BeforeType: f.Before.Type, BeforeID: id, PageSize: f.Limit,
	})
	if err != nil {
		return nil, mapQueryErr(err)
	}
	out := make([]dbstore.SessionTaskCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbstore.SessionTaskCandidate{Type: r.TaskType, ID: r.TaskID.String(), SessionID: r.SessionID.String(), CreatedAt: r.CreatedAt.Time, Name: r.Name})
	}
	return out, nil
}
