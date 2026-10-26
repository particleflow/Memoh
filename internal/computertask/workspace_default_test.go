package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestWorkspaceDefaultResumeKeepsOriginalDirectory(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	selected := `C:\Synthetic\first`
	defaults, creates := 0, 0
	service.WithDefaultWorkDir(func(context.Context, toolcontext.ComputerBinding, string, agenvo.Target) (string, error) {
		defaults++
		return selected, nil
	})
	service.relay.SetNativeCaller(func(_ context.Context, _ agenvo.Scope, _ agenvo.Target, method string, args map[string]any) (agenvo.Receipt, error) {
		if method != "workspace.create" || args["cwd"] != selected {
			t.Fatal("unexpected native input", method)
		}
		creates++
		return agenvo.Receipt{}, agenvo.ErrUnknown
	})
	in := Submit{Kind: "herdr_pi_task", Task: "synthetic default directory"}
	first, err := service.submit(ctx, f.binding, in)
	if !errors.Is(err, agenvo.ErrUnknown) || first.ID == "" || creates != 1 {
		t.Fatalf("expected one uncertain create: %+v %v", first, err)
	}
	before, details, err := service.load(ctx, f.binding, first.ID)
	if err != nil || details.DefaultWorkDir != selected || details.Batch.Progress.RequestHash != before.Fingerprint || details.TargetSnapshot == nil || agenvo.TargetFingerprint(*details.TargetSnapshot) != before.TargetFingerprint || details.TargetSnapshot.Producer != f.profile.Producer {
		t.Fatal("resolved directory was not retained with original request identity", err)
	}
	selected = `C:\Synthetic\later`
	in.ResumeOperationID = first.ID
	resumed, err := service.submit(ctx, f.binding, in)
	// This fixture has no continuation-admission store. Reaching that existing
	// denial proves the restored request passed the original fingerprint check;
	// it is not evidence that a native resume or a second create succeeded.
	if !errors.Is(err, ErrDenied) || resumed.ID != first.ID || defaults != 1 || creates != 1 {
		t.Fatalf("resume used a new default or crossed admission: %+v %v", resumed, err)
	}
	after, _, err := service.load(ctx, f.binding, first.ID)
	if err != nil || !reflect.DeepEqual(before, after) || f.batch.runs != 0 {
		t.Fatal("denied continuation changed original operation or replayed Pi", err)
	}
	details.TargetSnapshot.WorkDir = `C:\Synthetic\different-root`
	after.Details, err = json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	f.store.records[after.ID] = after // controlled corruption, never a real row
	if _, _, err = service.load(ctx, f.binding, after.ID); !errors.Is(err, agenvo.ErrUnknown) {
		t.Fatal("changed root snapshot was accepted against original target fingerprint", err)
	}
}
