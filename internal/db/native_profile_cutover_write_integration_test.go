//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/felinics/memoh/internal/agenvo"
	memohdb "github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/team"
)

const profileCutoverSQL = `SELECT public.memoh_cutover_paused_native_profile($1,$2,$3,$4::jsonb,$5::jsonb,$6,$7,$8)`

func TestPausedNativeProfileCutoverAtomic(t *testing.T) {
	f := newNativeHerdrFixture(t)
	old := []byte(`{"herdr":{"instance_id":"synthetic-native-instance","session":"synthetic-session","agent":{"config_dir":"C:\\Synthetic\\old","model":"fixture"}},"other":{"preserved":true}}`)
	next := []byte(`{"herdr":{"instance_id":"synthetic-native-instance","session":"synthetic-session","agent":{"config_dir":"C:\\Synthetic\\new","model":"fixture"}},"other":{"preserved":true}}`)
	f.exec(t, `UPDATE user_runtimes SET provider_config=$2 WHERE id=$1`, f.runtime, old)
	f.config = old
	request := uuid.NewString()
	gate := f.change(t, request, 0, true)
	args := []any{f.owner, f.runtime, f.mount, old, next, gate.InstanceID, request, gate.Revision}
	foreign := rlsConn(t, f.pool, teamMigrationDSN(t))
	if _, err := foreign.Exec(t.Context(), `SELECT set_config('memoh.team_id',$1,false)`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	var foreignReceipt []byte
	foreignErr := foreign.QueryRow(t.Context(), profileCutoverSQL, args...).Scan(&foreignReceipt)
	var foreignPG *pgconn.PgError
	if !errors.As(foreignErr, &foreignPG) || foreignPG.Code != "MC003" {
		t.Fatalf("foreign team crossed scope: %v", foreignErr)
	}
	// A client-controlled GUC is never a permit for the original PUT path.
	guardTx, err := f.conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = guardTx.Exec(t.Context(), `SELECT set_config('memoh.native_profile_cutover','true',true)`); err != nil {
		t.Fatal(err)
	}
	_, err = guardTx.Exec(t.Context(), `UPDATE user_runtimes SET provider_config=$2 WHERE id=$1`, f.runtime, next)
	var guardErr *pgconn.PgError
	if !errors.As(err, &guardErr) || guardErr.Code != "MC001" {
		t.Fatalf("forged GUC bypassed old guard: %v", err)
	}
	if err = guardTx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	var receipt []byte
	bad := append([]any(nil), args...)
	bad[7] = gate.Revision + 1
	err = f.conn.QueryRow(t.Context(), profileCutoverSQL, bad...).Scan(&receipt)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "MC002" {
		t.Fatalf("wrong revision: %v", err)
	}
	bad = append([]any(nil), args...)
	bad[4] = []byte(`{"herdr":{"instance_id":"synthetic-native-instance","session":"synthetic-session","agent":{"config_dir":"C:\\Synthetic\\new","model":"changed"}},"other":{"preserved":true}}`)
	err = f.conn.QueryRow(t.Context(), profileCutoverSQL, bad...).Scan(&receipt)
	if !errors.As(err, &pgErr) || pgErr.Code != "MC003" {
		t.Fatalf("non-profile field changed: %v", err)
	}
	// A successful operation inside a caller transaction must roll back both
	// the config and the ephemeral intent if the caller does not commit.
	tx, err := f.conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(t.Context(), profileCutoverSQL, args...).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	rollbackGate, rollbackErr := f.store.ReadComputerAdmission(t.Context(), f.owner, f.runtime, f.runtime, gate.InstanceID)
	if rollbackErr != nil || rollbackGate != gate {
		t.Fatalf("rollback changed gate: %+v %v", rollbackGate, rollbackErr)
	}
	var oldStill bool
	if err = f.conn.QueryRow(t.Context(), `SELECT provider_config=$2::jsonb FROM user_runtimes WHERE id=$1`, f.runtime, old).Scan(&oldStill); err != nil || !oldStill {
		t.Fatalf("rollback changed config: %v", err)
	}
	var intents int
	if err = f.conn.QueryRow(t.Context(), `SELECT count(*) FROM native_profile_cutover_intents`).Scan(&intents); err != nil || intents != 0 {
		t.Fatalf("intent remained after rollback: %d %v", intents, err)
	}
	const racers = 2
	start := make(chan struct{})
	results := make(chan error, racers)
	var wg sync.WaitGroup
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, e := f.pool.Acquire(context.Background())
			if e != nil {
				results <- e
				return
			}
			defer conn.Release()
			if _, e = conn.Exec(context.Background(), `SELECT set_config('memoh.team_id',$1,false)`, team.DefaultTeamID); e != nil {
				results <- e
				return
			}
			<-start
			var result []byte
			e = conn.QueryRow(context.Background(), profileCutoverSQL, args...).Scan(&result)
			results <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
			continue
		}
		var p *pgconn.PgError
		if errors.As(e, &p) && p.Code == "MC002" {
			conflict++
			continue
		}
		t.Fatalf("unexpected concurrent error: %v", e)
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	var newSet bool
	if err = f.conn.QueryRow(t.Context(), `SELECT provider_config=$2::jsonb FROM user_runtimes WHERE id=$1`, f.runtime, next).Scan(&newSet); err != nil || !newSet {
		t.Fatalf("new config missing: %v", err)
	}
	if _, err = f.store.ConfigureNativeHerdrRuntime(t.Context(), f.owner, f.runtime, next, old); !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
		t.Fatalf("old PUT bypassed pause after cutover: %v", err)
	}
	after, err := f.store.ReadComputerAdmission(t.Context(), f.owner, f.runtime, f.runtime, gate.InstanceID)
	expected := gate
	expected.Revision++
	if err != nil || after != expected || !after.Paused {
		t.Fatalf("pause changed: %v", err)
	}
	if err = f.conn.QueryRow(t.Context(), `SELECT count(*) FROM native_profile_cutover_intents`).Scan(&intents); err != nil || intents != 0 {
		t.Fatalf("intent remained: %d %v", intents, err)
	}
}

