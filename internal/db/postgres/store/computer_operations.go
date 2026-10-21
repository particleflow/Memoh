package postgresstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func computerIDs(r dbstore.ComputerOperationRecord) ([]pgtype.UUID, error) {
	ids := make([]pgtype.UUID, 0, 5)
	for i, value := range []string{r.ID, r.BotID, r.SessionID, r.UserID, r.TargetID} {
		if i == 4 && value == "" {
			ids = append(ids, pgtype.UUID{})
			continue
		}
		id, err := db.ParseUUID(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func operationRecord(r dbsqlc.ComputerOperation) dbstore.ComputerOperationRecord {
	return dbstore.ComputerOperationRecord{ID: r.ID.String(), BotID: r.BotID.String(), SessionID: r.SessionID.String(), UserID: r.UserID.String(), TargetID: r.TargetID.String(), RunID: r.RunID, Fingerprint: r.Fingerprint, TargetFingerprint: r.TargetFingerprint, State: r.State, Details: r.Details, CancelRequested: r.CancelRequested, Revision: r.Revision}
}

func (s *Store) ClaimComputerOperation(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, bool, error) {
	ids, err := computerIDs(r)
	if err != nil {
		return r, false, err
	}
	row, err := s.queries.CreateComputerOperation(ctx, dbsqlc.CreateComputerOperationParams{ID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3], TargetID: ids[4], RunID: r.RunID, Fingerprint: r.Fingerprint, TargetFingerprint: r.TargetFingerprint, Details: r.Details})
	if err == nil {
		return operationRecord(row), true, nil
	}
	if !errors.Is(mapQueryErr(err), db.ErrNotFound) {
		return r, false, err
	}
	row, err = s.queries.FindComputerOperation(ctx, dbsqlc.FindComputerOperationParams{BotID: ids[1], SessionID: ids[2], UserID: ids[3], TargetID: ids[4], RunID: r.RunID, Fingerprint: r.Fingerprint})
	return operationRecord(row), false, mapQueryErr(err)
}

func (s *Store) ReadComputerOperation(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	ids, err := computerIDs(r)
	if err != nil {
		return r, err
	}
	row, err := s.queries.GetComputerOperation(ctx, dbsqlc.GetComputerOperationParams{ID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3]})
	return operationRecord(row), mapQueryErr(err)
}

func (s *Store) SaveComputerOperation(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	ids, err := computerIDs(r)
	if err != nil {
		return r, err
	}
	row, err := s.queries.UpdateComputerOperation(ctx, dbsqlc.UpdateComputerOperationParams{ID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3], State: r.State, Details: r.Details, CancelRequested: r.CancelRequested, Revision: r.Revision})
	return operationRecord(row), mapQueryErr(err)
}

func (s *Store) OwnedAgenvoRuntime(ctx context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	user, err := db.ParseUUID(owner)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	runtime, err := db.ParseUUID(id)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	row, err := s.queries.GetOwnedAgenvoRuntime(ctx, dbsqlc.GetOwnedAgenvoRuntimeParams{ID: runtime, UserID: user})
	return userRuntimeRecord(row), mapQueryErr(err)
}

func (s *Store) ConfigureAgenvoRuntime(ctx context.Context, owner, id string, previous, next dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	user, err := db.ParseUUID(owner)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	runtime, err := db.ParseUUID(id)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	row, err := s.queries.UpdateAgenvoRuntimeConfig(ctx, dbsqlc.UpdateAgenvoRuntimeConfigParams{ID: runtime, UserID: user, ExpectedConfig: previous, ProviderConfig: next})
	return userRuntimeRecord(row), mapQueryErr(err)
}
