package computertask

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/nativeherdr"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

func TestNativeComplexProducerSubmitFitsCLIAndPreservesUnknown(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	probeOnly := errors.New("synthetic Runtime preflight reached")
	probes := 0
	cli := nativeherdr.NewExecutor(func(context.Context, agenvo.Scope, agenvo.Target) (*bridge.Client, bridge.WorkspaceInfo, error) {
		probes++
		return nil, bridge.WorkspaceInfo{}, probeOnly
	})
	service.relay.SetNativeCaller(func(ctx context.Context, scope agenvo.Scope, target agenvo.Target, method string, params map[string]any) (agenvo.Receipt, error) {
		if method == "pane.run" && params["paneId"] == "job" {
			// Invoke the actual production CLI argument encoder, stopping at its
			// injected Runtime resolver. No Exec or real device is contacted.
			if _, err := cli.Invoke(ctx, scope, target, method, params); !errors.Is(err, probeOnly) {
				t.Fatalf("native launch rejected by actual CLI preflight: %v", err)
			}
		}
		return f.invoke(ctx, scope, target, method, params)
	})
	in := Submit{Kind: "herdr_pi_task", WorkDir: ".", Task: strings.Repeat("雪", 1145), Artifact: &deliverymeta.Artifact{SourceRef: strings.Repeat("r", 23), Mime: "text/plain"}}
	invalid := in
	invalid.WorkDir = "synthetic_child"
	if _, err := service.submit(ctx, f.binding, invalid); !errors.Is(err, ErrInvalid) || len(f.store.records) != 0 || len(f.batch.calls) != 0 {
		t.Fatal("artifact child workdir bypassed its original pre-claim guard", err)
	}
	f.batch.lost, f.batch.missing = true, true
	first, err := service.submit(ctx, f.binding, in)
	if !errors.Is(err, agenvo.ErrUnknown) || first.ID == "" || first.State != "unknown" || probes != 1 || f.batch.runs != 1 {
		t.Fatalf("complex native request did not reach one bounded dispatch: %+v %v probes=%d runs=%d", first, err, probes, f.batch.runs)
	}
	original := f.store.records[first.ID]
	plan := f.batch.progress.Producer
	if plan == nil || plan.Task.OperationID != first.ID || plan.Task.RunID != f.binding.RunID || plan.Task.TargetID != f.binding.TargetID || plan.Task.RootHash != deliverymeta.RootHash(f.profile.WorkDir) {
		t.Fatal("producer lost the original native operation authority")
	}
	service = f.restart()
	later := f.binding
	later.RunID = "synthetic-reconnected-run"
	ctx, err = toolcontext.WithComputerBinding(t.Context(), later)
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.submit(ctx, later, in)
	if err != nil || again.ID != first.ID || again.State != "unknown" || len(f.store.records) != 1 || f.batch.runs != 1 {
		t.Fatal("restart replayed the complex request or replaced its operation", err)
	}
	f.batch.missing, f.batch.lost = false, false
	var got Result
	for range 5 {
		got, err = service.observe(ctx, later, first.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if got.PiResult != nil {
			break
		}
	}
	saved := f.store.records[first.ID]
	if got.State != "completed" || got.PiResult == nil || got.PiResult.Outcome != "succeeded" || got.Delivery == nil || got.Delivery.State != "pending" || got.Delivery.Reference != nil || saved.RunID != original.RunID || saved.TargetFingerprint != original.TargetFingerprint || f.batch.runs != 1 {
		t.Fatalf("original receipt recovery changed identity or fabricated NAS delivery: %+v", got)
	}
}