func TestNativeProfileCutoverMigrationRoundTrip(t *testing.T) {
	f := newNativeHerdrFixture(t)
	down, err := fs.ReadFile(postgresMigrationsFS(t), "0178_native_profile_cutover.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, err := fs.ReadFile(postgresMigrationsFS(t), "0178_native_profile_cutover.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{down, up, up} {
		if _, err = f.conn.Exec(t.Context(), string(raw)); err != nil {
			t.Fatalf("migration roundtrip: %v", err)
		}
	}
	gate := f.change(t, uuid.NewString(), 0, true)
	if _, err = f.store.ConfigureNativeHerdrRuntime(t.Context(), f.owner, f.runtime, f.config, []byte(`{}`)); !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
		t.Fatalf("ordinary paused update accepted: %v", err)
	}
	after, err := f.store.ReadComputerAdmission(t.Context(), f.owner, f.runtime, f.runtime, gate.InstanceID)
	if err != nil || after != gate {
		t.Fatalf("gate changed: %v", err)
	}
}

func TestPausedNativeProfileOwnerService(t *testing.T) {
	f := newNativeHerdrFixture(t)
	profile := agenvo.NativeHerdrProfile{InstanceID: "synthetic-native-instance", Session: "default", WorkDir: `C:\Synthetic\work`, BashPath: `C:\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\old`}}
	old, err := json.Marshal(map[string]any{"herdr": profile, "other": "retained"})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE user_runtimes SET provider_config=$2 WHERE id=$1`, f.runtime, old)
	f.config = old
	request := uuid.NewString()
	gate := f.change(t, request, 0, true)
	row, err := f.store.OwnedNativeHerdrRuntime(t.Context(), f.owner, f.runtime)
	if err != nil {
		t.Fatal(err)
	}
	target, err := agenvo.DecodeNativeTarget(f.runtime, row.ProviderConfig)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agenvo.NativeProfileConfigDigest(row.ProviderConfig)
	if err != nil {
		t.Fatal(err)
	}
	in := agenvo.NativeProfileCutoverRequest{RuntimeID: f.runtime, TargetID: f.mount, InstanceID: target.InstanceID, ExpectedConfigSHA256: digest, ExpectedTargetFingerprint: agenvo.TargetFingerprint(target), PauseRequestID: request, ExpectedPauseRevision: "1", AgentConfigDir: `C:\Synthetic\new`}
	service := agenvo.NewService(f.store, nil, nil)
	if _, err = service.CutoverPausedNativeProfile(t.Context(), uuid.NewString(), f.runtime, in); !errors.Is(err, agenvo.ErrForbidden) {
		t.Fatalf("foreign owner: %v", err)
	}
	bad := in
	bad.ExpectedPauseRevision = "2"
	if _, err = service.CutoverPausedNativeProfile(t.Context(), f.owner, f.runtime, bad); !errors.Is(err, agenvo.ErrNativeProfileConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	bad = in
	bad.TargetID = uuid.NewString()
	if _, err = service.CutoverPausedNativeProfile(t.Context(), f.owner, f.runtime, bad); !errors.Is(err, agenvo.ErrForbidden) {
		t.Fatalf("foreign target: %v", err)
	}
	receipt, err := service.CutoverPausedNativeProfile(t.Context(), f.owner, f.runtime, in)
	if err != nil || receipt.Revision != "2" || !receipt.Paused || receipt.RequestID != request {
		t.Fatalf("cutover response: %+v %v", receipt, err)
	}
	var dir string
	if err = f.conn.QueryRow(t.Context(), `SELECT provider_config #>> '{herdr,agent,config_dir}' FROM user_runtimes WHERE id=$1`, f.runtime).Scan(&dir); err != nil || dir != in.AgentConfigDir {
		t.Fatalf("changed directory mismatch: %v", err)
	}
	after, err := f.store.ReadComputerAdmission(t.Context(), f.owner, f.runtime, f.runtime, gate.InstanceID)
	expected := gate
	expected.Revision++
	if err != nil || after != expected {
		t.Fatalf("pause changed: %v", err)
	}
}

