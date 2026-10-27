package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

// The shared-source adapter does not turn a flat source_ref into a path relative
// to a selected child CWD. Keep that unsupported combination explicit before
// operation creation; only an artifact at the original configured root is valid.
func TestWorkspaceSourceCombinationKeepsArtifactRoot(t *testing.T) {
	const root = `\\synthetic-nas\workspace\computer`
	for _, test := range []struct {
		name, selected, requested string
		allowed                   bool
	}{
		{name: "configured-root", selected: ".", allowed: true},
		{name: "selected-child", selected: root + `\project`},
		{name: "explicit-child", requested: root + `\project`},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, service, ctx := newNativeTaskFixture(t)
			f.profile.WorkDir = root
			mount := f.store.mounts[f.binding.TargetID]
			var err error
			mount.RuntimeProviderConfig, err = json.Marshal(map[string]any{"herdr": f.profile})
			if err != nil {
				t.Fatal(err)
			}
			f.store.mounts[f.binding.TargetID] = mount
			service.WithDefaultWorkDir(func(context.Context, toolcontext.ComputerBinding, string, agenvo.Target) (string, error) {
				return test.selected, nil
			})
			creates := 0
			service.relay.SetNativeCaller(func(_ context.Context, _ agenvo.Scope, target agenvo.Target, method string, args map[string]any) (agenvo.Receipt, error) {
				if !test.allowed || method != "workspace.create" || args["cwd"] != root || target.WorkDir != root || target.Producer != f.profile.Producer {
					t.Fatal("unexpected dispatch or changed task/producer root")
				}
				creates++
				return agenvo.Receipt{}, agenvo.ErrUnknown
			})
			result, err := service.submit(ctx, f.binding, Submit{
				Kind: "herdr_pi_task", Task: "synthetic root-bound artifact", WorkDir: test.requested,
				Artifact: &deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"},
			})
			if !test.allowed {
				if !errors.Is(err, ErrInvalid) || result.ID != "" || creates != 0 || len(f.store.records) != 0 {
					t.Fatal("child CWD was silently interpreted as the producer root", err)
				}
				return
			}
			if !errors.Is(err, agenvo.ErrUnknown) || result.ID == "" || creates != 1 {
				t.Fatal("expected one uncertain root workspace creation", err)
			}
			_, details, err := service.load(ctx, f.binding, result.ID)
			if err != nil || details.TargetSnapshot == nil || details.TargetSnapshot.WorkDir != root || details.TargetSnapshot.Producer != f.profile.Producer || details.Batch.Progress.Producer.Task.RootHash != deliverymeta.RootHash(root) || details.Batch.Progress.Producer.Artifact.SourceRef != "result" || f.batch.runs != 0 {
				t.Fatal("original root, source reference or producer snapshot changed", err)
			}
		})
	}
}
