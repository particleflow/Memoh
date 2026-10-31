//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	memohdb "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
)

type admissionFixture struct {
	pool                                            *pgxpool.Pool
	conn                                            *pgxpool.Conn
	store                                           *postgresstore.Store
	owner, bot, session, runtime, mount, connection string
	config                                          dbstore.JSON
}

func newAdmissionFixture(t *testing.T) admissionFixture {
	t.Helper()
	pool := freshMigratedDB(t)
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Release)
	f := admissionFixture{pool: pool, conn: conn, store: postgresstore.NewWithQueries(dbsqlc.New(conn)), owner: uuid.NewString(), bot: uuid.NewString(), session: uuid.NewString(), connection: uuid.NewString()}
	f.exec(t, "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID)
	f.exec(t, "INSERT INTO users(id,username) VALUES($1,'admission-test')", f.owner)
	f.exec(t, "INSERT INTO team_members(user_id) VALUES($1)", f.owner)
	f.exec(t, "INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'admission-test')", f.bot, f.owner)
	f.exec(t, "INSERT INTO bot_sessions(id,bot_id) VALUES($1,$2)", f.session, f.bot)
	f.runtime, f.mount, f.config = f.registration(t, f.connection, "original")
	return f
}

func (f admissionFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.conn.Exec(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func (f admissionFixture) registration(t *testing.T, connection, session string) (string, string, dbstore.JSON) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"connection_id": connection, "device_id": "synthetic-device", "instance_id": "synthetic-herdr", "session": session})
	runtime := uuid.NewString()
	f.exec(t, "INSERT INTO user_runtimes(id,user_id,name,api_token,provider,provider_config,activated_at,pending_expires_at) VALUES($1,$2,$3,$4,'agenvo',$5,now(),NULL)", runtime, f.owner, uuid.NewString(), uuid.NewString(), raw)
	mount := uuid.NewString()
	f.exec(t, "INSERT INTO bot_remote_runtime_bindings(id,bot_id,runtime_id) VALUES($1,$2,$3)", mount, f.bot, runtime)
	return runtime, mount, raw
}

func (f admissionFixture) operation(mount string) dbstore.ComputerOperationRecord {
	return dbstore.ComputerOperationRecord{ID: uuid.NewString(), BotID: f.bot, SessionID: f.session, UserID: f.owner, TargetID: mount, RunID: uuid.NewString(), Fingerprint: uuid.NewString(), TargetFingerprint: "synthetic-registration", Details: []byte(`{"kind":"herdr_pi_task","followups":null,"batch":{"recovery_reader":{"phase":"creating"}}}`)}
}

