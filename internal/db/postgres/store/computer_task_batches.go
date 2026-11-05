package postgresstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func computerTaskBatchRecord(r dbsqlc.ComputerTaskBatch) dbstore.ComputerTaskBatchRecord {
	return dbstore.ComputerTaskBatchRecord{ID: r.ID.String(), BotID: r.BotID.String(), SessionID: r.SessionID.String(), UserID: r.UserID.String(), TargetID: r.TargetID.String(), RunID: r.RunID, Label: r.Label, Fingerprint: r.Fingerprint, State: r.State, Items: r.Items, Delivery: r.ResultDelivery, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time}
}

func (s *Store) ClaimComputerTaskBatch(ctx context.Context, rec dbstore.ComputerTaskBatchRecord) (dbstore.ComputerTaskBatchRecord, bool, error) {
	ids, err := computerIDs(dbstore.ComputerOperationRecord{ID: rec.ID, BotID: rec.BotID, SessionID: rec.SessionID, UserID: rec.UserID, TargetID: rec.TargetID})
	if err != nil {
		return rec, false, err
	}
	row, err := s.queries.CreateComputerTaskBatch(ctx, dbsqlc.CreateComputerTaskBatchParams{ID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3], TargetID: ids[4], RunID: rec.RunID, Label: rec.Label, Fingerprint: rec.Fingerprint, Items: rec.Items, State: rec.State})
	if err == nil {
		return computerTaskBatchRecord(row), true, nil
	}
	if !errors.Is(mapQueryErr(err), db.ErrNotFound) {
		return rec, false, mapQueryErr(err)
	}
	row, err = s.queries.FindComputerTaskBatch(ctx, dbsqlc.FindComputerTaskBatchParams{BotID: ids[1], SessionID: ids[2], UserID: ids[3], RunID: rec.RunID, Fingerprint: rec.Fingerprint})
	return computerTaskBatchRecord(row), false, mapQueryErr(err)
}

func (s *Store) ReadComputerTaskBatch(ctx context.Context, id, bot, session, user string) (dbstore.ComputerTaskBatchRecord, error) {
	ids, err := computerIDs(dbstore.ComputerOperationRecord{ID: id, BotID: bot, SessionID: session, UserID: user})
	if err != nil {
		return dbstore.ComputerTaskBatchRecord{}, err
	}
	row, err := s.queries.GetComputerTaskBatch(ctx, dbsqlc.GetComputerTaskBatchParams{ID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3]})
	return computerTaskBatchRecord(row), mapQueryErr(err)
}

func (s *Store) ListSessionComputerTaskBatches(ctx context.Context, bot, session, user string, limit int32) ([]dbstore.ComputerTaskBatchRecord, error) {
	ids, err := computerIDs(dbstore.ComputerOperationRecord{BotID: bot, SessionID: session, UserID: user})
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListSessionComputerTaskBatches(ctx, dbsqlc.ListSessionComputerTaskBatchesParams{BotID: ids[1], SessionID: ids[2], UserID: ids[3], LimitRows: limit})
	if err != nil {
		return nil, mapQueryErr(err)
	}
	return computerTaskBatchRecords(rows), nil
}

// ListActiveComputerTaskBatches returns the scan work list in dispatch order.
// The scan is the only writer, so no claim is needed; per-batch transitions
// still take the row lock.
func (s *Store) ListActiveComputerTaskBatches(ctx context.Context) ([]dbstore.ComputerTaskBatchRecord, error) {
	rows, err := s.queries.ListActiveComputerTaskBatches(ctx)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	return computerTaskBatchRecords(rows), nil
}

// ListCompletedComputerTaskBatches is the aggregate-delivery work list: a batch
// completes once, then its single result envelope is published.
func (s *Store) ListCompletedComputerTaskBatches(ctx context.Context) ([]dbstore.ComputerTaskBatchRecord, error) {
	rows, err := s.queries.ListCompletedComputerTaskBatches(ctx)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	return computerTaskBatchRecords(rows), nil
}

func computerTaskBatchRecords(rows []dbsqlc.ComputerTaskBatch) []dbstore.ComputerTaskBatchRecord {
	out := make([]dbstore.ComputerTaskBatchRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, computerTaskBatchRecord(row))
	}
	return out
}

func (s *Store) ReadComputerTaskBatchDelivery(ctx context.Context, id string) ([]byte, error) {
	batch, err := db.ParseUUID(id)
	if err != nil {
		return nil, err
	}
	raw, err := s.queries.ReadComputerTaskBatchDelivery(ctx, batch)
	return raw, mapQueryErr(err)
}

func (s *Store) SaveComputerTaskBatchDelivery(ctx context.Context, id string, expected, delivery []byte) ([]byte, error) {
	batch, err := db.ParseUUID(id)
	if err != nil {
		return nil, err
	}
	raw, err := s.queries.SaveComputerTaskBatchDelivery(ctx, dbsqlc.SaveComputerTaskBatchDeliveryParams{ID: batch, Expected: expected, Delivery: delivery})
	return raw, mapQueryErr(err)
}

// ChangeComputerTaskBatch locks the batch row and applies one mutation. The
// items/state write commits together with the caller's mutation; an error
// rolls the whole transition back, so a failed dispatch attempt never consumes
// a queue position.
func (s *Store) ChangeComputerTaskBatch(ctx context.Context, id string, mutate func(*dbstore.ComputerTaskBatchRecord) error) error {
	batchID, err := db.ParseUUID(id)
	if err != nil {
		return err
	}
	if s.pool == nil {
		// A pool-less store is already a single transactional connection (the
		// test fixtures hand one in); run the transition inline.
		return s.changeComputerTaskBatch(ctx, s.queries, batchID, mutate)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err = s.changeComputerTaskBatch(ctx, s.queries.WithTx(tx), batchID, mutate); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (*Store) changeComputerTaskBatch(ctx context.Context, queries *dbsqlc.Queries, batchID pgtype.UUID, mutate func(*dbstore.ComputerTaskBatchRecord) error) error {
	row, err := queries.LockComputerTaskBatch(ctx, batchID)
	if err != nil {
		return mapQueryErr(err)
	}
	rec := computerTaskBatchRecord(row)
	if err = mutate(&rec); err != nil {
		return err
	}
	if _, err = queries.UpdateComputerTaskBatch(ctx, dbsqlc.UpdateComputerTaskBatchParams{ID: batchID, Items: rec.Items, State: rec.State}); err != nil {
		return mapQueryErr(err)
	}
	return nil
}
