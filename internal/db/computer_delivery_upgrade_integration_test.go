//go:build integration

package db_test

import (
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
	t.Log("170 -> 171 -> 172 -> 173: transfer credentials and delivery ledger preserved; guard and delegation RLS present")
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