func TestPausedNativeProfileCutoverRacesResumeAndRevocation(t *testing.T) {
	for _, action := range []string{"resume", "revoke"} {
		t.Run(action, func(t *testing.T) {
			f := newNativeHerdrFixture(t)
			old := []byte(`{"herdr":{"instance_id":"synthetic-native-instance","session":"synthetic-session","agent":{"config_dir":"C:\\Synthetic\\old"}}}`)
			next := []byte(`{"herdr":{"instance_id":"synthetic-native-instance","session":"synthetic-session","agent":{"config_dir":"C:\\Synthetic\\new"}}}`)
			f.exec(t, `UPDATE user_runtimes SET provider_config=$2 WHERE id=$1`, f.runtime, old)
			f.config = old
			request := uuid.NewString()
			gate := f.change(t, request, 0, true)
			args := []any{f.owner, f.runtime, f.mount, old, next, gate.InstanceID, request, gate.Revision}
			start := make(chan struct{})
			out := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func(i int) {
					conn, err := f.pool.Acquire(context.Background())
					if err != nil {
						out <- err
						return
					}
					defer conn.Release()
					if _, err = conn.Exec(context.Background(), `SELECT set_config('memoh.team_id',$1,false)`, team.DefaultTeamID); err != nil {
						out <- err
						return
					}
					<-start
					if i == 0 {
						var r []byte
						err = conn.QueryRow(context.Background(), profileCutoverSQL, args...).Scan(&r)
					} else if action == "resume" {
						var r []byte
						err = conn.QueryRow(context.Background(), `SELECT public.memoh_change_computer_admission($1,$2,$3::jsonb,$4,$5,false)`, f.owner, f.runtime, old, request, gate.Revision).Scan(&r)
					} else {
						_, err = conn.Exec(context.Background(), `UPDATE user_runtimes SET revoked_at=now() WHERE id=$1`, f.runtime)
					}
					out <- err
				}(i)
			}
			close(start)
			first, second := <-out, <-out
			var intents int
			if err := f.conn.QueryRow(t.Context(), `SELECT count(*) FROM native_profile_cutover_intents`).Scan(&intents); err != nil || intents != 0 {
				t.Fatalf("intent remained: %d %v", intents, err)
			}
			if action == "resume" {
				if (first == nil) == (second == nil) {
					t.Fatalf("exactly one transition should win: %v %v", first, second)
				}
			} else {
				var revoked bool
				if err := f.conn.QueryRow(t.Context(), `SELECT revoked_at IS NOT NULL FROM user_runtimes WHERE id=$1`, f.runtime).Scan(&revoked); err != nil || !revoked {
					t.Fatalf("revocation lost: %v", err)
				}
				if first != nil && second != nil {
					t.Fatalf("both race operations failed: %v %v", first, second)
				}
			}
		})
	}
}

