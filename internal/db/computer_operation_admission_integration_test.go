//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	memohdb "github.com/felinics/memoh/internal/db"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
)

func operationAdmissionStore(t *testing.T, f admissionFixture, teamID, role string) (*postgresstore.Store, uint32) {
	t.Helper()
	cfg := f.pool.Config().Copy()
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["memoh.team_id"] = teamID
	if role != "" {
		cfg.ConnConfig.RuntimeParams["role"] = role
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var backend uint32
	if err = pool.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&backend); err != nil {
		t.Fatal(err)
	}
	store, err := postgresstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	return store, backend
}

func admissionOperation(t *testing.T, f admissionFixture) dbstore.ComputerOperationRecord {
	t.Helper()
	op, won, err := f.store.ClaimComputerOperation(t.Context(), f.operation(f.mount))
	if err != nil || !won {
		t.Fatal("claim", won, err)
	}
	op.State = "unknown"
	op.Details = []byte(`{"phase":"batch_ready_for_tab","batch":{"progress":{"attempt":"original","generation":"original"}}}`)
	op, err = f.store.SaveComputerOperation(t.Context(), op)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func admissionReservation(op dbstore.ComputerOperationRecord) dbstore.ComputerOperationRecord {
	op.State = "dispatching"
	op.Details = []byte(`{"phase":"batch_creating_tab","batch":{"progress":{"attempt":"original","generation":"original"}}}`)
	return op
}

func assertAdmissionOperationUnchanged(t *testing.T, f admissionFixture, before dbstore.ComputerOperationRecord) {
	t.Helper()
	after, err := f.store.ReadComputerOperation(t.Context(), before)
	if err != nil || after.Revision != before.Revision || after.State != before.State || after.CancelRequested != before.CancelRequested || string(after.Details) != string(before.Details) {
		t.Fatal("rejected reservation changed operation", err)
	}
}

func TestComputerOperationAdmissionCASPreservesScopeAndDelivery(t *testing.T) {
	f := newNativeHerdrFixture(t)
	store, _ := operationAdmissionStore(t, f, team.DefaultTeamID, "")
	op := admissionOperation(t, f)
	ledger := []byte(`{"version":5,"storage":"rows"}`)
	var err error
	op, err = f.store.SaveComputerDelivery(t.Context(), op, ledger)
	if err != nil {
		t.Fatal(err)
	}
	request := uuid.NewString()
	gate := f.change(t, request, 0, true)
	if _, err = store.SaveComputerOperationWithAdmission(t.Context(), admissionReservation(op)); !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
		t.Fatal("paused dispatch admitted", err)
	}
	assertAdmissionOperationUnchanged(t, f, op)
	// Pausing new input must not block ordinary receipt/progress persistence.
	op, err = f.store.SaveComputerOperation(t.Context(), op)
	if err != nil {
		t.Fatal("ordinary progress was changed", err)
	}
	f.change(t, request, gate.Revision, false)
	for _, field := range []string{"bot", "session", "user", "target", "run", "request", "registration", "revision", "cancel"} {
		bad := admissionReservation(op)
		switch field {
		case "bot":
			bad.BotID = uuid.NewString()
		case "session":
			bad.SessionID = uuid.NewString()
		case "user":
			bad.UserID = uuid.NewString()
		case "target":
			bad.TargetID = uuid.NewString()
		case "run":
			bad.RunID = "other-run"
		case "request":
			bad.Fingerprint = "other-task"
		case "registration":
			bad.TargetFingerprint = "other-target"
		case "revision":
			bad.Revision--
		case "cancel":
			bad.CancelRequested = true
		}
		if _, err = store.SaveComputerOperationWithAdmission(t.Context(), bad); !errors.Is(err, memohdb.ErrNotFound) {
			t.Fatalf("%s accepted: %v", field, err)
		}
		assertAdmissionOperationUnchanged(t, f, op)
	}
	if _, err = f.store.SaveComputerOperationWithAdmission(t.Context(), admissionReservation(op)); err == nil {
		t.Fatal("pool-less store silently skipped the transaction")
	}
	invalid := admissionReservation(op)
	invalid.State = "invalid-dispatch-state"
	if _, err = store.SaveComputerOperationWithAdmission(t.Context(), invalid); err == nil {
		t.Fatal("invalid CAS write committed")
	}
	assertAdmissionOperationUnchanged(t, f, op)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = store.SaveComputerOperationWithAdmission(canceled, admissionReservation(op)); err == nil {
		t.Fatal("canceled context committed a reservation")
	}
	assertAdmissionOperationUnchanged(t, f, op)
	before := op
	op, err = store.SaveComputerOperationWithAdmission(t.Context(), admissionReservation(op))
	if err != nil || op.Revision != before.Revision+1 || op.ID != before.ID || op.RunID != before.RunID || op.TargetID != before.TargetID || op.Fingerprint != before.Fingerprint || op.TargetFingerprint != before.TargetFingerprint {
		t.Fatal("reservation lost identity or CAS", err)
	}
	var details map[string]json.RawMessage
	if err = json.Unmarshal(op.Details, &details); err != nil {
		t.Fatal(err)
	}
	assertJSONSemanticallyEqual(t, details["delivery_ledger"], ledger)
	if _, err = store.SaveComputerOperationWithAdmission(t.Context(), admissionReservation(before)); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("pre-restart revision repeated a dispatch", err)
	}
	entry := dbstore.ComputerDeliveryEntry{DeliveryID: "original-delivery", Version: 4, Data: []byte(`{"receipt":"unchanged"}`)}
	op, err = f.store.SaveComputerDeliveryEntry(t.Context(), op, entry)
	if err != nil {
		t.Fatal(err)
	}
	op.DeliveryEntries = map[string]dbstore.ComputerDeliveryEntry{entry.DeliveryID: entry}
	op, err = store.SaveComputerOperationWithAdmission(t.Context(), admissionReservation(op))
	if err != nil || op.DeliveryEntries[entry.DeliveryID].DeliveryID != entry.DeliveryID {
		t.Fatal("delivery projection was lost", err)
	}
	storedEntry, err := f.store.ReadComputerDeliveryEntry(t.Context(), op, entry.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONSemanticallyEqual(t, storedEntry.Data, entry.Data)
	op.CancelRequested = true
	op, err = f.store.SaveComputerOperation(t.Context(), op)
	if err != nil {
		t.Fatal(err)
	}
	bad := admissionReservation(op)
	bad.CancelRequested = false
	if _, err = store.SaveComputerOperationWithAdmission(t.Context(), bad); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("persisted cancellation was cleared", err)
	}
	assertAdmissionOperationUnchanged(t, f, op)
}

