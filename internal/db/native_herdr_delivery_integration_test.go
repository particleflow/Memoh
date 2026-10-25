//go:build integration

package db_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/userruntime"
)

func TestNativeHerdrDeliveryUsesTransactionalRegistry(t *testing.T) {
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
	f.config = raw
	target, err := agenvo.DecodeNativeTarget(f.runtime, raw)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, "UPDATE bot_sessions SET created_by_user_id=$2 WHERE id=$1", f.session, f.owner)
	// Exercise the production transaction adapter with forced RLS, not a DB owner.
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
	defer pool.Close()
	store, err := postgresstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	credentials := userruntime.NewTransferCredentials(store, true)
	enrolled, err := credentials.Enroll(t.Context(), f.owner, f.runtime, userruntime.ChangeTransferCredentialRequest{})
	if err != nil {
		t.Fatal("native purpose-specific enrollment", err)
	}
	device, err := credentials.VerifyDevice(t.Context(), enrolled.Credential)
	if err != nil || device.ConnectionID != f.runtime || device.DeviceID != f.runtime || device.InstanceID != profile.InstanceID {
		t.Fatal("native credential identity", err)
	}
	if _, err = credentials.VerifyDevice(t.Context(), enrolled.Credential+"invalid"); err == nil {
		t.Fatal("wrong native secret accepted")
	}
	input := f.operation(f.mount)
	id := computertask.DeliveryIdentity{Binding: toolcontext.ComputerBinding{BotID: f.bot, UserID: f.owner, SessionID: f.session, TargetID: f.mount, RunID: input.RunID}, OperationID: input.ID, DeviceID: target.DeviceID, InstanceID: target.InstanceID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	input.TargetFingerprint = agenvo.TargetFingerprint(target)
	input.Details, _ = json.Marshal(computertask.Details{Kind: "herdr_pi_task", Batch: &computertask.BatchDetails{Progress: agenvo.BatchProgress{Attempt: id.Attempt, Generation: id.Generation}}, Phase: "submitted"})
	if _, won, err := store.ClaimComputerOperation(t.Context(), input); err != nil || !won {
		t.Fatal(won, err)
	}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), id.Binding)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = computertask.WithDeliveryIdentity(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	assets := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
	asset, err := assets.Ingest(ctx, media.IngestInput{BotID: f.bot, Mime: "text/plain", Reader: strings.NewReader("synthetic native delivery"), MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	ref := computertask.DeliveryRequest{Version: 1, Direction: "upload", ContentHash: asset.ContentHash, SizeBytes: asset.SizeBytes, Mime: asset.Mime}
	service := computertask.NewPostgresDeliveryService(store, assets)
	committed, err := service.Commit(ctx, input.ID, "native-result", ref)
	if err != nil || committed.Outcome != "committed" {
		t.Fatal("native transaction commit", committed, err)
	}
	service = computertask.NewPostgresDeliveryService(store, assets)
	if replay, err := service.Commit(ctx, input.ID, "native-result", ref); err != nil || replay != committed {
		t.Fatal("native restart replay", err)
	}
	wrong := id
	wrong.DeviceID = uuid.NewString()
	wrongCtx, err := toolcontext.WithComputerBinding(t.Context(), wrong.Binding)
	if err != nil {
		t.Fatal(err)
	}
	wrongCtx, err = computertask.WithDeliveryIdentity(wrongCtx, wrong)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Resolve(wrongCtx, input.ID, "native-result", ref, false); err == nil {
		t.Fatal("other native PC read delivery")
	}
	changed := profile
	changed.InstanceID = "another-native-instance"
	changedRaw, _ := json.Marshal(map[string]any{"herdr": changed})
	if _, err = store.ConfigureNativeHerdrRuntime(ctx, f.owner, f.runtime, raw, changedRaw); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Resolve(ctx, input.ID, "native-result", ref, false); err == nil {
		t.Fatal("changed native fingerprint read old delivery")
	}
	if _, err = credentials.VerifyDevice(ctx, enrolled.Credential); !errors.Is(err, userruntime.ErrTransferCredential) {
		t.Fatal("changed native instance retained credential authority", err)
	}
	if _, err = store.ConfigureNativeHerdrRuntime(ctx, f.owner, f.runtime, changedRaw, raw); err != nil {
		t.Fatal(err)
	}
	rotated, err := credentials.Enroll(ctx, f.owner, f.runtime, userruntime.ChangeTransferCredentialRequest{ExpectedID: enrolled.CredentialID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = credentials.VerifyDevice(ctx, enrolled.Credential); !errors.Is(err, userruntime.ErrTransferCredential) {
		t.Fatal("rotation retained old credential", err)
	}
	if _, err = credentials.VerifyDevice(ctx, rotated.Credential); err != nil {
		t.Fatal(err)
	}
	if err = credentials.Revoke(ctx, f.owner, f.runtime, rotated.CredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err = credentials.VerifyDevice(ctx, rotated.Credential); !errors.Is(err, userruntime.ErrTransferCredential) {
		t.Fatal("revocation retained credential", err)
	}
	f.exec(t, "UPDATE user_runtimes SET revoked_at=now() WHERE id=$1", f.runtime)
	if _, err = service.Resolve(ctx, input.ID, "native-result", ref, false); err == nil {
		t.Fatal("revoked native runtime read delivery")
	}
	saved, err := store.ReadComputerOperation(ctx, input)
	if err != nil || !strings.Contains(string(saved.Details), "delivery_ledger") {
		t.Fatal("original result ledger lost", err)
	}
	t.Log("native registry + forced RLS transaction: original task/run/operation and delivery replay preserved; foreign PC, changed instance/fingerprint and revoked runtime denied; dedicated synthetic transfer credential rotated/revoked without MCP")
}
