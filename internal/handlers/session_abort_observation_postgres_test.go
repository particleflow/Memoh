package handlers

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

func TestPostgresAbortObservationPreservesRunAndScope(t *testing.T) {
	dsn := os.Getenv("MEMOH_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MEMOH_TEST_POSTGRES_DSN is not set")
	}
	ctx := t.Context()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["memoh.team_id"] = "00000000-0000-0000-0000-000000000001"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	user, bot, session, run := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `WITH created_user AS (
INSERT INTO users(id, username, is_active) VALUES ($1, $2, true) RETURNING id)
INSERT INTO team_members(user_id, role) SELECT id, 'admin' FROM created_user`, user, "abort-observation-"+user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		// Only this test's new fixture; never enumerate or alter other records.
		_, _ = pool.Exec(cleanup, "DELETE FROM bots WHERE id=$1", bot)
		_, _ = pool.Exec(cleanup, "DELETE FROM users WHERE id=$1", user)
	})
	// The canonical snapshot retains the legacy type column; an existing
	// database upgraded through 0033 has removed it. Exercise the same fixture
	// and assertions against either supported migration path.
	var legacyBotType bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='bots' AND column_name='type')`).Scan(&legacyBotType); err != nil {
		t.Fatal(err)
	}
	insertBot := `INSERT INTO bots(id, owner_user_id, name) VALUES ($1,$2,'synthetic-abort-observation')`
	if legacyBotType {
		insertBot = `INSERT INTO bots(id, owner_user_id, type, name) VALUES ($1,$2,'personal','synthetic-abort-observation')`
	}
	if _, err := pool.Exec(ctx, insertBot, bot, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bot_sessions(id, bot_id, channel_type, runtime_type) VALUES ($1,$2,'local','model')`, session, bot); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint,abort_requested_at)
VALUES ($1,$2,$3,'synthetic', $4,1,'aborted','{}','synthetic',now())`, run, bot, session, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := pool.QueryRow(ctx, `SELECT md5(row_to_json(r)::text) FROM session_runs r WHERE run_id=$1`, run).Scan(&before); err != nil {
		t.Fatal(err)
	}
	role := "abort_obs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN NOSUPERUSER"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DROP OWNED BY "+quoted)
		_, _ = pool.Exec(context.Background(), "DROP ROLE "+quoted)
	}()
	if _, err := pool.Exec(ctx, "GRANT USAGE ON SCHEMA public TO "+quoted+"; GRANT SELECT ON session_runs TO "+quoted+"; GRANT SELECT, INSERT ON session_run_abort_controls TO "+quoted); err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET ROLE "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), "RESET ROLE") }()
	q := sqlc.New(conn)
	key := sqlc.GetSessionRunAbortControlParams{BotID: testUUID(bot), SessionID: testUUID(session), RunID: testUUID(run)}
	target := sqlc.SessionRunMatchesAbortScopeParams(key)
	if matched, err := q.SessionRunMatchesAbortScope(ctx, target); err != nil || !matched {
		t.Fatalf("own run scope=%v, %v", matched, err)
	}
	foreign := target
	foreign.SessionID = testUUID(uuid.NewString())
	if matched, err := q.SessionRunMatchesAbortScope(ctx, foreign); err != nil || matched {
		t.Fatalf("mismatched run scope=%v, %v", matched, err)
	}
	if _, err := q.GetSessionRunAbortControl(ctx, key); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old run acquired invented observation: %v", err)
	}
	arg := sqlc.RecordSessionRunAbortControlParams{
		BotID: key.BotID, SessionID: key.SessionID, RunID: key.RunID,
		RequestHash: strings.Repeat("a", 64), ClientReason: "user_stop",
		ReceivedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	wrong := arg
	wrong.BotID = testUUID(uuid.NewString())
	if n, err := q.RecordSessionRunAbortControl(ctx, wrong); err != nil || n != 0 {
		t.Fatalf("cross-bot write=%d, %v", n, err)
	}
	wrong = arg
	wrong.SessionID = testUUID(uuid.NewString())
	if n, err := q.RecordSessionRunAbortControl(ctx, wrong); err != nil || n != 0 {
		t.Fatalf("cross-session write=%d, %v", n, err)
	}
	if _, err := conn.Exec(ctx, "SET memoh.team_id='99999999-9999-4999-8999-999999999999'"); err != nil {
		t.Fatal(err)
	}
	if n, err := q.RecordSessionRunAbortControl(ctx, arg); err != nil || n != 0 {
		t.Fatalf("cross-team write=%d, %v", n, err)
	}
	if _, err := q.GetSessionRunAbortControl(ctx, key); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-team read: %v", err)
	}
	if _, err := conn.Exec(ctx, "RESET memoh.team_id"); err != nil {
		t.Fatal(err)
	}
	if n, err := q.RecordSessionRunAbortControl(ctx, arg); err != nil || n != 1 {
		t.Fatalf("observation write=%d, %v", n, err)
	}
	arg.ClientReason = "bot_switch"
	if n, err := q.RecordSessionRunAbortControl(ctx, arg); err != nil || n != 0 {
		t.Fatalf("duplicate control changed record: %d, %v", n, err)
	}
	row, err := q.GetSessionRunAbortControl(ctx, key)
	if err != nil || row.Source != "websocket" || row.ClientReason != "user_stop" {
		t.Fatalf("observation = %#v, %v", row, err)
	}
	if row.ReceivedAt.Time.Before(arg.ReceivedAt.Time.Add(-time.Microsecond)) {
		t.Fatal("new request was backdated")
	}
	var after string
	if err := pool.QueryRow(ctx, `SELECT md5(row_to_json(r)::text) FROM session_runs r WHERE run_id=$1`, run).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("observation changed original terminal history")
	}
	t.Log("generated queries: original run unchanged; old source absent; new control distinct; duplicate/cross-bot/cross-session/cross-team writes=0; RLS read denied")
}