func TestComputerOperationAdmissionConcurrentReservationsAndRLS(t *testing.T) {
	f := newNativeHerdrFixture(t)
	op := admissionOperation(t, f)
	roleConn := rlsConn(t, f.pool, teamMigrationDSN(t))
	var role string
	if err := roleConn.QueryRow(t.Context(), "SELECT current_user").Scan(&role); err != nil {
		t.Fatal(err)
	}
	foreign, _ := operationAdmissionStore(t, f, uuid.NewString(), role)
	if _, err := foreign.SaveComputerOperationWithAdmission(t.Context(), admissionReservation(op)); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("cross-team reservation escaped RLS", err)
	}
	assertAdmissionOperationUnchanged(t, f, op)
	first, _ := operationAdmissionStore(t, f, team.DefaultTeamID, role)
	second, _ := operationAdmissionStore(t, f, team.DefaultTeamID, role)
	ready := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, store := range []*postgresstore.Store{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			_, err := store.SaveComputerOperationWithAdmission(t.Context(), admissionReservation(op))
			results <- err
		}()
	}
	close(ready)
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, memohdb.ErrNotFound) {
			t.Fatal(err)
		}
	}
	current, err := f.store.ReadComputerOperation(t.Context(), op)
	if err != nil || winners != 1 || current.Revision != op.Revision+1 {
		t.Fatal("concurrent CAS did not reserve exactly once", winners, err)
	}
}

