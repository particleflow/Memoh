package postgresstore

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func albumPairRecord(row dbsqlc.AlbumPair) dbstore.AlbumPairRecord {
	return dbstore.AlbumPairRecord{
		ID:        row.ID.String(),
		UserID:    row.UserID.String(),
		BotID:     row.BotID.String(),
		PairKey:   row.PairKey,
		State:     row.State,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func (s *Store) ListAlbumPairs(ctx context.Context, userID string) ([]dbstore.AlbumPairRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListAlbumPairs(ctx, owner)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	out := make([]dbstore.AlbumPairRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, albumPairRecord(row))
	}
	return out, nil
}

// UpsertAlbumPair retires the owner's previous active target for this bot and
// activates this one in the same transaction. Retiring first keeps the partial
// unique index satisfied without a retry loop.
func (s *Store) UpsertAlbumPair(ctx context.Context, record dbstore.AlbumPairRecord) (dbstore.AlbumPairRecord, error) {
	owner, err := db.ParseUUID(record.UserID)
	if err != nil {
		return dbstore.AlbumPairRecord{}, err
	}
	botID, err := db.ParseUUID(record.BotID)
	if err != nil {
		return dbstore.AlbumPairRecord{}, err
	}
	if s.pool == nil {
		// A pool-less store is already a single transactional connection (test
		// fixtures hand one in); run both statements inline.
		return upsertAlbumPair(ctx, s.queries, owner, botID, record.PairKey)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return dbstore.AlbumPairRecord{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	out, err := upsertAlbumPair(ctx, s.queries.WithTx(tx), owner, botID, record.PairKey)
	if err != nil {
		return dbstore.AlbumPairRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return dbstore.AlbumPairRecord{}, err
	}
	return out, nil
}

func upsertAlbumPair(ctx context.Context, queries *dbsqlc.Queries, owner, botID pgtype.UUID, pairKey string) (dbstore.AlbumPairRecord, error) {
	if err := queries.RetireActiveAlbumPair(ctx, dbsqlc.RetireActiveAlbumPairParams{
		UserID:  owner,
		BotID:   botID,
		PairKey: pairKey,
	}); err != nil {
		return dbstore.AlbumPairRecord{}, mapQueryErr(err)
	}
	row, err := queries.UpsertAlbumPair(ctx, dbsqlc.UpsertAlbumPairParams{
		UserID:  owner,
		BotID:   botID,
		PairKey: pairKey,
	})
	if err != nil {
		return dbstore.AlbumPairRecord{}, mapQueryErr(err)
	}
	return albumPairRecord(row), nil
}

func (s *Store) RevokeAlbumPair(ctx context.Context, userID, pairKey string) (dbstore.AlbumPairRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return dbstore.AlbumPairRecord{}, err
	}
	row, err := s.queries.RevokeAlbumPair(ctx, dbsqlc.RevokeAlbumPairParams{UserID: owner, PairKey: pairKey})
	if err != nil {
		return dbstore.AlbumPairRecord{}, mapQueryErr(err)
	}
	return albumPairRecord(row), nil
}

func (s *Store) ResolveActiveAlbumPair(ctx context.Context, userID, botID string) (dbstore.AlbumPairRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return dbstore.AlbumPairRecord{}, err
	}
	bot, err := db.ParseUUID(botID)
	if err != nil {
		return dbstore.AlbumPairRecord{}, err
	}
	row, err := s.queries.ResolveActiveAlbumPair(ctx, dbsqlc.ResolveActiveAlbumPairParams{UserID: owner, BotID: bot})
	if err != nil {
		return dbstore.AlbumPairRecord{}, mapQueryErr(err)
	}
	return albumPairRecord(row), nil
}