func (f admissionFixture) change(t *testing.T, request string, rev int64, paused bool) dbstore.ComputerAdmission {
	t.Helper()
	got, err := f.store.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, f.config, request, rev, paused)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestComputerAdmissionDurabilityCoverageAndReservations(t *testing.T) {
	f := newAdmissionFixture(t)
	_, alias, _ := f.registration(t, f.connection, "different-session-and-profile")
	input := f.operation(f.mount)
	op, won, err := f.store.ClaimComputerOperation(t.Context(), input)
	if err != nil || !won {
		t.Fatal(won, err)
	}
	request := uuid.NewString()
	gate := f.change(t, request, 0, true)
	if !gate.Paused || gate.Revision != 1 {
		t.Fatal(gate)
	}
	if got := f.change(t, request, 0, true); got != gate {
		t.Fatal("lost acknowledgement created another pause", got)
	}
	restarted := postgresstore.NewWithQueries(dbsqlc.New(f.conn))
	if got, err := restarted.ReadComputerAdmission(t.Context(), f.owner, f.connection, "synthetic-device", "synthetic-herdr"); err != nil || got != gate {
		t.Fatal("pause lost on restart", got, err)
	}
	for _, target := range []string{f.mount, alias} {
		if _, won, err := f.store.ClaimComputerOperation(t.Context(), f.operation(target)); !errors.Is(err, memohdb.ErrComputerAdmissionPaused) || won {
			t.Fatal("new input admitted", won, err)
		}
	}
	input.ID = uuid.NewString()
	if got, won, err := f.store.ClaimComputerOperation(t.Context(), input); err != nil || won || got.ID != op.ID {
		t.Fatal("idempotent receipt lost", got.ID, won, err)
	}
	// Existing accepted work and unknown recovery-reader state remain writable.
	op.State = "unknown"
	op, err = f.store.SaveComputerOperation(t.Context(), op)
	if err != nil {
		t.Fatal("existing collection denied", err)
	}
	saved := op
	op.Details = []byte(`{"followups":[{"request_id":"new-input","progress":{"state":"reserved"}}]}`)
	if _, err = f.store.SaveComputerOperation(t.Context(), op); !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
		t.Fatal("new follow-up admitted", err)
	}
	if got, err := f.store.ReadComputerOperation(t.Context(), saved); err != nil || got.Revision != saved.Revision || string(got.Details) != string(saved.Details) {
		t.Fatal("rejected reservation changed receipt", err)
	}
	if _, err = f.store.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, f.config, uuid.NewString(), gate.Revision, false); !errors.Is(err, memohdb.ErrComputerAdmissionConflict) {
		t.Fatal("foreign pause released", err)
	}
	gate = f.change(t, request, gate.Revision, false)
	if gate.Paused || gate.Revision != 2 {
		t.Fatal(gate)
	}
	if _, err = f.store.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, f.config, request, gate.Revision, true); !errors.Is(err, memohdb.ErrComputerAdmissionConflict) {
		t.Fatal("old pause request reclosed gate", err)
	}
	// A fresh pause may be taken only with a new intent and the current revision.
	next := uuid.NewString()
	f.change(t, next, gate.Revision, true)
	_, alias, _ = f.registration(t, uuid.NewString(), "ambiguous-alias")
	if _, won, err = f.store.ClaimComputerOperation(t.Context(), f.operation(alias)); won || !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
		t.Fatal("new alias bypassed pause", won, err)
	}
	if _, err = f.store.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, f.config, next, 3, false); !errors.Is(err, memohdb.ErrComputerAdmissionScope) {
		t.Fatal("ambiguous alias attested", err)
	}
	f.exec(t, "UPDATE user_runtimes SET revoked_at=now() WHERE id=(SELECT runtime_id FROM bot_remote_runtime_bindings WHERE id=$1)", alias)
	f.change(t, next, 3, false)
}

func TestComputerAdmissionPreservesAlreadyReservedFollowup(t *testing.T) {
	f := newAdmissionFixture(t)
	op, _, err := f.store.ClaimComputerOperation(t.Context(), f.operation(f.mount))
	if err != nil {
		t.Fatal(err)
	}
	op.Details = []byte(`{"followups":[{"request_id":"accepted-before-pause","progress":{"state":"reserved"}}]}`)
	op, err = f.store.SaveComputerOperation(t.Context(), op)
	if err != nil {
		t.Fatal(err)
	}
	f.change(t, uuid.NewString(), 0, true)
	op.Details = []byte(`{"followups":[{"request_id":"accepted-before-pause","progress":{"state":"unknown"}}],"output":"recovered"}`)
	if _, err = f.store.SaveComputerOperation(t.Context(), op); err != nil {
		t.Fatal("pre-pause reservation could not settle", err)
	}
}

func TestComputerAdmissionPauseSerializesWithClaims(t *testing.T) {
	exerciseAdmissionPauseSerializesWithClaims(t, newAdmissionFixture)
}