func awaitAdmissionLock(t *testing.T, ctx context.Context, f admissionFixture, backend uint32, result <-chan error) {
	t.Helper()
	for {
		var waiting bool
		if _, err := f.conn.Exec(ctx, "SELECT pg_stat_clear_snapshot()"); err != nil {
			t.Fatal(err)
		}
		if err := f.conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='advisory')", backend).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case err := <-result:
			t.Fatal("expected a real advisory lock wait", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestComputerOperationAdmissionSerializesPauseAndCancellation(t *testing.T) {
	for _, order := range []string{"pause_first", "reservation_first", "cancel_during_admission"} {
		t.Run(order, func(t *testing.T) {
			f := newNativeHerdrFixture(t)
			store, backend := operationAdmissionStore(t, f, team.DefaultTeamID, "")
			op := admissionOperation(t, f)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			// A test-only trigger holds the real store between its admission
			// assertion and CAS completion, without adding production hooks.
			if order == "reservation_first" {
				f.exec(t, `CREATE FUNCTION test_hold_operation_reservation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(432198765); RETURN NEW; END $$`)
				f.exec(t, `CREATE TRIGGER test_hold_operation_reservation BEFORE UPDATE ON computer_operations FOR EACH ROW WHEN (NEW.details->>'phase'='batch_creating_tab') EXECUTE FUNCTION test_hold_operation_reservation()`)
			}
			tx, err := f.conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			request := uuid.NewString()
			switch order {
			case "pause_first":
				f.change(t, request, 0, true) // The fixture's pinned connection is in tx.
			case "reservation_first":
				if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(432198765)"); err != nil {
					t.Fatal(err)
				}
			default:
				if _, err = tx.Exec(ctx, "SELECT public.memoh_computer_admission_lock($1)", f.owner); err != nil {
					t.Fatal(err)
				}
			}
			reserved := make(chan error, 1)
			reservation := admissionReservation(op)
			go func() {
				_, err := store.SaveComputerOperationWithAdmission(ctx, reservation)
				reserved <- err
			}()
			awaitAdmissionLock(t, ctx, f, backend, reserved)
			var paused chan error
			if order == "reservation_first" {
				pauseStore, pauseBackend := operationAdmissionStore(t, f, team.DefaultTeamID, "")
				paused = make(chan error, 1)
				go func() {
					_, err := pauseStore.ChangeComputerAdmission(ctx, f.owner, f.runtime, f.config, request, 0, true)
					paused <- err
				}()
				awaitAdmissionLock(t, ctx, f, pauseBackend, paused)
			}
			if order == "cancel_during_admission" {
				op.CancelRequested = true
				op, err = f.store.SaveComputerOperation(ctx, op)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-reserved
			switch order {
			case "pause_first":
				if !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
					t.Fatal("committed pause did not block reservation", err)
				}
				assertAdmissionOperationUnchanged(t, f, op)
			case "cancel_during_admission":
				if !errors.Is(err, memohdb.ErrNotFound) {
					t.Fatal("concurrent cancellation did not win CAS", err)
				}
				assertAdmissionOperationUnchanged(t, f, op)
			default:
				if err != nil {
					t.Fatal("reservation preceding pause failed", err)
				}
				if err = <-paused; err != nil {
					t.Fatal(err)
				}
				current, err := f.store.ReadComputerOperation(ctx, op)
				if err != nil || current.Revision != op.Revision+1 || current.State != "dispatching" {
					t.Fatal("committed reservation lost", err)
				}
				if _, err = store.SaveComputerOperationWithAdmission(ctx, current); !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
					t.Fatal("later reservation bypassed pause", err)
				}
			}
		})
	}
}
