//go:build integration

package db_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	memohdb "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
)

func TestComputerAdmissionEndpointConfigurationPreservesUnknownOperation(t *testing.T) {
	f := newAdmissionFixture(t)
	var config map[string]any
	if err := json.Unmarshal(f.config, &config); err != nil {
		t.Fatal(err)
	}
	config["work_dir"], config["bash_path"] = `C:\Synthetic`, `C:\Synthetic\bash.exe`
	raw, _ := json.Marshal(config)
	if _, err := f.store.ConfigureAgenvoRuntime(t.Context(), f.owner, f.runtime, f.config, raw); err != nil {
		t.Fatal(err)
	}
	f.config = raw
	target, err := agenvo.DecodeTarget(raw)
	if err != nil {
		t.Fatal(err)
	}
	input := f.operation(f.mount)
	input.TargetFingerprint = agenvo.TargetFingerprint(target)
	op, won, err := f.store.ClaimComputerOperation(t.Context(), input)
	if err != nil || !won {
		t.Fatal(won, err)
	}
	op.State = "unknown"
	op, err = f.store.SaveComputerOperation(t.Context(), op)
	if err != nil {
		t.Fatal(err)
	}
	config["owner_endpoint"] = map[string]string{"program_path": `D:\Endpoint\endpoint.mjs`, "host_root": `E:\Host data`}
	next, _ := json.Marshal(config)
	if _, err = f.store.ConfigureAgenvoRuntime(t.Context(), uuid.NewString(), f.runtime, f.config, next); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("foreign owner changed locator", err)
	}
	row, err := f.store.ConfigureAgenvoRuntime(t.Context(), f.owner, f.runtime, f.config, next)
	if err != nil {
		t.Fatal(err)
	}
	current, err := agenvo.DecodeTarget(row.ProviderConfig)
	if err != nil || agenvo.TargetFingerprint(current) != op.TargetFingerprint {
		t.Fatal("configuration invalidated task fingerprint", err)
	}
	if _, err = f.store.ConfigureAgenvoRuntime(t.Context(), f.owner, f.runtime, f.config, next); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("stale configuration CAS succeeded", err)
	}
	f.config = row.ProviderConfig
	gate := f.change(t, uuid.NewString(), 0, true)
	if _, err = f.store.ConfigureAgenvoRuntime(t.Context(), f.owner, f.runtime, f.config, raw); !errors.Is(err, memohdb.ErrComputerAdmissionPaused) {
		t.Fatal("live pause allowed locator change", err)
	}
	restarted := postgresstore.NewWithQueries(dbsqlc.New(f.conn))
	read, err := restarted.OwnedAgenvoRuntime(t.Context(), f.owner, f.runtime)
	if err != nil || string(read.ProviderConfig) != string(f.config) {
		t.Fatal("locator did not persist", err)
	}
	got, err := restarted.ReadComputerOperation(t.Context(), op)
	if err != nil || !reflect.DeepEqual(got, op) {
		t.Fatal("unknown operation, revision or recovery-reader changed", err)
	}
	input.ID = uuid.NewString()
	if got, won, err := restarted.ClaimComputerOperation(t.Context(), input); err != nil || won || got.ID != op.ID || got.Revision != op.Revision {
		t.Fatal("existing unknown operation replayed or lost", won, err)
	}
	f.change(t, gate.RequestID, gate.Revision, false)
	t.Log("PASS original unknown operation/revision/fingerprint/recovery-reader unchanged; same claim deduplicated; owner configuration persisted; stale/foreign CAS and paused mutation rejected")
}
