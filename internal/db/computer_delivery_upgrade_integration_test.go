//go:build integration

package db_test

import (
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/team"
)

// Every database here is created and dropped by the existing isolated helper.
// The 170 anchor is explicit; later migrations cannot silently shift this test.
func TestComputerDeliveryPostgresUpgradeFrom170(t *testing.T) {
	ownerPool := freshMigratedDB(t)
	dsn := teamMigrationDSN(t)
	migrateTo(t, dsn, 170)
	config := ownerPool.Config().Copy()
	config.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	assertTransferSchema(t, pool, 0, 0)
	owner, bot, session, runtime, operation := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users(id) VALUES($1)", owner)
	exec("INSERT INTO team_members(user_id) VALUES($1)", owner)
	exec("INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'upgrade-fixture')", bot, owner)
	exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id) VALUES($1,$2,$3)", session, bot, owner)
	exec(`INSERT INTO user_runtimes(id,user_id,name,api_token,provider,provider_config,activated_at,pending_expires_at) VALUES($1,$2,'upgrade-fixture','synthetic-control-token','agenvo','{"device_id":"fixture-device","instance_id":"fixture-instance"}',now(),NULL)`, runtime, owner)
	original := `{"phase":"fixture-original","batch":{"progress":{"attempt":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}}`
	exec(`INSERT INTO computer_operations(id,bot_id,session_id,user_id,target_id,run_id,fingerprint,target_fingerprint,state,details,cancel_requested,revision) VALUES($1,$2,$3,$4,$5,'upgrade-run','fingerprint','target-fingerprint','completed',$6,true,7)`, operation, bot, session, owner, runtime, original)
	checkOriginal := func() {
		t.Helper()
		var ok bool
		if err := pool.QueryRow(t.Context(), `SELECT details=$2::jsonb AND revision=7 AND cancel_requested AND state='completed' FROM computer_operations WHERE id=$1`, operation, original).Scan(&ok); err != nil || !ok {
			t.Fatal("existing operation changed", err)
		}
		if err := pool.QueryRow(t.Context(), `SELECT api_token='synthetic-control-token' AND provider='agenvo' AND provider_config='{"device_id":"fixture-device","instance_id":"fixture-instance"}'::jsonb AND activated_at IS NOT NULL AND pending_expires_at IS NULL FROM user_runtimes WHERE id=$1`, runtime).Scan(&ok); err != nil || !ok {
			t.Fatal("existing device changed", err)
		}
	}
	migrateTo(t, dsn, 171)
	assertTransferSchema(t, pool, 0, 1)
	checkOriginal()
	migrateTo(t, dsn, 172)
	assertTransferSchema(t, pool, 4, 1)
	checkOriginal()
	var ok bool
	if err := pool.QueryRow(t.Context(), `SELECT transfer_credential_id='' AND transfer_credential_digest='' AND transfer_target_digest='' AND transfer_expires_at IS NULL FROM user_runtimes WHERE id=$1`, runtime).Scan(&ok); err != nil || !ok {
		t.Fatal("upgrade created file credentials", err)
	}
	// Once a ledger exists, a legacy writer may not erase it after the upgrade.
	exec(`UPDATE computer_operations SET details=jsonb_set(details,'{delivery_ledger}','{"version":3,"entries":{}}'::jsonb) WHERE id=$1`, operation)
	if _, err := pool.Exec(t.Context(), "UPDATE computer_operations SET details=$2 WHERE id=$1", operation, original); sqlState(err) != "23514" {
		t.Fatal("legacy writer erased ledger", err)
	}
	// Compose the transfer schema with CrossBot's next migration. Existing
	// credentials and the guarded delivery ledger must survive unchanged.
	exec(`UPDATE user_runtimes SET transfer_credential_id='synthetic-transfer', transfer_credential_digest='synthetic-digest', transfer_target_digest='synthetic-target', transfer_expires_at=now()+interval '1 hour' WHERE id=$1`, runtime)
	const snapshotSQL = `SELECT jsonb_build_object('runtime',(SELECT to_jsonb(r) FROM user_runtimes r WHERE id=$1),'operation',(SELECT to_jsonb(o) FROM computer_operations o WHERE id=$2))::text`
	var before, after string
	if err := pool.QueryRow(t.Context(), snapshotSQL, runtime, operation).Scan(&before); err != nil {
		t.Fatal(err)
	}
	migrateTo(t, dsn, 173)
	assertTransferSchema(t, pool, 4, 1)
	assertDelegationSchema(t, pool)
	if err := pool.QueryRow(t.Context(), snapshotSQL, runtime, operation).Scan(&after); err != nil || before != after {
		t.Fatal("173 changed existing transfer credentials or delivery state", err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE computer_operations SET details=$2 WHERE id=$1", operation, original); sqlState(err) != "23514" {
		t.Fatal("173 removed legacy writer guard", err)
	}
	// Preserve opaque historical receipt fields, including unknown work. Upgrade
	// is part of ordinary migrations, never a user-operated queue migration.
	legacy := `{"version":4,"entries":{"ready_history":{"outcome":"committed","transfer":{"upload_id":"original_receipt","ready":{"content_hash":"original_hash","size_bytes":42}}},"unknown_history":{"outcome":"reserved","transfer":{"creating":true}}}}`
	exec(`UPDATE computer_operations SET details=jsonb_set(details,'{delivery_ledger}',$2::jsonb) WHERE id=$1`, operation, legacy)
	var legacyDetails string
	if err := pool.QueryRow(t.Context(), `SELECT details::text FROM computer_operations WHERE id=$1`, operation).Scan(&legacyDetails); err != nil {
		t.Fatal(err)
	}
	migrateTo(t, dsn, 174)
	checkRows := func() {
		t.Helper()
		var preserved bool
		if err := pool.QueryRow(t.Context(), `SELECT jsonb_object_agg(delivery_id,entry)=$2::jsonb->'entries' AND bool_and(version=4) FROM computer_delivery_entries WHERE operation_id=$1`, operation, legacy).Scan(&preserved); err != nil || !preserved {
			t.Fatal("legacy entries changed", err)
		}
		if err := pool.QueryRow(t.Context(), `SELECT details->'delivery_ledger'='{"version":5,"storage":"rows"}'::jsonb AND details-'delivery_ledger'=$2::jsonb-'delivery_ledger' AND revision=7 AND cancel_requested FROM computer_operations WHERE id=$1`, operation, legacyDetails).Scan(&preserved); err != nil || !preserved {
			t.Fatal("operation identity changed", err)
		}
	}
	checkRows()
	if _, err := pool.Exec(t.Context(), `UPDATE computer_operations SET details=jsonb_set(details,'{delivery_ledger}',$2::jsonb) WHERE id=$1`, operation, legacy); sqlState(err) != "23514" {
		t.Fatal("old JSON writer accepted", err)
	}
	up, err := os.ReadFile("../../db/postgres/migrations/0174_computer_delivery_entries.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(up))
	checkRows()
	migrateTo(t, dsn, 173)
	var restored string
	if err := pool.QueryRow(t.Context(), `SELECT details::text FROM computer_operations WHERE id=$1`, operation).Scan(&restored); err != nil || restored != legacyDetails {
		t.Fatal("lossless rollback changed history", err)
	}
	migrateTo(t, dsn, 174)
	checkRows()
	// A future downgrade must explicitly reject data that old writers cannot
	// represent, retaining all rows and the enabled security policy on failure.
	down, err := os.ReadFile("../../db/postgres/migrations/0174_computer_delivery_entries.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Row layout can retain old v1 download evidence alongside newer uploads;
	// old JSON ledgers cannot describe those versions independently.
	exec(`INSERT INTO computer_delivery_entries(team_id,operation_id,delivery_id,version,entry) SELECT team_id,operation_id,'legacy_download',1,'{"reference":{"direction":"download"},"outcome":"canceled"}'::jsonb FROM computer_delivery_entries WHERE operation_id=$1 AND delivery_id='unknown_history'`, operation)
	if _, err := pool.Exec(t.Context(), string(down)); sqlState(err) != "23514" {
		t.Fatal("mixed proof rollback accepted", err)
	}
	exec(`DELETE FROM computer_delivery_entries WHERE operation_id=$1 AND delivery_id='legacy_download'`, operation)
	exec(`INSERT INTO computer_delivery_entries(team_id,operation_id,delivery_id,version,entry) SELECT team_id,operation_id,'retained_'||g,version,entry FROM computer_delivery_entries CROSS JOIN generate_series(1,129) g WHERE operation_id=$1 AND delivery_id='unknown_history'`, operation)
	if _, err := pool.Exec(t.Context(), string(down)); sqlState(err) != "23514" {
		t.Fatal("lossy rollback accepted", err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM computer_delivery_entries WHERE operation_id=$1`, operation).Scan(&count); err != nil || count != 131 {
		t.Fatal("rollback discarded history", err, count)
	}
	var secured bool
	if err := pool.QueryRow(t.Context(), `SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid='computer_delivery_entries'::regclass`).Scan(&secured); err != nil || !secured {
		t.Fatal("rollback changed RLS", err)
	}
	t.Log("170 -> 174, repeat-up, down/up: old receipt/unknown payloads and operation revision retained; old writer and lossy rollback rejected")
}

func TestComputerDeliveryPostgresCanonicalTransferSchema(t *testing.T) {
	dsn := teamMigrationDSN(t)
	applyCanonicalInitOnly(t, dsn)
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	assertTransferSchema(t, pool, 4, 1)
	assertDelegationSchema(t, pool)
}

func assertDelegationSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var secured bool
	if err := pool.QueryRow(t.Context(), `SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid='public.bot_delegations'::regclass`).Scan(&secured); err != nil || !secured {
		t.Fatal("delegation ledger or forced RLS absent", err)
	}
}

func assertTransferSchema(t *testing.T, pool *pgxpool.Pool, columns, triggers int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='user_runtimes' AND column_name LIKE 'transfer_%'`).Scan(&got); err != nil || got != columns {
		t.Fatal("transfer columns", got, columns, err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_trigger WHERE tgrelid='public.computer_operations'::regclass AND tgname='computer_delivery_guard' AND NOT tgisinternal`).Scan(&got); err != nil || got != triggers {
		t.Fatal("delivery guard", got, triggers, err)
	}
}
