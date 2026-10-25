//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/transferclient"
	"github.com/felinics/memoh/internal/userruntime"
)

// This explicit combination uses the existing Linux protocol acceptance path.
// The producer must be a separate pinned checkout; no native-server product
// files are copied into it. Windows publication remains a separate acceptance.
func TestNativeTransferPinnedProducerCombination(t *testing.T) {
	producerSource := os.Getenv("MEMOH_NATIVE_PRODUCER_SOURCE")
	if producerSource == "" || !filepath.IsAbs(producerSource) || os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Fatal("explicit pinned producer checkout and isolated PostgreSQL DSN required")
	}
	f := newNativeHerdrFixture(t)
	profile := agenvo.NativeHerdrProfile{
		InstanceID: "synthetic-native-instance", Session: "synthetic-session", WorkDir: `C:\Synthetic`,
		BashPath: `C:\Synthetic\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`,
		Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\config`},
	}
	raw, err := json.Marshal(map[string]any{"herdr": profile})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ConfigureNativeHerdrRuntime(t.Context(), f.owner, f.runtime, f.config, raw); err != nil {
		t.Fatal(err)
	}
	target, err := agenvo.DecodeNativeTarget(f.runtime, raw)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, "UPDATE bot_sessions SET created_by_user_id=$2 WHERE id=$1", f.session, f.owner)
	rls := rlsConn(t, f.pool, teamMigrationDSN(t))
	var role string
	if err = rls.QueryRow(t.Context(), "SELECT current_user").Scan(&role); err != nil {
		t.Fatal(err)
	}
	cfg := f.pool.Config().Copy()
	cfg.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	cfg.ConnConfig.RuntimeParams["role"] = role
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := postgresstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	input := f.operation(f.mount)
	id := computertask.DeliveryIdentity{Binding: toolcontext.ComputerBinding{BotID: f.bot, UserID: f.owner, SessionID: f.session, TargetID: f.mount, RunID: input.RunID}, OperationID: input.ID, DeviceID: target.DeviceID, InstanceID: target.InstanceID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	input.TargetFingerprint = agenvo.TargetFingerprint(target)
	input.Details, _ = json.Marshal(computertask.Details{Kind: "herdr_pi_task", Batch: &computertask.BatchDetails{Progress: agenvo.BatchProgress{Attempt: id.Attempt, Generation: id.Generation}}, Phase: "submitted"})
	if _, won, err := store.ClaimComputerOperation(t.Context(), input); err != nil || !won {
		t.Fatal(won, err)
	}
	// Create a valid legacy device on the SAME owner/Bot. Its own mft must work
	// for its old operation but cannot authorize the native operation (or vice
	// versa). This is stronger than an invalid-token-only rejection fixture.
	connection := input.ID
	f.exec(t, "INSERT INTO mcp_connections(id,bot_id,name,type) VALUES($1,$2,'legacy-isolation','http')", connection, f.bot)
	legacyTarget := target
	legacyTarget.NativeRuntimeID, legacyTarget.HerdrCLI, legacyTarget.HerdrConfigRoot = "", "", ""
	legacyTarget.ConnectionID, legacyTarget.DeviceID, legacyTarget.InstanceID = connection, "synthetic-legacy-device", "synthetic-legacy-instance"
	legacyRaw, _ := json.Marshal(legacyTarget)
	legacy, err := store.CreateAgenvoRuntime(t.Context(), f.owner, "synthetic-legacy", legacyRaw)
	if err != nil {
		t.Fatal(err)
	}
	mount, err := store.CreateOrUpdateMount(t.Context(), f.bot, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacyOperation := f.operation(mount.ID)
	legacyOperation.TargetFingerprint = agenvo.TargetFingerprint(legacyTarget)
	legacyOperation.Details = input.Details
	if _, won, err := store.ClaimComputerOperation(t.Context(), legacyOperation); err != nil || !won {
		t.Fatal(won, err)
	}
	credentials := userruntime.NewTransferCredentials(store, true)
	legacyCredential, err := credentials.Enroll(t.Context(), f.owner, legacy.ID, userruntime.ChangeTransferCredentialRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), id.Binding)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = computertask.WithDeliveryIdentity(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	exerciseRealRouteRoundTripWithProducer(ctx, t, store, id, producerSource, func(base string, transport *http.Client, native *registeredTransferVerifier, job transferclient.Job) {
		if native.device.ConnectionID != f.runtime || native.device.DeviceID != f.runtime || native.device.InstanceID != profile.InstanceID {
			t.Fatal("native registry projection changed")
		}
		old := job
		old.OperationID, old.Task.TargetID, old.Task.RunID = legacyOperation.ID, mount.ID, legacyOperation.RunID
		probe := func(job transferclient.Job, token string, want int) {
			t.Helper()
			client, err := transferclient.NewDeviceTask(base, job, "download", job.Task, transport, func(context.Context) (string, error) { return "Memoh-Transfer " + token, nil })
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			_, err = client.Limits(ctx, job.BotID)
			if want == 200 {
				if err != nil {
					t.Fatal("own registered identity rejected", err)
				}
				return
			}
			var status *transferclient.HTTPError
			if !errors.As(err, &status) || status.Status != want {
				t.Fatal("cross-provider/purpose status mismatch", want, err)
			}
		}
		probe(job, native.token, 200)
		probe(old, legacyCredential.Credential, 200)
		probe(job, legacyCredential.Credential, 403)
		probe(old, native.token, 403)
		probe(job, "mrk_"+strings.Repeat("a", 64), 401)
		t.Log("real Server verifier/PG: native and legacy own identities accepted; both cross-provider directions denied=403; Runtime mrk rejected=401")
	})
	t.Log("server=2030c2d5 producer=0.10.1 separately pinned; task/attempt/receipt/reference retained across fresh CLI processes; Linux download transport only, no Windows publication claim")
}
