//go:build integration

package db_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/google/uuid"

	memohdb "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
)

func newNativeHerdrFixture(t *testing.T) admissionFixture {
	t.Helper()
	f := newAdmissionFixture(t)
	f.runtime, f.mount = uuid.NewString(), uuid.NewString()
	f.connection = f.runtime // Actual native runtime, never an MCP connection.
	f.config = []byte(`{"herdr":{"instance_id":"synthetic-native-instance","session":"synthetic-session"}}`)
	f.exec(t, "INSERT INTO user_runtimes(id,user_id,name,api_token,provider,provider_config,activated_at,pending_expires_at) VALUES($1,$2,'native-herdr-test',$3,'memoh',$4,now(),NULL)", f.runtime, f.owner, "synthetic-"+uuid.NewString(), []byte(f.config))
	f.exec(t, "INSERT INTO bot_remote_runtime_bindings(id,bot_id,runtime_id) VALUES($1,$2,$3)", f.mount, f.bot, f.runtime)
	return f
}

func TestNativeHerdrAdmissionPersistsAndFencesNewWork(t *testing.T) {
	f := newNativeHerdrFixture(t)
	input := f.operation(f.mount)
	op, won, err := f.store.ClaimComputerOperation(t.Context(), input)
	if err != nil || !won {
		t.Fatal(won, err)
	}
	request := uuid.NewString()
	gate := f.change(t, request, 0, true)
	if gate.ConnectionID != f.runtime || gate.DeviceID != f.runtime || gate.InstanceID != "synthetic-native-instance" {
		t.Fatal(gate)
	}
	restarted := postgresstore.NewWithQueries(dbsqlc.New(f.conn))
	if got, err := restarted.ReadComputerAdmission(t.Context(), f.owner, f.runtime, f.runtime, gate.InstanceID); err != nil || got != gate {
		t.Fatal("pause lost on store restart", got, err)
	}
	aliasBot, aliasSession, alias := uuid.NewString(), uuid.NewString(), uuid.NewString()
	f.exec(t, "INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'native-alias')", aliasBot, f.owner)
	f.exec(t, "INSERT INTO bot_sessions(id,bot_id) VALUES($1,$2)", aliasSession, aliasBot)
	f.exec(t, "INSERT INTO bot_remote_runtime_bindings(id,bot_id,runtime_id) VALUES($1,$2,$3)", alias, aliasBot, f.runtime)
	aliasInput := f.operation(alias)
	aliasInput.BotID = aliasBot
	aliasInput.SessionID = aliasSession
	for _, record := range []dbstore.ComputerOperationRecord{f.operation(f.mount), aliasInput} {
		if _, won, err := f.store.ClaimComputerOperation(t.Context(), record); won || !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
			t.Fatal("new native work bypassed pause", won, err)
		}
	}
	input.ID = uuid.NewString()
	if got, won, err := restarted.ClaimComputerOperation(t.Context(), input); err != nil || won || got.ID != op.ID {
		t.Fatal("retry did not return existing operation", got, won, err)
	}
	op.State = "unknown"
	op, err = restarted.SaveComputerOperation(t.Context(), op)
	if err != nil {
		t.Fatal("paused accepted work cannot be recovered", err)
	}
	op.Details = []byte(`{"followups":[{"request_id":"new-input"}]}`)
	if _, err = restarted.SaveComputerOperation(t.Context(), op); !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
		t.Fatal("followup bypassed pause", err)
	}
	if _, err = f.store.ConfigureNativeHerdrRuntime(t.Context(), f.owner, f.runtime, f.config, []byte(`{}`)); !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
		t.Fatal("paused registration changed", err)
	}
	if _, err = f.store.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, f.config, uuid.NewString(), gate.Revision, false); !errors.Is(err, memohdb.ErrComputerAdmissionConflict) {
		t.Fatal("unrelated request reopened gate", err)
	}
	f.change(t, request, gate.Revision, false)
}

