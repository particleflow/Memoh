package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

type nativeTaskStore struct {
	*fixtureStore
}

type nativeTaskFixture struct {
	t       *testing.T
	store   *nativeTaskStore
	batch   *batchTransport
	assets  *batchAssets
	binding toolcontext.ComputerBinding
	profile agenvo.NativeHerdrProfile
	denied  bool
}

func (f *nativeTaskFixture) HasBotPermission(_ context.Context, bot, user, permission string) (bool, error) {
	return !f.denied && bot == f.binding.BotID && user == f.binding.UserID && permission == "workspace_exec", nil
}

func newNativeTaskFixture(t *testing.T) (*nativeTaskFixture, *Service, context.Context) {
	t.Helper()
	_, store, batch, assets := setupBatch(t)
	f := &nativeTaskFixture{
		t: t, store: &nativeTaskStore{fixtureStore: store}, batch: batch, assets: assets,
		binding: toolcontext.ComputerBinding{BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "native-first-run"},
		profile: agenvo.NativeHerdrProfile{InstanceID: "herdr-native", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{Provider: "synthetic", Model: "synthetic-model", ConfigDir: `C:\Synthetic\Pi`, ExecutionMode: "pi-json-v1"}},
	}
	f.profile.Producer = agenvo.ProducerLocator{ProgramPath: `C:\Synthetic\producer.exe`, ConfigDir: `C:\Synthetic\producer-config`, StateDir: `C:\Synthetic\producer-state`}
	raw, err := json.Marshal(map[string]any{"herdr": f.profile})
	if err != nil {
		t.Fatal(err)
	}
	store.mounts = map[string]dbstore.BotRemoteRuntimeBindingRecord{f.binding.TargetID: {
		ID: f.binding.TargetID, BotID: f.binding.BotID, RuntimeID: uuid.NewString(), RuntimeName: "Synthetic native PC", RuntimeUserID: "owner", BotOwnerUserID: "owner", RuntimeProvider: "memoh", RuntimeProviderConfig: raw,
	}}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), f.binding)
	if err != nil {
		t.Fatal(err)
	}
	return f, f.service(), ctx
}

func (f *nativeTaskFixture) service() *Service {
	relay := agenvo.NewService(f.store, f, f.invoke)
	return NewService(f.store, f.store, relay, f.assets, func(context.Context, toolcontext.ComputerBinding, string, string, string) error { return nil })
}

// invoke checks the selected Runtime and forwards fixed native operations to
// the synthetic CLI fixture. nativeherdr separately tests the Exec transport.
func (f *nativeTaskFixture) invoke(ctx context.Context, scope agenvo.Scope, target agenvo.Target, method string, params map[string]any) (agenvo.Receipt, error) {
	f.t.Helper()
	mount := f.store.mounts[f.binding.TargetID]
	if scope.BotID != f.binding.BotID || scope.UserID != f.binding.UserID || scope.TargetID != f.binding.TargetID || target.NativeRuntimeID != mount.RuntimeID || target.ConnectionID != mount.RuntimeID || target.DeviceID != mount.RuntimeID || target.InstanceID != f.profile.InstanceID {
		f.t.Fatalf("native dispatch lost selected Runtime identity: scope=%+v target=%+v", scope, target)
	}
	return f.batch.call(ctx, scope, target, method, params)
}

func (f *nativeTaskFixture) restart() *Service {
	f.t.Helper()
	// Reconstruct the host service from serialized ledger rows, with no service
	// object or active-turn state retained. Real PG/Runtime process restart is
	// covered by the dedicated integration suites, not claimed by this fixture.
	raw, err := json.Marshal(f.store.records)
	if err != nil {
		f.t.Fatal(err)
	}
	reopened := &fixtureStore{mounts: f.store.mounts}
	if err = json.Unmarshal(raw, &reopened.records); err != nil {
		f.t.Fatal(err)
	}
	f.store = &nativeTaskStore{fixtureStore: reopened}
	f.batch.store = reopened
	return f.service()
}