func exerciseAdmissionPauseSerializesWithClaims(t *testing.T, fixture func(*testing.T) admissionFixture) {
	for _, first := range []string{"pause", "claim", "pause_followup", "followup"} {
		t.Run(first, func(t *testing.T) {
			f := fixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			input := f.operation(f.mount)
			followup := strings.Contains(first, "followup")
			if followup {
				var err error
				input, _, err = f.store.ClaimComputerOperation(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				input.Details = []byte(`{"followups":[{"request_id":"concurrent-input","progress":{"state":"reserved"}}]}`)
			}
			accept := func(store *postgresstore.Store) error {
				if followup {
					_, err := store.SaveComputerOperation(ctx, input)
					return err
				}
				_, _, err := store.ClaimComputerOperation(ctx, input)
				return err
			}
			tx, err := f.conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			locked := postgresstore.NewWithQueries(dbsqlc.New(tx))
			request := uuid.NewString()
			pauseFirst := strings.HasPrefix(first, "pause")
			if pauseFirst {
				if _, err = locked.ChangeComputerAdmission(ctx, f.owner, f.runtime, f.config, request, 0, true); err != nil {
					t.Fatal(err)
				}
			} else if err = accept(locked); err != nil {
				t.Fatal(err)
			}
			other, err := f.pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Release()
			if _, err = other.Exec(ctx, "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); err != nil {
				t.Fatal(err)
			}
			backend := other.Conn().PgConn().PID()
			ready := make(chan error, 1)
			go func() {
				store := postgresstore.NewWithQueries(dbsqlc.New(other))
				if pauseFirst {
					ready <- accept(store)
				} else {
					_, e := store.ChangeComputerAdmission(ctx, f.owner, f.runtime, f.config, request, 0, true)
					ready <- e
				}
			}()
			// Observe the actual advisory lock wait, not timing-based completion guesses.
			for {
				var waiting bool
				if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='advisory')", backend).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case e := <-ready:
					t.Fatal("operation did not serialize", e)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-ready
			if pauseFirst && !errors.Is(err, memohdb.ErrComputerAdmissionPaused) || !pauseFirst && err != nil {
				t.Fatal("incorrect linearization", first, err)
			}
			if !pauseFirst {
				if _, err = f.store.ReadComputerOperation(ctx, input); err != nil {
					t.Fatal("accepted input lost", err)
				}
			}
		})
	}
}

func TestComputerAdmissionRollbackRefusesLivePause(t *testing.T) {
	f := newAdmissionFixture(t)
	request := uuid.NewString()
	f.change(t, request, 0, true)
	down, err := fs.ReadFile(postgresMigrationsFS(t), "0175_computer_admission.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), string(down)); err == nil {
		t.Fatal("live pause dropped")
	}
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.change(t, request, 1, false)
	tx, err = f.conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), string(down)); err != nil {
		t.Fatal(err)
	}
	// Restore within the same transaction: no operation or receipt data migration.
	up, err := fs.ReadFile(postgresMigrationsFS(t), "0175_computer_admission.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), string(up)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestComputerAdmissionRLSAndUnknownMapping(t *testing.T) {
	f := newAdmissionFixture(t)
	c := rlsConn(t, f.pool, teamMigrationDSN(t))
	if _, err := c.Exec(t.Context(), "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); err != nil {
		t.Fatal(err)
	}
	store := postgresstore.NewWithQueries(dbsqlc.New(c))
	request := uuid.NewString()
	if _, err := store.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, f.config, request, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, won, err := store.ClaimComputerOperation(t.Context(), f.operation(uuid.NewString())); won || !errors.Is(err, memohdb.ErrComputerAdmissionScope) {
		t.Fatal("missing mapping bypassed owner pause", won, err)
	}
	if _, err := c.Exec(t.Context(), "SELECT set_config('memoh.team_id',$1,false)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadComputerAdmission(t.Context(), f.owner, f.connection, "synthetic-device", "synthetic-herdr"); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("foreign team read pause", err)
	}
	if _, err := store.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, f.config, request, 1, false); !errors.Is(err, memohdb.ErrComputerAdmissionScope) {
		t.Fatal("foreign team reopened pause", err)
	}
	if _, _, err := store.ClaimComputerOperation(t.Context(), f.operation(f.mount)); err == nil {
		t.Fatal("foreign team claimed operation")
	}
}
