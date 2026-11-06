package postgresstore

import (
	"context"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func phoneAlbumWritebackRecord(row dbsqlc.PhoneAlbumWriteback) dbstore.PhoneAlbumWritebackRecord {
	record := dbstore.PhoneAlbumWritebackRecord{
		ID:          row.ID.String(),
		UserID:      row.UserID.String(),
		PairKey:     row.PairKey,
		BotID:       row.BotID.String(),
		ContentHash: row.ContentHash,
		Mime:        row.Mime,
		Name:        row.Name,
		State:       row.State,
		CreatedAt:   row.CreatedAt.Time,
	}
	if row.SizeBytes.Valid {
		size := row.SizeBytes.Int64
		record.SizeBytes = &size
	}
	if row.WrittenAt.Valid {
		record.WrittenAt = row.WrittenAt.Time
	}
	return record
}

func (s *Store) EnqueuePhoneAlbumWriteback(ctx context.Context, record dbstore.PhoneAlbumWritebackRecord) (dbstore.PhoneAlbumWritebackRecord, error) {
	userID, err := db.ParseUUID(record.UserID)
	if err != nil {
		return record, err
	}
	botID, err := db.ParseUUID(record.BotID)
	if err != nil {
		return record, err
	}
	params := dbsqlc.EnqueuePhoneAlbumWritebackParams{
		UserID:      userID,
		PairKey:     record.PairKey,
		BotID:       botID,
		ContentHash: record.ContentHash,
		Mime:        record.Mime,
		Name:        record.Name,
	}
	if record.SizeBytes != nil {
		params.SizeBytes.Int64 = *record.SizeBytes
		params.SizeBytes.Valid = true
	}
	row, err := s.queries.EnqueuePhoneAlbumWriteback(ctx, params)
	if err != nil {
		return record, mapQueryErr(err)
	}
	return phoneAlbumWritebackRecord(row), nil
}

func (s *Store) ListPendingPhoneAlbumWritebacks(ctx context.Context, userID string) ([]dbstore.PhoneAlbumWritebackRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListPendingPhoneAlbumWritebacks(ctx, owner)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	out := make([]dbstore.PhoneAlbumWritebackRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, phoneAlbumWritebackRecord(row))
	}
	return out, nil
}

func (s *Store) MarkPhoneAlbumWritebackWritten(ctx context.Context, userID, pairKey, contentHash string) (dbstore.PhoneAlbumWritebackRecord, error) {
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return dbstore.PhoneAlbumWritebackRecord{}, err
	}
	row, err := s.queries.MarkPhoneAlbumWritebackWritten(ctx, dbsqlc.MarkPhoneAlbumWritebackWrittenParams{
		UserID:      owner,
		PairKey:     pairKey,
		ContentHash: contentHash,
	})
	if err != nil {
		return dbstore.PhoneAlbumWritebackRecord{}, mapQueryErr(err)
	}
	return phoneAlbumWritebackRecord(row), nil
}
