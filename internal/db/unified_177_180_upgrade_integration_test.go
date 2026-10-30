//go:build integration

package db_test

import (
	"errors"
	"io/fs"
	"maps"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"

	memohdb "github.com/felinics/memoh/internal/db"
)

func TestUnified177To181PreservesAcceptedWorkAndHistory(t *testing.T) {
	// Keep the historical test identity used by CI. The actual up command
	// consumes the current embedded chain, so its endpoint must come from that
	// same source rather than a version constant that predates new migrations.
	source, err := iofs.New(postgresMigrationsFS(t), ".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	expectedVersion, err := source.First()
	if err != nil {
		t.Fatal(err)
	}
	for {
		next, nextErr := source.Next(expectedVersion)
		if errors.Is(nextErr, fs.ErrNotExist) {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		expectedVersion = next
	}
	f := newNativeHerdrFixture(t)
	// Only this newly created disposable fixture is prepared as schema 177.
	// This is not a production rollback procedure or an old-data restoration.
	for _, name := range []string{"0180_schedule_history_retention.down.sql", "0179_session_abort_outcome.down.sql", "0178_native_profile_cutover.down.sql"} {
		raw, err := fs.ReadFile(postgresMigrationsFS(t), name)
		if err != nil {
			t.Fatal(err)
		}
		f.exec(t, string(raw))
	}
	f.exec(t, `UPDATE schema_migrations SET version=177, dirty=false`)
	op, won, err := f.store.ClaimComputerOperation(t.Context(), f.operation(f.mount))
	if err != nil || !won {
		t.Fatal(won, err)
	}
	op.State = "unknown"
	op.Details = []byte(`{"kind":"herdr_pi_task","phase":"batch_submitted","batch":{"recovery_reader":{"phase":"creating"}},"synthetic_receipt":{"identity":"original-receipt","counter":9007199254740993}}`)
	if _, err = f.store.SaveComputerOperation(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	f.change(t, uuid.NewString(), 0, true)
	run := uuid.NewString()
	f.exec(t, `INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,'synthetic-upgrade-run',$4,1,'aborted','{}','synthetic')`, run, f.bot, f.session, uuid.NewString())
	f.exec(t, `INSERT INTO bot_history_messages(bot_id,session_id,role,content) VALUES($1,$2,'assistant','{"role":"assistant","content":"synthetic retained history"}')`, f.bot, f.session)
	f.exec(t, `INSERT INTO session_run_abort_controls(session_id,run_id,request_hash,client_reason,received_at,source) VALUES($1,$2,repeat('a',64),'user_stop',now(),'websocket')`, f.session, run)
	snapshot := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, table := range []string{"users", "team_members", "bots", "bot_sessions", "user_runtimes", "bot_remote_runtime_bindings", "computer_operations", "computer_admission_gates", "session_runs", "bot_history_messages"} {
			var raw string
			if err := f.conn.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM `+table+` r`).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			out[table] = raw
		}
		var audit string
		if err := f.conn.QueryRow(t.Context(), `SELECT jsonb_agg(jsonb_build_object('session_id',session_id,'run_id',run_id,'request_hash',request_hash,'client_reason',client_reason,'received_at',received_at,'source',source) ORDER BY request_hash)::text FROM session_run_abort_controls`).Scan(&audit); err != nil {
			t.Fatal(err)
		}
		out["original_audit"] = audit
		return out
	}
	before := snapshot()
	if err := memohdb.RunMigrate(nil, pgConfigFromDSN(t, teamMigrationDSN(t)), postgresMigrationsFS(t), "up", nil); err != nil {
		t.Fatal(err)
	}
	var version uint
	var dirty, unknown bool
	if err := f.conn.QueryRow(t.Context(), `SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil || version != expectedVersion || dirty {
		t.Fatalf("upgrade status: version=%d dirty=%v err=%v", version, dirty, err)
	}
	if !maps.Equal(before, snapshot()) {
		t.Fatal("upgrade changed accepted work, pause, receipt, history or original audit")
	}
	if err := f.conn.QueryRow(t.Context(), `SELECT completed_at IS NULL AND applied IS NULL AND error_class IS NULL AND server_source IS NULL FROM session_run_abort_controls WHERE run_id=$1`, run).Scan(&unknown); err != nil || !unknown {
		t.Fatal("upgrade invented historical cancellation outcome", err)
	}
}
