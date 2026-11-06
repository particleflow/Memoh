//go:build integration

package db_test

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
)

func TestSessionAbortOutcomeMigrationScopeAndIdempotency(t *testing.T) {
	f := newAdmissionFixture(t)
	run := uuid.NewString()
	f.exec(t, `INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,'synthetic-abort-outcome',$4,1,'aborted','{}','synthetic')`, run, f.bot, f.session, uuid.NewString())
	var before string
	if err := f.conn.QueryRow(t.Context(), `SELECT md5(row_to_json(r)::text) FROM session_runs r WHERE run_id=$1`, run).Scan(&before); err != nil {
		t.Fatal(err)
	}
	q := dbsqlc.New(f.conn)
	key := dbsqlc.GetSessionRunAbortControlParams{BotID: testAbortUUID(t, f.bot), SessionID: testAbortUUID(t, f.session), RunID: testAbortUUID(t, run)}
	received := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	incoming := dbsqlc.RecordSessionRunAbortControlParams{BotID: key.BotID, SessionID: key.SessionID, RunID: key.RunID, RequestHash: strings.Repeat("a", 64), ClientReason: "user_stop", ReceivedAt: received}
	if n, e := q.RecordSessionRunAbortControl(t.Context(), incoming); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	// Upgrade leaves old observations unknown; down/up preserves their old facts.
	for _, name := range []string{"0179_session_abort_outcome.down.sql", "0179_session_abort_outcome.up.sql", "0179_session_abort_outcome.up.sql"} {
		raw, e := fs.ReadFile(postgresMigrationsFS(t), name)
		if e != nil {
			t.Fatal(e)
		}
		f.exec(t, string(raw))
	}
	old, e := q.GetSessionRunAbortControl(t.Context(), key)
	if e != nil || old.CompletedAt.Valid || old.Applied.Valid || old.ErrorClass.Valid || old.ServerSource.Valid {
		t.Fatal("invented old completion", old, e)
	}
	complete := dbsqlc.CompleteSessionRunAbortControlParams{BotID: key.BotID, SessionID: key.SessionID, RunID: key.RunID, RequestHash: incoming.RequestHash, ReceivedAt: received, ClientReason: "unspecified", CompletedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}, Applied: pgtype.Bool{Bool: true, Valid: true}, ErrorClass: pgtype.Text{String: "none", Valid: true}, ServerSource: pgtype.Text{String: "runtime_controller", Valid: true}}
	for _, field := range []string{"bot", "session", "team"} {
		bad := complete
		query := q
		switch field {
		case "bot":
			bad.BotID = testAbortUUID(t, uuid.NewString())
		case "session":
			bad.SessionID = testAbortUUID(t, uuid.NewString())
		case "team":
			conn := rlsConn(t, f.pool, teamMigrationDSN(t))
			if _, e = conn.Exec(t.Context(), `SELECT set_config('memoh.team_id',$1,false)`, uuid.NewString()); e != nil {
				t.Fatal(e)
			}
			query = dbsqlc.New(conn)
		}
		if n, e := query.CompleteSessionRunAbortControl(t.Context(), bad); e != nil || n != 0 {
			t.Fatal("foreign completion", field, n, e)
		}
	}
	if n, e := q.CompleteSessionRunAbortControl(t.Context(), complete); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	changed := complete
	changed.Applied.Bool = false
	changed.ErrorClass.String = "control_error"
	if n, e := q.CompleteSessionRunAbortControl(t.Context(), changed); e != nil || n != 0 {
		t.Fatal("duplicate overwrote completion", n, e)
	}
	got, e := q.GetSessionRunAbortControl(t.Context(), key)
	if e != nil || !got.Applied.Valid || !got.Applied.Bool || got.ClientReason != "user_stop" || got.ErrorClass.String != "none" {
		t.Fatal("incoming/result facts changed", got, e)
	}
	// Result-first order is legal; a delayed incoming observation cannot clear it.
	complete.RequestHash = strings.Repeat("b", 64)
	if n, e := q.CompleteSessionRunAbortControl(t.Context(), complete); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	incoming.RequestHash = complete.RequestHash
	if n, e := q.RecordSessionRunAbortControl(t.Context(), incoming); e != nil || n != 0 {
		t.Fatal("late incoming changed row", n, e)
	}
	var count int
	var after string
	if e = f.conn.QueryRow(t.Context(), `SELECT count(*) FROM session_run_abort_controls WHERE run_id=$1 AND completed_at IS NOT NULL`, run).Scan(&count); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	if e = f.conn.QueryRow(t.Context(), `SELECT md5(row_to_json(r)::text) FROM session_runs r WHERE run_id=$1`, run).Scan(&after); e != nil || before != after {
		t.Fatal("run history changed", e)
	}
}

func testAbortUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	var out pgtype.UUID
	if e := out.Scan(value); e != nil {
		t.Fatal(e)
	}
	return out
}
