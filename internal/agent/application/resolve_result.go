package application

import (
	"github.com/felinics/memoh/internal/agent/runtime/native"
	"github.com/felinics/memoh/internal/reasoning"
)

// ResolveRunConfigResult holds a fully resolved run configuration for one
// agent request, together with the selected model and session runtime.
// Produced by application.Service.ResolveRunConfig and consumed by the turn
// runtime adapters.
type ResolveRunConfigResult struct {
	RunConfig              native.RunConfig
	ModelID                string // database UUID of the selected model
	RuntimeType            string
	ContextBudgetMaxTokens int
	// ReasoningTierDecision records the per-turn effort tiering outcome for
	// accounting on the turn's assistant rows; nil when no tiering applied.
	ReasoningTierDecision *reasoning.TierDecision
}
