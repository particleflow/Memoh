package postgresstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

const (
	authSessionRevokeReplaced        = "replaced"
	authSessionRevokePasswordChanged = "password_changed"
	pgUniqueViolation                = "23505"
)

func (s *Store) CreateAuthSession(ctx context.Context, input dbstore.CreateAuthSessionInput) (dbstore.AuthSessionRecord, error) {
	userID, err := db.ParseUUID(input.UserID)
	if err != nil {
		return dbstore.AuthSessionRecord{}, err
	}
	params := dbsqlc.CreateAuthSessionParams{
		UserID:      userID,
		DeviceID:    input.DeviceID,
		DeviceName:  input.DeviceName,
		RefreshHash: input.RefreshHash,
		ExpiresAt:   pgtype.Timestamptz{Time: input.ExpiresAt, Valid: true},
	}
	var row dbsqlc.AuthSession
	create := func(queries *dbsqlc.Queries) error {
		if input.DeviceID != "" {
			if _, err := queries.RevokeAuthSessionsForDevice(ctx, dbsqlc.RevokeAuthSessionsForDeviceParams{
				RevokeReason: text(authSessionRevokeReplaced),
				UserID:       userID,
				DeviceID:     input.DeviceID,
			}); err != nil {
				return err
			}
		}
		created, err := queries.CreateAuthSession(ctx, params)
		if err != nil {
			return err
		}
		row = created
		return nil
	}
	// A concurrent login on the same device inserts its row between this
	// transaction's revoke and insert. The retry's revoke then sees the
	// committed row and replaces it.
	err = s.inTx(ctx, create)
	if isUniqueViolation(err) {
		err = s.inTx(ctx, create)
		if isUniqueViolation(err) {
			return dbstore.AuthSessionRecord{}, fmt.Errorf("%w: %w", db.ErrAuthSessionConflict, err)
		}
	}
	if err != nil {
		return dbstore.AuthSessionRecord{}, err
	}
	return authSessionRecord(row), nil
}

func (s *Store) GetAuthSessionByID(ctx context.Context, sessionID string) (dbstore.AuthSessionRecord, error) {
	id, err := db.ParseUUID(sessionID)
	if err != nil {
		return dbstore.AuthSessionRecord{}, err
	}
	row, err := s.queries.GetAuthSessionByID(ctx, id)
	if err != nil {
		return dbstore.AuthSessionRecord{}, mapQueryErr(err)
	}
	return authSessionRecord(row), nil
}

func (s *Store) GetActiveAuthSession(ctx context.Context, sessionID, userID string) error {
	id, err := db.ParseUUID(sessionID)
	if err != nil {
		return err
	}
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return err
	}
	_, err = s.queries.GetActiveAuthSession(ctx, dbsqlc.GetActiveAuthSessionParams{ID: id, UserID: owner})
	return mapQueryErr(err)
}

func (s *Store) TouchAuthSession(ctx context.Context, sessionID string, expiresAt time.Time) (int64, error) {
	id, err := db.ParseUUID(sessionID)
	if err != nil {
		return 0, err
	}
	return s.queries.TouchAuthSession(ctx, dbsqlc.TouchAuthSessionParams{
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
		ID:        id,
	})
}

func (s *Store) RevokeAuthSession(ctx context.Context, sessionID, userID, reason string) (int64, error) {
	id, err := db.ParseUUID(sessionID)
	if err != nil {
		return 0, err
	}
	owner, err := db.ParseUUID(userID)
	if err != nil {
		return 0, err
	}
	return s.queries.RevokeAuthSession(ctx, dbsqlc.RevokeAuthSessionParams{
		RevokeReason: text(reason),
		ID:           id,
		UserID:       owner,
	})
}

func (s *Store) DeleteStaleAuthSessions(ctx context.Context) (int64, error) {
	return s.queries.DeleteStaleAuthSessions(ctx)
}

// inTx runs fn in one transaction. A store built without a pool (focused
// store tests only) runs fn directly.
func (s *Store) inTx(ctx context.Context, fn func(*dbsqlc.Queries) error) error {
	if s.pool == nil {
		return fn(s.queries)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

func authSessionRecord(row dbsqlc.AuthSession) dbstore.AuthSessionRecord {
	return dbstore.AuthSessionRecord{
		ID:          row.ID.String(),
		UserID:      row.UserID.String(),
		DeviceID:    row.DeviceID,
		DeviceName:  row.DeviceName,
		RefreshHash: row.RefreshHash,
		CreatedAt:   db.TimeFromPg(row.CreatedAt),
		LastUsedAt:  db.TimeFromPg(row.LastUsedAt),
		ExpiresAt:   db.TimeFromPg(row.ExpiresAt),
		RevokedAt:   db.TimeFromPg(row.RevokedAt),
	}
}
