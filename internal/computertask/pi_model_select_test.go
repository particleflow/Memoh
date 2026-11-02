package computertask

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

// testPiModels is the synthetic live catalog for fixtures. Its ids are fixture
// data, not any real catalog; membership is what the gate checks.
func testPiModels() []PiModel {
	return []PiModel{
		{ID: "synthetic-model", Kind: "chat", Input: []string{"text"}, Output: []string{"text"}},
		{ID: "vendor/second-model"},
	}
}

func testPiModelList(context.Context) ([]PiModel, error) { return testPiModels(), nil }

// TestPiModelSchemaHasNoFrozenChoices: the model tool parameter is a free id
// validated against the live catalog; it must not carry an enum, default or
// any other frozen choice list.
func TestPiModelSchemaHasNoFrozenChoices(t *testing.T) {
	tools, err := (&Source{service: &Service{}}).ListTools(t.Context(), mcp.ToolSessionContext{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Name != "computer_task_submit" {
			continue
		}
		schema := tool.InputSchema
		properties, _ := schema["properties"].(map[string]any)
		model, _ := properties["model"].(map[string]any)
		if model == nil || model["type"] != "string" {
			t.Fatalf("model parameter missing or not a string: %+v", properties["model"])
		}
		for _, frozen := range []string{"enum", "default", "examples", "anyOf", "oneOf"} {
			if _, exists := model[frozen]; exists {
				t.Fatalf("model parameter carries frozen choice list %q", frozen)
			}
		}
		return
	}
	t.Fatal("computer_task_submit tool not found")
}

// scriptModelValue decodes the id the generated batch script feeds to Pi's
// --model channel: the memoh_model shellData chunk.
func scriptModelValue(t *testing.T, script string) string {
	t.Helper()
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		const prefix = `memoh_model=$(printf '%s' '`
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		rest := strings.TrimPrefix(trimmed, prefix)
		end := strings.Index(rest, `' | base64 --decode`)
		if end < 0 {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(rest[:end])
		if err != nil {
			t.Fatalf("model shellData chunk: %v", err)
		}
		return string(raw)
	}
	t.Fatal("generated script carries no memoh_model value")
	return ""
}

// TestPiModelSelectionCarriesChosenModelIntoBatchScript: a submit whose model
// is in the live catalog dispatches, and only the chosen id reaches --model.
func TestPiModelSelectionCarriesChosenModelIntoBatchScript(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	service = service.WithPiModelList(testPiModelList)
	r, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "compute 42", Model: "vendor/second-model"})
	if err != nil || r.State == "rejected" || f.batch.runs != 1 {
		t.Fatalf("dispatch failed: %+v %v runs=%d", r, err, f.batch.runs)
	}
	if !strings.Contains(f.batch.lastScript, `pi --mode json --provider "$memoh_provider" --model "$memoh_model"`) {
		t.Fatal("script lost the fixed Pi --model channel")
	}
	if model := scriptModelValue(t, f.batch.lastScript); model != "vendor/second-model" {
		t.Fatalf("script model = %q, want the per-request choice", model)
	}
}

// TestPiModelSelectionRejectsMissingOrStaleModelWithLiveList: no model, or a
// model absent from the current catalog, must not dispatch and must return the
// current catalog; the server selects nothing on the caller's behalf.
func TestPiModelSelectionRejectsMissingOrStaleModelWithLiveList(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	service = service.WithPiModelList(testPiModelList)
	for name, model := range map[string]string{"missing": "", "stale": "gone/from-catalog"} {
		r, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "compute 42", Model: model})
		var selection *ModelSelectionError
		if !errors.As(err, &selection) || r.ID != "" || f.batch.runs != 0 || len(f.store.records) != 0 {
			t.Fatalf("%s: want selection error before claim/dispatch: %+v %v runs=%d records=%d", name, r, err, f.batch.runs, len(f.store.records))
		}
		want := testPiModels()
		if len(selection.Models) != len(want) || selection.Models[0].ID != want[0].ID || selection.Models[1].ID != want[1].ID {
			t.Fatalf("%s: error catalog = %+v, want the live list %+v", name, selection.Models, want)
		}
	}
	// foreground_bash accepts no model at all.
	if _, err := service.submit(ctx, f.binding, Submit{Kind: "foreground_bash", Task: "printf 1", Model: "synthetic-model"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreground model not rejected: %v", err)
	}
}

