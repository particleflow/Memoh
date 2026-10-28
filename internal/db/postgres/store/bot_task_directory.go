package postgresstore

import (
	"context"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) ListBotTaskDirectoryCandidates(ctx context.Context, f dbstore.SessionTaskFilter) ([]dbstore.BotTaskDirectoryCandidate, error) {
	values := []string{f.BotID, f.UserID, f.Before.ID, f.ID}
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
	rows, err := s.queries.ListBotTaskDirectoryCandidates(ctx, dbsqlc.ListBotTaskDirectoryCandidatesParams{
		BotID: ids[0], UserID: ids[1], SnapshotAt: pgtype.Timestamptz{Time: f.SnapshotAt, Valid: true},
		UseCursor: f.Before.ID != "", BeforeTime: pgtype.Timestamptz{Time: f.Before.CreatedAt, Valid: true}, BeforeType: f.Before.Type, BeforeID: ids[2],
		UseTask: f.ID != "", TaskType: f.Type, RecordID: ids[3], PageSize: f.Limit,
	})
	if err != nil {
		return nil, mapQueryErr(err)
	}
	out := make([]dbstore.BotTaskDirectoryCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbstore.BotTaskDirectoryCandidate{Type: r.TaskType, ID: r.TaskID, RecordID: r.RecordID.String(), SessionID: r.SessionID.String(), RunID: r.RunID,
			SourceBotID: r.SourceBotID.String(), SourceSessionID: r.SourceSessionID.String(), ScheduleID: r.ScheduleID.String(), OwnerUserID: r.OwnerUserID.String(),
			Name: r.Name, State: r.State, QueueAdmission: r.QueueAdmission, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time})
	}
	return out, nil
}

func (s *Store) ReadBotTaskRun(ctx context.Context, bot, session, run string) (dbstore.BotTaskRunReference, error) {
	bid, err := db.ParseUUID(bot)
	if err != nil {
		return dbstore.BotTaskRunReference{}, err
	}
	sid, err := db.ParseUUID(session)
	if err != nil {
		return dbstore.BotTaskRunReference{}, err
	}
	r, err := s.queries.ReadBotTaskRun(ctx, dbsqlc.ReadBotTaskRunParams{BotID: bid, SessionID: sid, RunID: run})
	if err != nil {
		return dbstore.BotTaskRunReference{}, mapQueryErr(err)
	}
	return dbstore.BotTaskRunReference{BotID: r.BotID.String(), SessionID: r.SessionID.String(), RunID: r.RunID.String(), TurnID: r.TurnID.String(), InvocationID: r.InvocationID}, nil
}
