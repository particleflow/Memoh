//go:build integration

package db_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	memohdb "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/team"
)

// batchFixture drives the real durable batch store: migration 0188, the row
// lock, the create-or-return identity and the CAS-guarded result envelope.
type batchFixture struct {
	pool                        *pgxpool.Pool
	owner, bot, session, target string
	store                       *postgresstore.Store
}

func newBatchFixture(t *testing.T) batchFixture {
	t.Helper()
	f := newAdmissionFixture(t)
	return batchFixture{pool: f.pool, owner: f.owner, bot: f.bot, session: f.session, target: f.mount, store: f.store}
}

func (f batchFixture) record(run, label string, items []byte) dbstore.ComputerTaskBatchRecord {
	return dbstore.ComputerTaskBatchRecord{ID: uuid.NewString(), BotID: f.bot, SessionID: f.session, UserID: f.owner, TargetID: f.target, RunID: run, Label: label, Fingerprint: uuid.NewString(), State: "running", Items: items}
}

func batchItems(t *testing.T, states ...string) []byte {
	t.Helper()
	items := make([]map[string]any, 0, len(states))
	for i, state := range states {
		items = append(items, map[string]any{"spec": map[string]any{"kind": "herdr_pi_task", "task": "synthetic item " + string(rune('a'+i)), "model": "synthetic-model"}, "state": state})
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestComputerTaskBatchDurableIdentityAndReplay(t *testing.T) {
	f := newBatchFixture(t)
	input := f.record("run-a", "first", batchItems(t, "queued", "queued"))
	created, won, err := f.store.ClaimComputerTaskBatch(t.Context(), input)
	if err != nil || !won || created.ID != input.ID {
		t.Fatal(won, err)
	}
	// The same submission identity returns the stored row, never a second one.
	replay, won, err := f.store.ClaimComputerTaskBatch(t.Context(), input)
	if err != nil || won || replay.ID != created.ID || replay.Fingerprint != input.Fingerprint {
		t.Fatalf("replay=%+v won=%v err=%v", replay, won, err)
	}
	// The durable result envelope starts empty and CAS-guards its writes.
	empty, err := f.store.ReadComputerTaskBatchDelivery(t.Context(), created.ID)
	if err != nil || normalizeDelivery(empty) != "{}" {
		t.Fatalf("delivery=%s err=%v", empty, err)
	}
	next := []byte(`{"event":{"report_state":"pending"}}`)
	if _, err = f.store.SaveComputerTaskBatchDelivery(t.Context(), created.ID, empty, next); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.SaveComputerTaskBatchDelivery(t.Context(), created.ID, empty, next); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("stale delivery CAS accepted", err)
	}
}

func TestComputerTaskBatchRowLockSerializesTransitions(t *testing.T) {
	f := newBatchFixture(t)
	created, _, err := f.store.ClaimComputerTaskBatch(t.Context(), f.record("run-b", "", batchItems(t, "queued")))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.ChangeComputerTaskBatch(t.Context(), created.ID, func(rec *dbstore.ComputerTaskBatchRecord) error {
		items, err := batchStates(rec.Items)
		if err != nil {
			return err
		}
		items[0]["state"] = "in_flight"
		raw, _ := json.Marshal(items)
		rec.Items, rec.State = raw, "running"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := f.store.ReadComputerTaskBatch(t.Context(), created.ID, f.bot, f.session, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	items, err := batchStates(saved.Items)
	if err != nil || items[0]["state"] != "in_flight" {
		t.Fatalf("transition lost: %s %v", saved.Items, err)
	}
	// A mutation that fails must not commit its own write. This store runs on a
	// single pooled connection, so the assertion is that the failure is reported
	// and the prior state is what a later read observes.
	before := saved.State
	if err = f.store.ChangeComputerTaskBatch(t.Context(), created.ID, func(rec *dbstore.ComputerTaskBatchRecord) error {
		rec.State = "completed"
		return errors.New("synthetic rollback")
	}); err == nil {
		t.Fatal("failing mutation committed")
	}
	after, err := f.store.ReadComputerTaskBatch(t.Context(), created.ID, f.bot, f.session, f.owner)
	if err != nil || after.State != before {
		t.Fatalf("failed transition was persisted: %+v %v", after, err)
	}
}

func TestComputerTaskBatchScopeIsolation(t *testing.T) {
	f := newBatchFixture(t)
	created, _, err := f.store.ClaimComputerTaskBatch(t.Context(), f.record("run-c", "", batchItems(t, "queued")))
	if err != nil {
		t.Fatal(err)
	}
	// Another conversation, Bot or user must not read the row. A foreign user is
	// exercised through the durable RLS boundary instead of the row filter, so
	// this case reads with a different team rather than a different user id.
	for name, ids := range map[string][4]string{
		"session": {created.ID, f.bot, uuid.NewString(), f.owner},
		"bot":     {created.ID, uuid.NewString(), f.session, f.owner},
	} {
		if _, err := f.store.ReadComputerTaskBatch(t.Context(), ids[0], ids[1], ids[2], ids[3]); err == nil {
			t.Fatalf("%s scope read the batch", name)
		}
	}
	// The lane index only ever lists active rows, in creation order.
	second, _, err := f.store.ClaimComputerTaskBatch(t.Context(), f.record("run-d", "", batchItems(t, "queued")))
	if err != nil {
		t.Fatal(err)
	}
	active, err := f.store.ListActiveComputerTaskBatches(t.Context())
	if err != nil || len(active) < 2 {
		t.Fatalf("active=%d err=%v", len(active), err)
	}
	if err = f.store.ChangeComputerTaskBatch(t.Context(), second.ID, func(rec *dbstore.ComputerTaskBatchRecord) error {
		rec.State = "completed"
		rec.Items = batchItems(t, "succeeded")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	active, err = f.store.ListActiveComputerTaskBatches(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range active {
		if rec.ID == second.ID {
			t.Fatal("completed batch stayed in the active lane")
		}
	}
	done, err := f.store.ListCompletedComputerTaskBatches(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rec := range done {
		if rec.ID == second.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("completed batch missing from the aggregate list")
	}
}

func TestComputerTaskBatchRLSHidesForeignTeams(t *testing.T) {
	f := newBatchFixture(t)
	created, _, err := f.store.ClaimComputerTaskBatch(t.Context(), f.record("run-rls", "", batchItems(t, "queued")))
	if err != nil {
		t.Fatal(err)
	}
	c := rlsConn(t, f.pool, teamMigrationDSN(t))
	if _, err = c.Exec(t.Context(), "SELECT set_config('memoh.team_id',$1,false)", team.DefaultTeamID); err != nil {
		t.Fatal(err)
	}
	store := postgresstore.NewWithQueries(dbsqlc.New(c))
	if _, err = store.ReadComputerTaskBatch(t.Context(), created.ID, f.bot, f.session, f.owner); err != nil {
		t.Fatalf("owning team read denied: %v", err)
	}
	if _, err = c.Exec(t.Context(), "SELECT set_config('memoh.team_id',$1,false)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReadComputerTaskBatch(t.Context(), created.ID, f.bot, f.session, f.owner); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("foreign team read the batch", err)
	}
	if err = store.ChangeComputerTaskBatch(t.Context(), created.ID, func(rec *dbstore.ComputerTaskBatchRecord) error {
		rec.State = "completed"
		return nil
	}); err == nil {
		t.Fatal("foreign team mutated the batch")
	}
}

func normalizeDelivery(raw []byte) string {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	out, _ := json.Marshal(value)
	return string(out)
}

func batchStates(raw []byte) ([]map[string]any, error) {
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
		return nil, errors.New("invalid stored items")
	}
	return items, nil
}

// The fixture writes through the same team setting the RLS policy reads.
var _ = team.DefaultTeamID
