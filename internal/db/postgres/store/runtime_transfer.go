package postgresstore

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func (s *Store) ReadOwnedRuntimeTransfer(ctx context.Context, runtime, owner string) (dbstore.RuntimeTransferRecord, error) {
	id, err := db.ParseUUID(runtime)
	if err != nil {
		return dbstore.RuntimeTransferRecord{}, err
	}
	user, err := db.ParseUUID(owner)
	if err != nil {
		return dbstore.RuntimeTransferRecord{}, err
	}
	r, err := s.queries.ReadOwnedTransferCredential(ctx, dbsqlc.ReadOwnedTransferCredentialParams{ID: id, UserID: user})
	return runtimeTransferRecord(dbsqlc.ReadTransferCredentialRow(r)), mapQueryErr(err)
}

func (s *Store) ReadRuntimeTransfer(ctx context.Context, credentialID string) (dbstore.RuntimeTransferRecord, error) {
	r, err := s.queries.ReadTransferCredential(ctx, credentialID)
	return runtimeTransferRecord(r), mapQueryErr(err)
}

func runtimeTransferRecord(r dbsqlc.ReadTransferCredentialRow) dbstore.RuntimeTransferRecord {
	return dbstore.RuntimeTransferRecord{RuntimeID: r.ID.String(), UserID: r.UserID.String(), Provider: r.Provider, Config: r.ProviderConfig, CredentialID: r.TransferCredentialID, Digest: r.TransferCredentialDigest, TargetDigest: r.TransferTargetDigest, ExpiresAt: db.TimeFromPg(r.TransferExpiresAt)}
}

func (s *Store) ChangeRuntimeTransfer(ctx context.Context, old, next dbstore.RuntimeTransferRecord) (bool, error) {
	id, err := db.ParseUUID(old.RuntimeID)
	if err != nil {
		return false, err
	}
	user, err := db.ParseUUID(old.UserID)
	if err != nil {
		return false, err
	}
	n, err := s.queries.ChangeTransferCredential(ctx, dbsqlc.ChangeTransferCredentialParams{ID: id, UserID: user, ExpectedID: old.CredentialID, ExpectedConfig: old.Config, CredentialID: next.CredentialID, Digest: next.Digest, TargetDigest: next.TargetDigest, ExpiresAt: pgtype.Timestamptz{Time: next.ExpiresAt, Valid: !next.ExpiresAt.IsZero()}})
	return n == 1, mapQueryErr(err)
}
