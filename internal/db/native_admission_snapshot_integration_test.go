//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/team"
)

func TestNativeAdmissionSnapshotCutoverAndLostResponse(t *testing.T) {
	f := newNativeHerdrFixture(t)
	profile := agenvo.NativeHerdrProfile{InstanceID: "synthetic-native-instance", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\A`}}
	a, _ := json.Marshal(map[string]any{"herdr": profile, "private": "SYNTHETIC-PRIVATE"})
	profile.Agent.ConfigDir = `C:\Synthetic\B`
	b, _ := json.Marshal(map[string]any{"herdr": profile, "private": "SYNTHETIC-PRIVATE"})
	f.exec(t, `UPDATE user_runtimes SET provider_config=$2 WHERE id=$1`, f.runtime, a)
	f.config = a
	request := uuid.NewString()
	gate := f.change(t, request, 0, true)
	service := agenvo.NewService(f.store, nil, nil)
	check := func(out agenvo.Admission) {
		t.Helper()
		var raw []byte
		if out.Revision == "" || !out.Paused || out.RequestID != request || out.ConnectionID != f.runtime || out.InstanceID != gate.InstanceID {
			t.Fatalf("bad identity: %+v", out)
		}
		var rev int64
		if _, err := fmt.Sscan(out.Revision, &rev); err != nil {
			t.Fatal(err)
		}
		if rev%2 == 1 {
			raw = a
			if out.AgentConfigDir != `C:\Synthetic\A` {
				t.Fatalf("mixed A revision/profile: %+v", out)
			}
		} else {
			raw = b
			if out.AgentConfigDir != `C:\Synthetic\B` {
				t.Fatalf("mixed B revision/profile: %+v", out)
			}
		}
		digest, _ := agenvo.NativeProfileConfigDigest(raw)
		target, err := agenvo.DecodeNativeTarget(f.runtime, raw)
		if err != nil || out.ConfigSHA256 != digest || out.TargetFingerprint != agenvo.TargetFingerprint(target) {
			t.Fatalf("mixed digest/fingerprint: %+v %v", out, err)
		}
	}
	first, err := service.ReadAdmission(t.Context(), f.owner, f.runtime)
	if err != nil {
		t.Fatal(err)
	}
	check(first)
	// Commit the PUT but deliberately discard its acknowledgement.
	if _, err = f.store.CutoverPausedNativeProfile(t.Context(), f.owner, f.runtime, f.mount, a, b, gate.InstanceID, request, gate.Revision); err != nil {
		t.Fatal(err)
	}
	won, err := service.ReadAdmission(t.Context(), f.owner, f.runtime)
	if err != nil {
		t.Fatal(err)
	}
	check(won)
	if won.Revision != "2" || won.AgentConfigDir != profile.Agent.ConfigDir {
		t.Fatalf("lost response winner not identified: %+v", won)
	}
	done := make(chan error, 1)
	go func() {
		conn, e := f.pool.Acquire(context.Background())
		if e != nil {
			done <- e
			return
		}
		defer conn.Release()
		if _, e = conn.Exec(context.Background(), `SELECT set_config('memoh.team_id',$1,false)`, team.DefaultTeamID); e != nil {
			done <- e
			return
		}
		store := postgresstore.NewWithQueries(dbsqlc.New(conn))
		old, next := b, a
		for rev := int64(2); rev < 10; rev++ {
			if _, e = store.CutoverPausedNativeProfile(context.Background(), f.owner, f.runtime, f.mount, old, next, gate.InstanceID, request, rev); e != nil {
				done <- e
				return
			}
			old, next = next, old
		}
		done <- nil
	}()
	for range 40 {
		out, e := service.ReadAdmission(t.Context(), f.owner, f.runtime)
		if e != nil {
			t.Fatal(e)
		}
		check(out)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	final, err := service.ReadAdmission(t.Context(), f.owner, f.runtime)
	if err != nil {
		t.Fatal(err)
	}
	check(final)
	if final.Revision != "10" {
		t.Fatal(final.Revision)
	}
	if _, err = service.ReadAdmission(t.Context(), uuid.NewString(), f.runtime); err == nil {
		t.Fatal("foreign owner read")
	}
	foreign := rlsConn(t, f.pool, teamMigrationDSN(t))
	if _, err = foreign.Exec(t.Context(), `SELECT set_config('memoh.team_id',$1,false)`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	fs := postgresstore.NewWithQueries(dbsqlc.New(foreign))
	if _, err = agenvo.NewService(fs, nil, nil).ReadAdmission(t.Context(), f.owner, f.runtime); err == nil {
		t.Fatal("foreign team read")
	}
	f.exec(t, `UPDATE team_members SET is_active=false WHERE user_id=$1`, f.owner)
	if _, err = service.ReadAdmission(t.Context(), f.owner, f.runtime); err == nil {
		t.Fatal("revoked membership read")
	}
	f.exec(t, `UPDATE team_members SET is_active=true WHERE user_id=$1`, f.owner)
	f.exec(t, `UPDATE users SET is_active=false WHERE id=$1`, f.owner)
	if _, err = service.ReadAdmission(t.Context(), f.owner, f.runtime); err == nil {
		t.Fatal("inactive account read")
	}
	f.exec(t, `UPDATE users SET is_active=true WHERE id=$1`, f.owner)
	f.exec(t, `UPDATE user_runtimes SET revoked_at=now() WHERE id=$1`, f.runtime)
	if _, err = service.ReadAdmission(t.Context(), f.owner, f.runtime); err == nil {
		t.Fatal("revoked runtime read")
	}
}
