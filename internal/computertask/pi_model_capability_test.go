package computertask

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/computertarget"
)

func capabilityModels() []PiModel {
	window, maxOut := 32000, 4096
	return []PiModel{
		{ID: "small/model", Kind: "chat", Input: []string{"text"}, Output: []string{"text"}, ContextWindow: &window, MaxOutput: &maxOut},
		{ID: "unstated/model", Kind: "chat"},
	}
}

// TestPiModelCapabilityReachesPerTaskPiConfiguration: the capability the live
// catalog stated for the chosen model must reach the endpoint-side Pi
// configuration, and the listing faces must not expose it.
func TestPiModelCapabilityReachesPerTaskPiConfiguration(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	service = service.WithPiModelList(func(context.Context) ([]PiModel, error) { return capabilityModels(), nil })
	r, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "compute 42", Model: "small/model"})
	if err != nil || f.batch.runs != 1 {
		t.Fatalf("dispatch failed: %+v %v runs=%d", r, err, f.batch.runs)
	}
	script := f.batch.lastScript
	if !strings.Contains(script, `export PI_CODING_AGENT_DIR="$(cygpath -w "$memoh_agent_dir")"`) {
		t.Fatal("per-task Pi configuration directory was not selected")
	}
	var spec computertarget.BatchModel
	if raw := scriptModelConfig(t, script); json.Unmarshal(raw, &spec) != nil {
		t.Fatalf("capability spec: %s", raw)
	}
	if spec.ID != "small/model" || spec.Provider != "synthetic" || spec.ContextWindow == nil || *spec.ContextWindow != 32000 || spec.MaxTokens == nil || *spec.MaxTokens != 4096 {
		t.Fatalf("stated capability did not travel with the invocation: %+v", spec)
	}
	if len(spec.Input) != 1 || spec.Input[0] != "text" {
		t.Fatalf("modalities did not travel with the invocation: %+v", spec.Input)
	}
	// The durable operation keeps the same snapshot so a resume derives it again.
	stored := computertarget.BatchProgress{}
	for _, record := range f.store.records {
		var d Details
		if json.Unmarshal(record.Details, &d) != nil || d.Batch == nil {
			continue
		}
		stored = d.Batch.Progress
	}
	if stored.Model == nil || stored.Model.ContextWindow == nil || *stored.Model.ContextWindow != 32000 {
		t.Fatalf("capability was not durable: %+v", stored.Model)
	}
}

// TestPiModelCapabilityAbsentKeepsSharedConfiguration: a catalog entry that
// states no capability must not send a zero/partial snapshot; Pi keeps the
// deployment's own models.json, which is the pre-existing behavior.
func TestPiModelCapabilityAbsentKeepsSharedConfiguration(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	service = service.WithPiModelList(func(context.Context) ([]PiModel, error) { return capabilityModels(), nil })
	if _, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "compute 42", Model: "unstated/model"}); err != nil || f.batch.runs != 1 {
		t.Fatalf("dispatch failed: %v runs=%d", err, f.batch.runs)
	}
	if strings.Contains(f.batch.lastScript, "PI_CODING_AGENT_DIR") {
		t.Fatal("an entry without stated capability changed Pi's shared configuration")
	}
}

// scriptModelConfig decodes the capability spec the generated script writes for
// the endpoint-side derivation program.
func scriptModelConfig(t *testing.T, script string) []byte {
	t.Helper()
	const prefix = `printf '%s' '`
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) || !strings.Contains(trimmed, "model-config.json") {
			continue
		}
		rest := strings.TrimPrefix(trimmed, prefix)
		end := strings.Index(rest, `' | base64`)
		if end < 0 {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(rest[:end])
		if err != nil {
			t.Fatalf("capability spec payload: %v", err)
		}
		return raw
	}
	t.Fatal("generated script carries no capability spec")
	return nil
}
