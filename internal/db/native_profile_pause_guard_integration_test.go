//go:build integration

package db_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	memohdb "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/team"
)

// A later profile-cutover operation must not weaken the original PUT guard.
// Each racer has its own real DB connection and no test reaches a device.
func TestNativeProfilePauseRejectsConcurrentOrdinaryUpdates(t *testing.T) {
	f := newNativeHerdrFixture(t)
	pauseID := uuid.NewString()
	gate := f.change(t, pauseID, 0, true)
	const racers = 2
	start := make(chan struct{})
	errorsCh := make(chan error, racers)
	var wg sync.WaitGroup
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := f.pool.Acquire(context.Background())
			if err != nil {
				errorsCh <- err
				return
			}
			defer conn.Release()
			if _, err = conn.Exec(context.Background(), `SELECT set_config('memoh.team_id',$1,false)`, team.DefaultTeamID); err != nil {
				errorsCh <- err
				return
			}
			<-start
			store := postgresstore.NewWithQueries(dbsqlc.New(conn))
			_, err = store.ConfigureNativeHerdrRuntime(context.Background(), f.owner, f.runtime, f.config, []byte(`{"herdr":{"instance_id":"synthetic-native-instance","session":"changed"}}`))
			errorsCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
			t.Fatalf("ordinary update bypassed pause: %v", err)
		}
	}
	var sameConfig bool
	err := f.conn.QueryRow(t.Context(), `SELECT provider_config=$2::jsonb FROM user_runtimes WHERE id=$1`, f.runtime, f.config).Scan(&sameConfig)
	if err != nil || !sameConfig {
		t.Fatalf("paused configuration changed: %v", err)
	}
	after, err := f.store.ReadComputerAdmission(t.Context(), f.owner, f.runtime, f.runtime, gate.InstanceID)
	if err != nil || after != gate || !after.Paused {
		t.Fatalf("pause identity or revision changed: %v", err)
	}
}