func TestNativeRuntimeTaskUnknownReopensSameOperationWithoutBusinessReplay(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	f.batch.lost, f.batch.missing = true, true
	in := Submit{Kind: "herdr_pi_task", Task: "compute synthetic 42"}
	first, err := service.submit(ctx, f.binding, in)
	if !errors.Is(err, agenvo.ErrUnknown) || first.State != "unknown" || first.ID == "" || f.batch.runs != 1 || first.ProcessStopped || first.PiResult != nil {
		t.Fatalf("lost native dispatch: %+v %v runs=%d", first, err, f.batch.runs)
	}
	before := f.store.records[first.ID]
	service = f.restart()
	later := f.binding
	later.RunID = "native-reconnected-run"
	ctx, err = toolcontext.WithComputerBinding(t.Context(), later)
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.submit(ctx, later, in)
	if err != nil || again.ID != first.ID || again.State != "unknown" || len(f.store.records) != 1 || f.batch.runs != 1 {
		t.Fatalf("reconnect replaced or replayed unknown operation: %+v %v", again, err)
	}
	in.Task = "different synthetic request"
	blocked, err := service.submit(ctx, later, in)
	if !errors.Is(err, ErrConflict) || blocked.BlockedByOperation != first.ID || f.batch.runs != 1 {
		t.Fatalf("unknown did not block fresh business input: %+v %v", blocked, err)
	}
	got, err := service.observe(ctx, later, first.ID, true)
	if err != nil || got.ID != first.ID || got.State != "unknown" || got.ProcessStopped || got.PiResult != nil {
		t.Fatalf("missing receipt was treated as completion: %+v %v", got, err)
	}
	f.batch.missing, f.batch.lost = false, false
	for range 5 {
		got, err = service.observe(ctx, later, first.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if got.PiResult != nil {
			break
		}
	}
	after := f.store.records[first.ID]
	if got.State != "completed" || got.PiResult == nil || got.PiResult.Outcome != "succeeded" || f.batch.runs != 1 || after.RunID != before.RunID || after.ID != before.ID || after.TargetID != before.TargetID || after.TargetFingerprint != before.TargetFingerprint {
		t.Fatalf("recovered receipt lost original task/run identity: %+v original=%+v current=%+v", got, before, after)
	}
}

func TestNativeRuntimeAcceptedCLIRequiresMatchingPiCompletion(t *testing.T) {
	for _, mode := range []string{"missing_receipt", "wrong_receipt", "missing_completion", "exit_zero_error"} {
		t.Run(mode, func(t *testing.T) {
			f, service, ctx := newNativeTaskFixture(t)
			if mode == "exit_zero_error" {
				f.batch.reason = "error"
			}
			first, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "synthetic completion boundary"})
			if err != nil || first.State != "submitted" || first.PiResult != nil || first.ProcessStopped {
				t.Fatalf("accepted CLI manufactured completion: %+v %v", first, err)
			}
			switch mode {
			case "missing_receipt":
				f.batch.missing = true
			case "wrong_receipt":
				f.batch.receipt.RequestHash = strings.Repeat("0", 64)
			case "missing_completion":
				f.batch.noCompletion = true
			}
			got, err := service.observe(ctx, f.binding, first.ID, true)
			if mode == "wrong_receipt" || mode == "missing_completion" {
				if err == nil {
					t.Fatal("unverified completion accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var persisted Details
			if err := json.Unmarshal(f.store.records[first.ID].Details, &persisted); err != nil {
				t.Fatal(err)
			}
			if mode == "exit_zero_error" {
				if got.PiResult == nil || got.PiResult.Outcome != "failed" || !got.ProcessStopped || got.ExitCode == nil || *got.ExitCode != 0 {
					t.Fatalf("exit 0 replaced Pi outcome: %+v", got)
				}
			} else if persisted.Batch.Result != nil {
				t.Fatal("receipt failure published a Pi result")
			}
			if f.batch.runs != 1 {
				t.Fatal("completion failure replayed business input")
			}
		})
	}
}

func TestNativeRuntimeTaskRechecksSelectedPCPermissionAndFingerprint(t *testing.T) {
	for _, mode := range []string{"other_selected_pc", "permission_revoked", "runtime_revoked", "owner_changed", "runtime_replaced", "profile_changed"} {
		t.Run(mode, func(t *testing.T) {
			f, service, ctx := newNativeTaskFixture(t)
			first, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "synthetic scoped task"})
			if err != nil {
				t.Fatal(err)
			}
			before := len(f.batch.calls)
			mount := f.store.mounts[f.binding.TargetID]
			binding := f.binding
			switch mode {
			case "other_selected_pc":
				binding.TargetID = uuid.NewString()
				ctx, err = toolcontext.WithComputerBinding(t.Context(), binding)
				if err != nil {
					t.Fatal(err)
				}
			case "permission_revoked":
				f.denied = true
			case "runtime_revoked":
				f.store.revoked = true
			case "owner_changed":
				mount.BotOwnerUserID = "different-owner"
			case "runtime_replaced":
				mount.RuntimeID = uuid.NewString()
			case "profile_changed":
				profile := f.profile
				profile.HerdrConfigRoot = `C:\Different\herdr`
				mount.RuntimeProviderConfig, _ = json.Marshal(map[string]any{"herdr": profile})
			}
			f.store.mounts[f.binding.TargetID] = mount
			if _, err = service.observe(ctx, binding, first.ID, true); err == nil {
				t.Fatal("changed authority/registration observed task")
			}
			if _, err = service.cancel(ctx, binding, first.ID); err == nil {
				t.Fatal("changed authority/registration canceled task")
			}
			if len(f.batch.calls) != before || f.batch.runs != 1 {
				t.Fatal("denial reached native Runtime")
			}
		})
	}
}

func TestNativeRuntimeArtifactRequiresProducerLocatorBeforeClaim(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	f.profile.Producer = agenvo.ProducerLocator{}
	mount := f.store.mounts[f.binding.TargetID]
	mount.RuntimeProviderConfig, _ = json.Marshal(map[string]any{"herdr": f.profile})
	f.store.mounts[f.binding.TargetID] = mount
	_, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "write result", Artifact: &deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"}})
	if !errors.Is(err, agenvo.ErrUnsupported) || len(f.store.records) != 0 || len(f.batch.calls) != 0 {
		t.Fatal("missing producer location claimed or dispatched work", err)
	}
}

