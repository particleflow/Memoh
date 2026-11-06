package postgresstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

var _ dbstore.ComputerOperationAdmissionStore = (*Store)(nil)

// SaveComputerOperationWithAdmission serializes a pre-dispatch reservation with
// the owner's admission transition. The pool supplies the existing team/RLS
// binding; no untransactional fallback or tenant override is permitted here.
func (s *Store) SaveComputerOperationWithAdmission(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	if s == nil || s.pool == nil {
		return dbstore.ComputerOperationRecord{}, errors.New("computer operation admission requires a transaction pool")
	}
	ids, err := computerIDs(r)
	if err != nil {
		return dbstore.ComputerOperationRecord{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return dbstore.ComputerOperationRecord{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	queries := s.queries.WithTx(tx)
	transaction := NewWithQueries(queries)
	current, err := transaction.ReadComputerOperation(ctx, r)
	if err != nil {
		return dbstore.ComputerOperationRecord{}, err
	}
	// Bind admission to the persisted operation, not a caller-substituted target.
	// A concurrent cancellation or progress write also loses the revision CAS.
	if r.CancelRequested || current.CancelRequested || current.Revision != r.Revision ||
		current.TargetID != r.TargetID || current.RunID != r.RunID ||
		current.Fingerprint != r.Fingerprint || current.TargetFingerprint != r.TargetFingerprint {
		return dbstore.ComputerOperationRecord{}, db.ErrNotFound
	}
	if err := queries.AssertComputerOperationAdmission(ctx, dbsqlc.AssertComputerOperationAdmissionParams{BotID: ids[1], TargetID: ids[4]}); err != nil {
		return dbstore.ComputerOperationRecord{}, mapQueryErr(err)
	}
	out, err := transaction.SaveComputerOperation(ctx, r)
	if err != nil {
		return dbstore.ComputerOperationRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		if !errors.Is(err, pgx.ErrTxCommitRollback) {
			err = fmt.Errorf("%w: %w", db.ErrCommitOutcomeUnknown, err)
		}
		return dbstore.ComputerOperationRecord{}, err
	}
	return out, nil
}
