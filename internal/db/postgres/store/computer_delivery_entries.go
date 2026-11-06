package postgresstore

import (
	"context"
	"errors"
	"strconv"

	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
)

func (s *Store) ReadComputerDeliveryEntry(ctx context.Context, r dbstore.ComputerOperationRecord, id string) (dbstore.ComputerDeliveryEntry, error) {
	ids, err := computerIDs(r)
	if err != nil {
		return dbstore.ComputerDeliveryEntry{}, err
	}
	row, err := s.queries.GetComputerDeliveryEntry(ctx, dbsqlc.GetComputerDeliveryEntryParams{OperationID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3], DeliveryID: id})
	return dbstore.ComputerDeliveryEntry{DeliveryID: row.DeliveryID, Version: int(row.Version), Data: row.Entry, Ordinal: row.Ordinal}, mapQueryErr(err)
}

func (s *Store) SaveComputerDeliveryEntry(ctx context.Context, r dbstore.ComputerOperationRecord, entry dbstore.ComputerDeliveryEntry) (dbstore.ComputerOperationRecord, error) {
	ids, err := computerIDs(r)
	if err != nil {
		return r, err
	}
	if entry.Version < 1 || entry.Version > 4 {
		return r, errors.New("unsupported delivery entry version")
	}
	row, err := s.queries.PutComputerDeliveryEntry(ctx, dbsqlc.PutComputerDeliveryEntryParams{OperationID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3], ExpectedRevision: r.Revision, DeliveryID: entry.DeliveryID, EntryVersion: int32(entry.Version), Entry: entry.Data})
	out := operationRecord(dbsqlc.ComputerOperation(row))
	out.DeliveryEntries = r.DeliveryEntries
	return out, mapQueryErr(err)
}

func (s *Store) ListComputerDeliveryEntries(ctx context.Context, r dbstore.ComputerOperationRecord, after string, limit int) ([]dbstore.ComputerDeliveryEntry, error) {
	ids, err := computerIDs(r)
	if err != nil {
		return nil, err
	}
	// One producer page plus lookahead, never a bound on retained history.
	if limit < 1 || limit > deliverymeta.MaxArtifacts+1 {
		return nil, errors.New("invalid delivery page size")
	}
	cursor := int64(0)
	if after != "" {
		cursor, err = strconv.ParseInt(after, 10, 64)
		if err != nil || cursor < 0 || strconv.FormatInt(cursor, 10) != after {
			return nil, errors.New("invalid delivery cursor")
		}
	}
	rows, err := s.queries.ListComputerDeliveryEntries(ctx, dbsqlc.ListComputerDeliveryEntriesParams{OperationID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3], AfterOrdinal: cursor, PageSize: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]dbstore.ComputerDeliveryEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, dbstore.ComputerDeliveryEntry{DeliveryID: row.DeliveryID, Version: int(row.Version), Data: row.Entry, Ordinal: row.Ordinal})
	}
	return out, nil
}
