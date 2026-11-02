package tools

import (
	"context"
	"testing"

	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/reasoning"
)

// Subagent spawns must run at low effort when tiering is enabled and the spawn
// carries no explicit requested tier, and must leave an explicit requested tier
// alone.
func TestResolveSubagentReasoningEffortTiering(t *testing.T) {
	model := models.GetResponse{}
	model.ID = "m1"
	model.Config.ThinkingMode = reasoning.ModeToggle
	model.Config.ReasoningEfforts = []string{reasoning.EffortLow, reasoning.EffortHigh, reasoning.EffortXHigh}

	session := SessionContext{BotID: "b1", ReasoningStoredEffort: reasoning.EffortXHigh}

	p := &SpawnProvider{}
	p.SetEffortTiering(true)
	cfg, err := p.resolveSubagentReasoning(context.Background(), session, model, "")
	if err != nil {
		t.Fatalf("resolveSubagentReasoning: %v", err)
	}
	if cfg == nil || !cfg.Active {
		t.Fatalf("want an active reasoning config, got %+v", cfg)
	}
	if cfg.Effort != reasoning.EffortLow {
		t.Fatalf("subagent effort = %q, want low", cfg.Effort)
	}

	session.ReasoningRequestedEffort = reasoning.EffortXHigh
	cfg, err = p.resolveSubagentReasoning(context.Background(), session, model, "")
	if err != nil {
		t.Fatalf("resolveSubagentReasoning: %v", err)
	}
	if cfg == nil || cfg.Effort != reasoning.EffortXHigh {
		t.Fatalf("explicit requested tier not honoured: %+v", cfg)
	}

	p.SetEffortTiering(false)
	session.ReasoningRequestedEffort = ""
	cfg, err = p.resolveSubagentReasoning(context.Background(), session, model, "")
	if err != nil {
		t.Fatalf("resolveSubagentReasoning: %v", err)
	}
	if cfg == nil || cfg.Effort != reasoning.EffortXHigh {
		t.Fatalf("tiering off must keep the stored effort, got %+v", cfg)
	}
}