// TestPiModelSelectionFollowsCatalogAtDispatchTime: the catalog read at submit
// time governs; a later catalog change moves the accepted set with it.
func TestPiModelSelectionFollowsCatalogAtDispatchTime(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	catalog := []PiModel{{ID: "first-era/model"}}
	service = service.WithPiModelList(func(context.Context) ([]PiModel, error) { return catalog, nil })
	if _, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "era one", Model: "first-era/model"}); err != nil || f.batch.runs != 1 {
		t.Fatalf("first-era dispatch failed: %v runs=%d", err, f.batch.runs)
	}
	catalog = []PiModel{{ID: "second-era/model"}}
	// A later conversation in the same deployment: the unresolved first-era
	// operation must not influence the second era's model gate either way.
	later := f.binding
	later.SessionID, later.RunID = "second-era-session", "second-era-run"
	ctx2, err := toolcontext.WithComputerBinding(t.Context(), later)
	if err != nil {
		t.Fatal(err)
	}
	var selection *ModelSelectionError
	if _, err := service.submit(ctx2, later, Submit{Kind: "herdr_pi_task", Task: "era two", Model: "first-era/model"}); !errors.As(err, &selection) || selection.Models[0].ID != "second-era/model" {
		t.Fatalf("stale-era model accepted: %v", err)
	}
	if _, err := service.submit(ctx2, later, Submit{Kind: "herdr_pi_task", Task: "era two", Model: "second-era/model"}); err != nil || f.batch.runs != 2 {
		t.Fatalf("second-era dispatch failed: %v runs=%d", err, f.batch.runs)
	}
}

// TestPiModelSelectionSurfacesListOnToolErrorFace: the computer_task_submit
// tool response carries the live catalog on the selection error face, and the
// computer_targets listing exposes the same live catalog.
func TestPiModelSelectionSurfacesListOnToolErrorFace(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	service = service.WithPiModelList(testPiModelList)
	source := NewSource(service)
	identity := mcp.ToolSessionContext{BotID: f.binding.BotID, UserID: f.binding.UserID, SessionID: f.binding.SessionID, RunID: f.binding.RunID, RunContext: ctx}
	bound, err := toolcontext.WithComputerBinding(ctx, f.binding)
	if err != nil {
		t.Fatal(err)
	}
	out, err := source.CallTool(bound, identity, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"})
	if err != nil {
		t.Fatalf("tool error face must be a structured response: %v", err)
	}
	if out["isError"] != true {
		t.Fatalf("selection error face is not an error: %v", out)
	}
	face, _ := out["structuredContent"].(map[string]any)
	if face["code"] != "computer.model_selection_required" || face["operation_created"] != false || face["business_dispatched"] != false {
		t.Fatalf("wrong selection error face: %v", face)
	}
	models, _ := face["available_models"].([]PiModel)
	if len(models) != 2 || models[0].ID != "synthetic-model" || models[1].ID != "vendor/second-model" {
		t.Fatalf("error face catalog = %+v", face["available_models"])
	}
	// The same live catalog is visible on the target listing before dispatch.
	listed, err := source.CallTool(bound, identity, "computer_targets", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	listFace, _ := listed["structuredContent"].(map[string]any)
	computer, _ := listFace["computer"].([]Target)
	if len(computer) != 1 || len(computer[0].AvailableModels) != 2 || computer[0].AvailableModels[1].ID != "vendor/second-model" {
		t.Fatalf("target listing omitted the live catalog: %+v", listFace["computer"])
	}
}