func TestPausedNativeProfileCutoverRejectsABA(t *testing.T) {
	f := newNativeHerdrFixture(t)
	old := []byte(`{"herdr":{"instance_id":"synthetic-native-instance","agent":{"config_dir":"C:\\Synthetic\\A"}}}`)
	next := []byte(`{"herdr":{"instance_id":"synthetic-native-instance","agent":{"config_dir":"C:\\Synthetic\\B"}}}`)
	f.exec(t, `UPDATE user_runtimes SET provider_config=$2 WHERE id=$1`, f.runtime, old)
	f.config = old
	request := uuid.NewString()
	gate := f.change(t, request, 0, true)
	original := []any{f.owner, f.runtime, f.mount, old, next, gate.InstanceID, request, gate.Revision}
	var receipt []byte
	if err := f.conn.QueryRow(t.Context(), profileCutoverSQL, original...).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	current, err := f.store.ReadComputerAdmission(t.Context(), f.owner, f.runtime, f.runtime, gate.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	back := []any{f.owner, f.runtime, f.mount, next, old, gate.InstanceID, request, current.Revision}
	if err = f.conn.QueryRow(t.Context(), profileCutoverSQL, back...).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	err = f.conn.QueryRow(t.Context(), profileCutoverSQL, original...).Scan(&receipt)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "MC002" {
		t.Fatalf("stale A→B replay accepted after A→B→A: %v", err)
	}
	current, err = f.store.ReadComputerAdmission(t.Context(), f.owner, f.runtime, f.runtime, gate.InstanceID)
	if err != nil || !current.Paused || current.Revision != gate.Revision+2 || current.RequestID != request {
		t.Fatalf("gate after ABA: %+v %v", current, err)
	}
	// Old resume revision must also fail, even though the configuration is A again.
	if _, err = f.store.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, old, request, gate.Revision, false); !errors.Is(err, memohdb.ErrComputerAdmissionConflict) {
		t.Fatalf("stale resume: %v", err)
	}
	resumed, err := f.store.ChangeComputerAdmission(t.Context(), f.owner, f.runtime, old, request, current.Revision, false)
	if err != nil || resumed.Paused || resumed.Revision != current.Revision+1 {
		t.Fatalf("fresh resume: %+v %v", resumed, err)
	}
	var intents int
	if err = f.conn.QueryRow(t.Context(), `SELECT count(*) FROM native_profile_cutover_intents`).Scan(&intents); err != nil || intents != 0 {
		t.Fatalf("intents=%d %v", intents, err)
	}
}
