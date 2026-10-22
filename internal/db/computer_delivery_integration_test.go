//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	memohdb "github.com/felinics/memoh/internal/db"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestComputerDeliveryPostgresAuthorizationCASAndPreservation(t *testing.T) {
	ctx := t.Context()
	ownerPool := freshMigratedDB(t) // Creates and drops a separate synthetic database.
	config := ownerPool.Config().Copy()
	config.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := postgresstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner, user, bot, session, connection := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO users(id) VALUES($1),($2)", owner, user)
	exec("INSERT INTO team_members(user_id) VALUES($1),($2)", owner, user)
	exec("INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'delivery-fixture')", bot, owner)
	exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id) VALUES($1,$2,$3)", session, bot, user)
	exec(`INSERT INTO bot_user_grants(bot_id,subject_type,user_id,permissions) VALUES($1,'user',$2,'["chat","workspace_read","workspace_write","workspace_exec"]')`, bot, user)
	exec("INSERT INTO mcp_connections(id,bot_id,name,type) VALUES($1,$2,'delivery-fixture','http')", connection, bot)
	target := agenvo.Target{ConnectionID: connection, DeviceID: "synthetic-device", InstanceID: "synthetic-instance", Session: "fixture", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\config`}}
	raw, _ := json.Marshal(target)
	runtime, err := store.CreateAgenvoRuntime(ctx, owner, "delivery-fixture", raw)
	if err != nil {
		t.Fatal(err)
	}
	mount, err := store.CreateOrUpdateMount(ctx, bot, runtime.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := computertask.DeliveryIdentity{Binding: toolcontext.ComputerBinding{BotID: bot, UserID: user, SessionID: session, TargetID: mount.ID, RunID: "synthetic-run"}, OperationID: uuid.NewString(), DeviceID: target.DeviceID, InstanceID: target.InstanceID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	details, _ := json.Marshal(computertask.Details{Kind: "herdr_pi_task", Batch: &computertask.BatchDetails{Progress: agenvo.BatchProgress{Attempt: id.Attempt, Generation: id.Generation}}, Phase: "submitted"})
	op, claimed, err := store.ClaimComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: bot, UserID: user, SessionID: session, TargetID: mount.ID, RunID: id.Binding.RunID, Fingerprint: "synthetic-fingerprint", TargetFingerprint: agenvo.TargetFingerprint(target), Details: details})
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	ctx, err = toolcontext.WithComputerBinding(ctx, id.Binding)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = computertask.WithDeliveryIdentity(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	mediaRoot := t.TempDir()
	assets := media.NewService(nil, localfs.NewOriginals(mediaRoot))
	if err := assets.EnableUploads(mediaRoot); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = assets.Uploads().Close() }()
	assets.SetRequirePersistence(true)
	asset, err := assets.Ingest(ctx, media.IngestInput{BotID: bot, Mime: "text/plain", Reader: strings.NewReader("synthetic delivery content"), MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	ref := computertask.DeliveryRequest{Version: 1, Direction: "upload", ContentHash: asset.ContentHash, SizeBytes: asset.SizeBytes, Mime: asset.Mime}
	// Exercise the production adapter under a non-superuser, NOBYPASSRLS
	// fixture role. Keep the admin pool only for synthetic revoke operations.
	rls := rlsConn(t, pool, teamMigrationDSN(t))
	var role string
	if err := rls.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := pool.Config().Copy()
	runtimeConfig.ConnConfig.RuntimeParams["role"] = role
	runtimePool, err := pgxpool.NewWithConfig(ctx, runtimeConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtimePool.Close)
	store, err = postgresstore.New(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	s := computertask.NewPostgresDeliveryService(store, assets)
	out, err := s.Commit(ctx, id.OperationID, "result", ref)
	if err != nil || out.Outcome != "committed" {
		t.Fatalf("real authorization/commit: %+v %v", out, err)
	}
	// A restarted service can resolve/replay using only the existing DB record.
	s = computertask.NewPostgresDeliveryService(store, assets)
	if again, err := s.Commit(ctx, id.OperationID, "result", ref); err != nil || again != out {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := store.SaveComputerOperation(ctx, op); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatalf("stale execution overwrote delivery: %v", err)
	}
	current, err := store.ReadComputerOperation(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	// Existing execution writers do not know the ledger field. The SQL keeps
	// it while allowing new execution receipt facts at the current revision.
	current.Details, current.State = details, "completed"
	current, err = store.SaveComputerOperation(ctx, current)
	if err != nil || !strings.Contains(string(current.Details), "delivery_ledger") {
		t.Fatalf("ledger lost: %s %v", current.Details, err)
	}
	if got, err := s.Resolve(ctx, id.OperationID, "result", ref, true); err != nil || got.Outcome != "committed" {
		t.Fatalf("cancel after execution write: %+v %v", got, err)
	}

	// Exercise the pre-association SQL shape with a current revision. An old
	// writer must get an explicit failure, not erase the durable ledger.
	before := current.Revision
	_, err = runtimePool.Exec(ctx, "UPDATE computer_operations SET details=$1, revision=revision+1 WHERE id=$2 AND revision=$3", details, id.OperationID, before)
	var guardError *pgconn.PgError
	if !errors.As(err, &guardError) || guardError.Code != "23514" {
		t.Fatalf("old writer was not rejected: %v", err)
	}
	after, err := store.ReadComputerOperation(ctx, op)
	if err != nil || after.Revision != before || string(after.Details) != string(current.Details) {
		t.Fatalf("rejected writer changed facts: %+v %v", after, err)
	}
	_, err = runtimePool.Exec(ctx, "UPDATE computer_operations SET details=jsonb_set(details,'{delivery_ledger,version}','0') WHERE id=$1", id.OperationID)
	if !errors.As(err, &guardError) || guardError.Code != "23514" {
		t.Fatalf("ledger downgrade accepted: %v", err)
	}

	// Transactions must not silently fall back when only a query handle exists.
	if _, err := computertask.NewPostgresDeliveryService(postgresstore.NewWithQueries(store.SQLC()), assets).Resolve(ctx, id.OperationID, "result", ref, false); !errors.Is(err, computertask.ErrDeliveryBinding) {
		t.Fatalf("transaction fallback: %v", err)
	}

	// Every already-committed lookup and replay rechecks real current grants.
	for _, permission := range []string{"chat", "workspace_read", "workspace_write", "workspace_exec"} {
		t.Run("revoke-"+permission, func(t *testing.T) {
			permissions := []string{"chat", "workspace_read", "workspace_write", "workspace_exec"}
			var kept []string
			for _, p := range permissions {
				// workspace_write implies read under the existing permission model.
				if p != permission && !(permission == "workspace_read" && p == "workspace_write") {
					kept = append(kept, p)
				}
			}
			encoded, _ := json.Marshal(kept)
			exec("UPDATE bot_user_grants SET permissions=$1 WHERE bot_id=$2", encoded, bot)
			if _, err := s.Resolve(ctx, id.OperationID, "result", ref, false); err == nil {
				t.Fatal("revoked lookup accepted")
			}
			if _, err := s.Commit(ctx, id.OperationID, "result", ref); err == nil {
				t.Fatal("revoked replay accepted")
			}
			exec(`UPDATE bot_user_grants SET permissions='["chat","workspace_read","workspace_write","workspace_exec"]' WHERE bot_id=$1`, bot)
		})
	}
	for _, item := range []struct {
		name, off, on string
		value         any
	}{
		{"caller-inactive", "UPDATE users SET is_active=false WHERE id=$1", "UPDATE users SET is_active=true WHERE id=$1", user},
		{"membership-inactive", "UPDATE team_members SET is_active=false WHERE user_id=$1", "UPDATE team_members SET is_active=true WHERE user_id=$1", user},
		{"owner-inactive", "UPDATE users SET is_active=false WHERE id=$1", "UPDATE users SET is_active=true WHERE id=$1", owner},
		{"runtime-revoked", "UPDATE user_runtimes SET revoked_at=now() WHERE id=$1", "UPDATE user_runtimes SET revoked_at=NULL WHERE id=$1", runtime.ID},
		{"session-reassigned", "UPDATE bot_sessions SET created_by_user_id=NULL WHERE id=$1", "UPDATE bot_sessions SET created_by_user_id='" + user + "' WHERE id=$1", session},
	} {
		t.Run(item.name, func(t *testing.T) {
			exec(item.off, item.value)
			if _, err := s.Resolve(ctx, id.OperationID, "result", ref, true); !errors.Is(err, computertask.ErrDenied) {
				t.Fatalf("revoked scope must be forbidden, not retryable: %v", err)
			}
			exec(item.on, item.value)
		})
	}

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_, err := s.Commit(ctx, id.OperationID, "race", ref)
				if err != nil && !errors.Is(err, computertask.ErrDeliveryConflict) && !errors.Is(err, computertask.ErrDeliveryRetry) {
					t.Error(err)
				}
			} else if _, err := s.Resolve(ctx, id.OperationID, "race", ref, true); err != nil && !errors.Is(err, computertask.ErrDeliveryRetry) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	final, err := s.Resolve(ctx, id.OperationID, "race", ref, false)
	if err != nil || (final.Outcome != "committed" && final.Outcome != "canceled") {
		t.Fatalf("race: %+v %v", final, err)
	}
	if final.Outcome == "canceled" {
		if _, err := s.Commit(ctx, id.OperationID, "race", ref); !errors.Is(err, computertask.ErrDeliveryConflict) {
			t.Fatalf("late commit: %v", err)
		}
	}
	t.Run("https-queue-handoff", func(t *testing.T) { exerciseDeliveryHandoff(ctx, t, store, assets, id, ref) })
	t.Run("task-principal-http", func(t *testing.T) { exerciseTaskTransferPrincipal(ctx, t, store, assets, id, ref) })
	_, err = runtimePool.Exec(ctx, "UPDATE computer_operations SET details=jsonb_set(details,'{delivery_ledger,version}','2') WHERE id=$1", id.OperationID)
	if !errors.As(err, &guardError) || guardError.Code != "23514" {
		t.Fatalf("old v2 writer downgraded principal ledger: %v", err)
	}
	// No outcome changes when the connection owner becomes unavailable.
	exec("DELETE FROM mcp_connections WHERE id=$1", connection)
	if _, err := s.Resolve(ctx, id.OperationID, "result", ref, false); !errors.Is(err, computertask.ErrDenied) {
		t.Fatal("deleted connection resolved")
	}
}

// Verify canceled contexts are errors rather than fabricated absent outcomes.
func TestComputerDeliveryPostgresCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := computertask.NewPostgresDeliveryService(nil, nil).Resolve(ctx, uuid.NewString(), "result", computertask.DeliveryRequest{}, false); err == nil {
		t.Fatal("canceled query acknowledged")
	}
}
