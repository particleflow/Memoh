package postgresstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func (s *Store) ReadComputerAdmission(ctx context.Context, owner, connection, device, instance string) (dbstore.ComputerAdmission, error) {
	user, err := db.ParseUUID(owner)
	if err != nil {
		return dbstore.ComputerAdmission{}, err
	}
	conn, err := db.ParseUUID(connection)
	if err != nil {
		return dbstore.ComputerAdmission{}, err
	}
	row, err := s.queries.ReadComputerAdmission(ctx, dbsqlc.ReadComputerAdmissionParams{OwnerID: user, ConnectionID: conn, DeviceID: device, InstanceID: instance})
	if err != nil {
		return dbstore.ComputerAdmission{}, mapQueryErr(err)
	}
	// A closed gate through another connection cannot establish equivalent scope.
	if row.ConnectionID != conn {
		return dbstore.ComputerAdmission{}, db.ErrComputerAdmissionScope
	}
	return dbstore.ComputerAdmission{ConnectionID: connection, DeviceID: device, InstanceID: instance, RequestID: row.RequestID.String(), Paused: row.Paused, Revision: row.Revision}, nil
}

func (s *Store) ChangeComputerAdmission(ctx context.Context, owner, runtime string, config dbstore.JSON, request string, revision int64, paused bool) (dbstore.ComputerAdmission, error) {
	user, err := db.ParseUUID(owner)
	if err != nil {
		return dbstore.ComputerAdmission{}, err
	}
	id, err := db.ParseUUID(runtime)
	if err != nil {
		return dbstore.ComputerAdmission{}, err
	}
	req, err := db.ParseUUID(request)
	if err != nil {
		return dbstore.ComputerAdmission{}, err
	}
	raw, err := s.queries.ChangeComputerAdmission(ctx, dbsqlc.ChangeComputerAdmissionParams{OwnerID: user, RuntimeID: id, ExpectedConfig: config, RequestID: req, ExpectedRevision: revision, Paused: paused})
	if err != nil {
		return dbstore.ComputerAdmission{}, mapQueryErr(err)
	}
	var out dbstore.ComputerAdmission
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) OwnedNativeProfileTarget(ctx context.Context, owner, runtime, target string) (bool, error) {
	u, err := db.ParseUUID(owner)
	if err != nil {
		return false, err
	}
	r, err := db.ParseUUID(runtime)
	if err != nil {
		return false, err
	}
	t, err := db.ParseUUID(target)
	if err != nil {
		return false, err
	}
	ok, err := s.queries.OwnedNativeProfileTarget(ctx, dbsqlc.OwnedNativeProfileTargetParams{OwnerID: u, RuntimeID: r, TargetID: t})
	return ok, mapQueryErr(err)
}

// CutoverPausedNativeProfile executes a single SQL statement. The database
// function rechecks all admission and configuration facts under the owner lock.
func (s *Store) CutoverPausedNativeProfile(ctx context.Context, owner, runtime, target string, before, after dbstore.JSON, instance, request string, revision int64) error {
	u, err := db.ParseUUID(owner)
	if err != nil {
		return err
	}
	r, err := db.ParseUUID(runtime)
	if err != nil {
		return err
	}
	t, err := db.ParseUUID(target)
	if err != nil {
		return err
	}
	id, err := db.ParseUUID(request)
	if err != nil {
		return err
	}
	params := dbsqlc.CutoverPausedNativeProfileParams{OwnerID: u, RuntimeID: r, TargetID: t, ExpectedConfig: before, ProposedConfig: after, InstanceID: instance, RequestID: id, ExpectedRevision: revision}
	for attempt := 0; attempt < 2; attempt++ {
		_, err = s.queries.CutoverPausedNativeProfile(ctx, params)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "40P01" || ctx.Err() != nil {
			break
		}
	}
	return mapQueryErr(err)
}