func TestNativeRuntimeBusinessCompletionDoesNotBypassProducerDelivery(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	// No HostFS/media importer is supplied. The compact result can complete,
	// but a file is deliverable only from the original producer's NAS ledger.
	service.assets = nil
	in := Submit{Kind: "herdr_pi_task", Task: "write declared synthetic output", Artifact: &deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"}}
	first, err := service.submit(ctx, f.binding, in)
	if err != nil {
		t.Fatal(err)
	}
	plan := f.batch.progress.Producer
	if plan == nil || plan.Task.OperationID != first.ID || plan.Task.RunID != f.binding.RunID || plan.Task.TargetID != f.binding.TargetID || plan.Task.RootHash != deliverymeta.RootHash(f.profile.WorkDir) {
		t.Fatal("native transport replaced producer authority")
	}
	var got Result
	for range 5 {
		got, err = service.observe(ctx, f.binding, first.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if got.PiResult != nil {
			break
		}
	}
	if got.PiResult == nil || got.PiResult.Outcome != "succeeded" || got.Delivery == nil || got.Delivery.State != "pending" || got.Delivery.Reference != nil || got.ArtifactHash != "" || len(f.assets.data) != 0 || f.batch.runs != 1 {
		t.Fatalf("execution/CLI success became file delivery: %+v", got)
	}
	artifact, err := service.artifactForObservation(ctx, f.binding, first.ID, "")
	if err != nil || artifact.Delivery == nil || artifact.Delivery.Reference != nil || artifact.ArtifactHash != "" || len(f.assets.data) != 0 {
		t.Fatalf("artifact read imported PC bytes outside producer ledger: %+v %v", artifact, err)
	}
	before := f.batch.producerWakes
	canceled, err := service.cancel(ctx, f.binding, first.ID)
	if err != nil || !canceled.CancelRequested || canceled.State != "completed" || canceled.Delivery.State != "canceled" || canceled.Delivery.Reference != nil {
		t.Fatalf("delivery cancellation rewrote business outcome: %+v %v", canceled, err)
	}
	if _, err = service.observe(ctx, f.binding, first.ID, true); err != nil || f.batch.producerWakes != before || f.batch.runs != 1 {
		t.Fatal("canceled delivery was rewoken or business rerun", err)
	}
}

func TestNativeRuntimeDeliveryRequiresVerifiedDeviceAndOriginalRegistration(t *testing.T) {
	for _, mode := range []string{"valid", "host_binding_only", "other_connection", "other_device", "other_instance", "changed_config"} {
		t.Run(mode, func(t *testing.T) {
			f, service, _, ref := newDeliveryFixture(t)
			runtimeID := uuid.NewString()
			profile := agenvo.NativeHerdrProfile{InstanceID: "herdr-native", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{Provider: "synthetic", Model: "synthetic-model", ConfigDir: `C:\Synthetic\Pi`, ExecutionMode: "pi-json-v1"}}
			config, _ := json.Marshal(map[string]any{"herdr": profile})
			var err error
			f.target, err = agenvo.DecodeNativeTarget(runtimeID, config)
			if err != nil {
				t.Fatal(err)
			}
			f.id.DeviceID, f.id.InstanceID = f.target.DeviceID, f.target.InstanceID
			f.record.TargetFingerprint = agenvo.TargetFingerprint(f.target)
			now := time.Now()
			p := VerifiedTransfer{Identity: f.id, DeliveryID: "native-file", Reference: ref, Audience: TransferAudience, CredentialID: "synthetic-device-grant", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Device: VerifiedTransferDevice{CredentialID: "synthetic-device-grant", ConnectionID: runtimeID, DeviceID: runtimeID, InstanceID: profile.InstanceID}}
			if mode == "host_binding_only" {
				ctx, err := toolcontext.WithComputerBinding(t.Context(), f.id.Binding)
				if err != nil {
					t.Fatal(err)
				}
				if err = service.ReserveTransfer(ctx); !errors.Is(err, ErrDeliveryBinding) || f.record.Revision != 3 {
					t.Fatal("Runtime/HostFS context alone established delivery authority", err)
				}
				return
			}
			switch mode {
			case "other_connection":
				p.Device.ConnectionID = uuid.NewString()
			case "other_device":
				p.Device.DeviceID = uuid.NewString()
			case "other_instance":
				p.Device.InstanceID = "different-herdr"
			case "changed_config":
				f.target.HerdrConfigRoot = `C:\Different\herdr`
			}
			ctx, err := BindVerifiedTransfer(t.Context(), p)
			if err != nil {
				t.Fatal(err)
			}
			err = service.ReserveTransfer(ctx)
			if mode == "valid" {
				if err != nil || f.record.Revision != 4 {
					t.Fatal("original native producer could not reserve its transfer", err)
				}
			} else if err == nil || f.record.Revision != 3 || f.verifications != 0 {
				t.Fatal("another native identity/config acquired file authority", err)
			}
		})
	}
}