func TestNativeHerdrConfigAndTransferOwnership(t *testing.T) {
	f := newNativeHerdrFixture(t)
	if _, err := f.store.OwnedNativeHerdrRuntime(t.Context(), uuid.NewString(), f.runtime); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("foreign owner read profile", err)
	}
	next := dbstore.JSON(`{"herdr":{"instance_id":"synthetic-native-instance","session":"updated"}}`)
	if _, err := f.store.ConfigureNativeHerdrRuntime(t.Context(), f.owner, f.runtime, []byte(`{}`), next); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("stale config changed profile", err)
	}
	if _, err := f.store.ConfigureNativeHerdrRuntime(t.Context(), f.owner, f.runtime, f.config, next); err != nil {
		t.Fatal(err)
	}
	f.config = next
	old, err := f.store.ReadOwnedRuntimeTransfer(t.Context(), f.runtime, f.owner)
	if err != nil || old.Provider != "memoh" {
		t.Fatal(old.Provider, err)
	}
	fresh := old
	fresh.CredentialID = uuid.NewString()
	fresh.Digest = "synthetic-digest"
	fresh.TargetDigest = "synthetic-target-digest"
	if won, err := f.store.ChangeRuntimeTransfer(t.Context(), old, fresh); err != nil || !won {
		t.Fatal("native credential CAS", won, err)
	}
	if won, err := f.store.ChangeRuntimeTransfer(t.Context(), old, fresh); err != nil || won {
		t.Fatal("stale credential CAS", won, err)
	}
	if got, err := f.store.ReadRuntimeTransfer(t.Context(), fresh.CredentialID); err != nil || got.RuntimeID != f.runtime || got.Provider != "memoh" {
		t.Fatal("native credential incorrectly requires MCP", got, err)
	}
	// Native credential validity stays tied to the account, team and actual runtime.
	for _, toggle := range [][2]string{
		{"UPDATE users SET is_active=false WHERE id=$1", "UPDATE users SET is_active=true WHERE id=$1"},
		{"UPDATE team_members SET is_active=false WHERE user_id=$1", "UPDATE team_members SET is_active=true WHERE user_id=$1"},
	} {
		f.exec(t, toggle[0], f.owner)
		if _, err := f.store.ReadRuntimeTransfer(t.Context(), fresh.CredentialID); !errors.Is(err, memohdb.ErrNotFound) {
			t.Fatal("inactive owner accepted", err)
		}
		f.exec(t, toggle[1], f.owner)
	}
	for _, toggle := range [][2]string{
		{"UPDATE user_runtimes SET revoked_at=now() WHERE id=$1", "UPDATE user_runtimes SET revoked_at=NULL WHERE id=$1"},
		{"UPDATE user_runtimes SET transfer_expires_at=now()-interval '1 second' WHERE id=$1", "UPDATE user_runtimes SET transfer_expires_at=NULL WHERE id=$1"},
		{"UPDATE user_runtimes SET activated_at=NULL,pending_expires_at=now()+interval '15 minutes' WHERE id=$1", "UPDATE user_runtimes SET activated_at=now(),pending_expires_at=NULL WHERE id=$1"},
	} {
		f.exec(t, toggle[0], f.runtime)
		if _, err := f.store.ReadRuntimeTransfer(t.Context(), fresh.CredentialID); !errors.Is(err, memohdb.ErrNotFound) {
			t.Fatal("invalid native runtime accepted", err)
		}
		f.exec(t, toggle[1], f.runtime)
	}
	replacement := fresh
	replacement.CredentialID = uuid.NewString()
	replacement.Digest = "synthetic-rotated-digest"
	if won, err := f.store.ChangeRuntimeTransfer(t.Context(), fresh, replacement); err != nil || !won {
		t.Fatal(won, err)
	}
	if _, err := f.store.ReadRuntimeTransfer(t.Context(), fresh.CredentialID); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("old credential survived rotation", err)
	}
	revoked := replacement
	revoked.CredentialID = ""
	revoked.Digest = ""
	revoked.TargetDigest = ""
	if won, err := f.store.ChangeRuntimeTransfer(t.Context(), replacement, revoked); err != nil || !won {
		t.Fatal(won, err)
	}
	if _, err := f.store.ReadRuntimeTransfer(t.Context(), replacement.CredentialID); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("revoked credential accepted", err)
	}
}

func TestNativeHerdrAdmissionRLSAndRollback(t *testing.T) {
	f := newNativeHerdrFixture(t)
	request := uuid.NewString()
	f.change(t, request, 0, true)
	down, err := fs.ReadFile(postgresMigrationsFS(t), "0176_native_herdr_admission.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), string(down)); err == nil {
		t.Fatal("live native pause removed by rollback")
	}
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	c := rlsConn(t, f.pool, teamMigrationDSN(t))
	s := postgresstore.NewWithQueries(dbsqlc.New(c))
	if _, err = c.Exec(t.Context(), "SELECT set_config('memoh.team_id',$1,false)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OwnedNativeHerdrRuntime(t.Context(), f.owner, f.runtime); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("foreign team read native profile", err)
	}
	if _, err = s.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, f.config, request, 1, false); !errors.Is(err, memohdb.ErrComputerAdmissionScope) {
		t.Fatal("foreign team resumed native gate", err)
	}
	if _, err = c.Exec(t.Context(), "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); err != nil {
		t.Fatal(err)
	}
	f.change(t, request, 1, false)
	up, err := fs.ReadFile(postgresMigrationsFS(t), "0176_native_herdr_admission.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{down, up, up} {
		if _, err = f.conn.Exec(t.Context(), string(raw)); err != nil {
			t.Fatal("migration roundtrip", err)
		}
	}
	f.change(t, uuid.NewString(), 2, true)
}

func TestNativeHerdrPauseSerializesWithClaims(t *testing.T) {
	exerciseAdmissionPauseSerializesWithClaims(t, newNativeHerdrFixture)
}

func TestNativeHerdrCanonicalAdmissionDefinition(t *testing.T) {
	dsn := teamMigrationDSN(t)
	pool := resetToEmpty(t)
	applyCanonicalInitOnly(t, dsn)
	runtime := uuid.NewString()
	var connection, device, instance string
	if err := pool.QueryRow(t.Context(), `SELECT identity->>'connection_id',identity->>'device_id',identity->>'instance_id'
        FROM (SELECT public.memoh_computer_admission_identity('memoh',$1,'{"herdr":{"instance_id":"canonical-native"}}') AS identity) AS i`, runtime).Scan(&connection, &device, &instance); err != nil || connection != runtime || device != runtime || instance != "canonical-native" {
		t.Fatal("canonical snapshot lacks native identity", connection, device, instance, err)
	}
}
