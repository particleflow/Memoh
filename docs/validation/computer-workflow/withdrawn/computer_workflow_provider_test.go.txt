package contextview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	contextfrag "github.com/felinics/memoh/internal/agent/context/fragment"
	agentpkg "github.com/felinics/memoh/internal/agent/runtime/native"
	agenttools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/skills"
	workspacepkg "github.com/felinics/memoh/internal/workspace"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

// Only the bridge/target service is synthetic; ContainerProvider registers its
// real multi-tool set, including apply_patch before list_execution_locations.
type routingTargetFixture struct{}

func (routingTargetFixture) MCPClient(context.Context, string) (*bridge.Client, error) {
	return nil, errors.New("fixture does not execute workspace operations")
}

func (routingTargetFixture) WorkspaceInfo(context.Context, string) (bridge.WorkspaceInfo, error) {
	return bridge.WorkspaceInfo{}, nil
}

func (routingTargetFixture) ResolveWorkspaceTarget(context.Context, string, string) (workspacepkg.ResolvedWorkspaceTarget, error) {
	return workspacepkg.ResolvedWorkspaceTarget{}, workspacepkg.ErrWorkspaceTargetNotFound
}

func (routingTargetFixture) ListWorkspaceTargets(context.Context, string) ([]workspacepkg.WorkspaceTarget, error) {
	return nil, nil
}

func TestComputerRoutingRequiredTracksDiscoveryCapability(t *testing.T) {
	const requiredID = "system.tool_usage.required.apply_patch"
	var captured agentpkg.RunConfig
	applier := ProviderRunConfigApplier(nil)
	agent := agentpkg.New(agentpkg.Deps{ContextViewApplier: func(ctx context.Context, cfg agentpkg.RunConfig) (agentpkg.RunConfig, error) {
		// The application supplies authoritative typed sources for capability
		// revocation; the legacy flattened-input path has no roster gate.
		cfg.ContextSourceFrags = CollectProviderSourceFrags(ctx, cfg)
		captured = cfg
		return applier(ctx, cfg)
	}})
	agent.SetToolProviders([]agenttools.ToolProvider{agenttools.NewContainerProvider(nil, routingTargetFixture{}, nil, "")})
	provider := &envelopeProbeProvider{handler: func(_ int, req sdk.Request) (sdk.ModelResult, error) {
		if !strings.Contains(req.System, "Computer routing contract") {
			t.Fatal("available discovery lost required instructions")
		}
		return sdk.ModelResult{Text: "fixture", FinishReason: sdk.FinishReasonStop}, nil
	}}
	_, err := agent.Generate(t.Context(), agentpkg.RunConfig{Model: &sdk.Model{ID: "synthetic", Provider: provider, Type: sdk.ModelTypeChat}, SupportsToolCall: true, ContextBudgetMaxTokens: 32768, Messages: []sdk.Message{sdk.UserMessage("Use my PC")}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, frag := range captured.ContextToolUsageFrags {
		if frag.ID == requiredID {
			found = true
			if frag.RequiredCapability != "list_execution_locations" || frag.RetentionTier != contextfrag.RetentionRequired {
				t.Fatalf("wrong capability or retention: %+v", frag)
			}
		}
	}
	if !found {
		t.Fatal("real multi-tool Container contract not assembled")
	}
	// Recompose after discovery is removed, while apply_patch and the rest of
	// the real tool set remain. A required tier must not defeat this gate.
	defs := make([]contextfrag.ToolDefAccounting, 0, len(captured.ContextToolDefs))
	for _, def := range captured.ContextToolDefs {
		if def.Name != "list_execution_locations" {
			defs = append(defs, def)
		}
	}
	if len(defs) != len(captured.ContextToolDefs)-1 {
		t.Fatal("fixture did not revoke exactly the discovery capability")
	}
	captured.ContextToolDefs = defs
	got, err := applier(t.Context(), captured)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.System, "Computer routing contract") || !hasFragID(got.ContextFrags, "system.tool_usage.apply_patch") {
		t.Fatalf("revocation: required_present=%v selected=%v decisions=%+v", strings.Contains(got.System, "Computer routing contract"), fragIDs(got.ContextFrags), got.ContextManifest.SelectionDecisions)
	}
	if decision, ok := decisionByID(got.ContextManifest.SelectionDecisions, requiredID); !ok || decision.Reason != capabilityGateDropReason {
		t.Fatalf("required fragment was not capability-gated: %+v", decision)
	}
}

// Run the actual native assembler, initial composer, step reselection and
// provider serialization. Only the model and read-only external operations
// are synthetic. Padding models the other system/tool definitions in the
// captured 32K application reproduction without publishing its payload.
type computerWorkflowProvider struct {
	*agenttools.ContainerProvider
	baseline, minimal bool
	observations      int
}

func (p *computerWorkflowProvider) RequiredUsage(ctx context.Context, session agenttools.SessionContext, available agenttools.AvailableTools) (agenttools.ToolName, string) {
	if p.baseline {
		return agenttools.ToolName{}, ""
	}
	return p.ContainerProvider.RequiredUsage(ctx, session, available)
}

func (p *computerWorkflowProvider) Usage(ctx context.Context, session agenttools.SessionContext, available agenttools.AvailableTools) string {
	usage := p.ContainerProvider.Usage(ctx, session, available)
	if p.minimal {
		usage += "\n" + strings.Repeat("Ordinary preferred fixture documentation. ", 200)
	}
	return usage
}

func (p *computerWorkflowProvider) Tools(ctx context.Context, session agenttools.SessionContext) ([]toolexec.Tool, error) {
	list := toolexec.Tool{Name: "list_execution_locations", Description: "Discover authorized targets.", Parameters: toolexec.SchemaFromValue(map[string]any{"type": "object"}), Execute: func(*toolexec.ToolExecContext, sdk.ToolArguments) (sdk.ToolOutput, error) {
		return toolexec.OutputFromValue(map[string]any{"locations": []any{map[string]any{"target_id": "synthetic-target", "provider": "agenvo", "capabilities": []string{"exec_foreground"}, "available": true}}}), nil
	}}
	if p.minimal {
		return []toolexec.Tool{list}, nil
	}
	base, err := p.ContainerProvider.Tools(ctx, session)
	if err != nil {
		return nil, err
	}
	observe := toolexec.Tool{Name: "synthetic_agent_observe", Description: "Read-only agent.get/agent.read fixture. " + strings.Repeat("d", 44000), Parameters: toolexec.SchemaFromValue(map[string]any{"type": "object"}), Execute: func(*toolexec.ToolExecContext, sdk.ToolArguments) (sdk.ToolOutput, error) {
		p.observations++
		return toolexec.OutputFromValue(map[string]any{"state": "unknown", "output": strings.Repeat("synthetic observation ", 100)}), nil
	}}
	return append(append(base, list), observe), nil
}

func computerSkillResult(req sdk.Request) string {
	for _, msg := range req.Messages {
		for _, part := range msg.Content {
			if result, ok := part.(sdk.ToolResultPart); ok && result.ToolName == "use_skill" {
				return result.Result.String()
			}
		}
	}
	return ""
}

func TestComputerWorkflowProviderRequests(t *testing.T) {
	for _, tc := range []struct {
		name                string
		baseline            bool
		window, systemBytes int
	}{
		{"32K original pruning", true, 32768, 18000},
		{"32K compact and required routing", false, 32768, 18000},
		{"64K reasoning and 110 synthetic messages", false, 65536, 21767},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := tc.baseline
			file := "../../contrib/bot-skills/windows-computer/SKILL.md"
			if baseline {
				file = "testdata/computer-workflow/56a4d08f-SKILL.md"
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			skill := skills.ParseFile(string(raw), "")
			tools := &computerWorkflowProvider{ContainerProvider: agenttools.NewContainerProvider(nil, nil, nil, ""), baseline: baseline}
			pruned := false
			provider := &envelopeProbeProvider{handler: func(call int, req sdk.Request) (sdk.ModelResult, error) {
				if !baseline {
					for _, text := range []string{"## Tool usage", "Computer routing contract", "Unknown submission: reconcile read-only", "missing or pruned", "do not guess", "Keep Server Workspace tasks there"} {
						if !strings.Contains(req.System, text) {
							t.Fatalf("request %d missing system contract %q", call, text)
						}
					}
				}
				result := computerSkillResult(req)
				if strings.Contains(result, "[tool result pruned:") || call > 2 && result == "" {
					pruned = true
				}
				if call == 2 {
					if baseline && !pruned {
						t.Fatal("original skill no longer reproduces pruning")
					}
					if !baseline && !strings.Contains(result, skill.Content) { // JSON escapes are inspected through OutputValue below.
						found := false
						for _, m := range req.Messages {
							for _, part := range m.Content {
								if r, ok := part.(sdk.ToolResultPart); ok && r.ToolName == "use_skill" {
									if v, ok := toolexec.OutputValue(r.Result).(map[string]any); ok {
										found = v["content"] == skill.Content
									}
								}
							}
						}
						if !found {
							t.Fatal("compact skill missing from first effective provider request")
						}
					}
				}
				t.Logf("request=%d system=%d tools=%d messages=%d total_input=%d skill_pruned=%v", call, contextfrag.ProviderEnvelopeTokens(req.System, nil, nil), contextfrag.ProviderEnvelopeTokens("", nil, req.Tools), contextfrag.ProviderEnvelopeTokens("", req.Messages, nil), contextfrag.ProviderEnvelopeTokens(req.System, req.Messages, req.Tools), pruned)
				if baseline && call == 2 || call == 7 {
					return sdk.ModelResult{Text: "Missing detailed instructions: stop mutations; reconcile read-only.", FinishReason: sdk.FinishReasonStop}, nil
				}
				name, args := "synthetic_agent_observe", map[string]any{}
				if call == 1 {
					name = "use_skill"
					args = map[string]any{"skillName": skill.Name, "reason": "Use the connected PC"}
				}
				if call == 2 {
					name = "list_execution_locations"
				}
				return sdk.ModelResult{FinishReason: sdk.FinishReasonToolCalls, ToolCalls: []sdk.ToolCall{{ToolCallID: fmt.Sprintf("call-%d", call), ToolName: name, Input: toolexec.ArgumentsFromValue(args)}}}, nil
			}}
			agent := agentpkg.New(agentpkg.Deps{ContextViewApplier: ProviderRunConfigApplier(nil)})
			agent.SetToolProviders([]agenttools.ToolProvider{tools, agenttools.NewSkillProvider(nil)})
			lifecycle := contextfrag.NewLifecycleHolder()
			index := 0
			messages := []sdk.Message{sdk.UserMessage("Use my connected PC; do not replay unknown work.")}
			var reasoning *models.ReasoningConfig
			if tc.window == 65536 {
				reasoning = &models.ReasoningConfig{Active: true, Effort: "low"}
				history := make([]sdk.Message, 0, 110)
				for i := range 109 {
					size := 380
					if i == 0 {
						size = 876
					}
					msg := sdk.UserMessage(strings.Repeat("h", size))
					if i%2 == 1 {
						msg = sdk.AssistantMessage(strings.Repeat("h", size))
					}
					history = append(history, msg)
				}
				messages = append(history, messages...)
				index = 109
			}
			_, err = agent.Generate(t.Context(), agentpkg.RunConfig{
				Model: &sdk.Model{ID: "synthetic", Provider: provider, Type: sdk.ModelTypeChat}, System: strings.Repeat("s", tc.systemBytes), Messages: messages,
				ContextCurrentUserMessageIndex: &index, ContextBudgetMaxTokens: tc.window, SupportsToolCall: true, ContextLifecycle: lifecycle, ReasoningConfig: reasoning,
				Skills: []agentpkg.SkillEntry{{Name: skill.Name, Description: skill.Description, Content: skill.Content, Path: "/data/skills/windows-computer/SKILL.md", Metadata: map[string]any{"priority": "required", "protected": true}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			snapshot, _ := lifecycle.Snapshot()
			t.Logf("budget=%+v", snapshot.BudgetPlan)
			if tc.window == 65536 && (snapshot.BudgetPlan.OutputReserve != 16384 || snapshot.BudgetPlan.OutputReserveResolution != "window_clamped") {
				t.Fatalf("wrong 64K reserve: %+v", snapshot.BudgetPlan)
			}
			if !pruned {
				t.Fatal("test did not exercise tool-result pruning")
			}
			if !baseline && tools.observations != 4 {
				t.Fatalf("read-only observation count=%d", tools.observations)
			}
		})
	}
}

func TestComputerRoutingRequiredAcrossComposition(t *testing.T) {
	for _, typed := range []bool{false, true} {
		for _, window := range []int{2000, 900} {
			t.Run(strconv.Itoa(window)+"/typed="+strconv.FormatBool(typed), func(t *testing.T) {
				provider := &envelopeProbeProvider{handler: func(_ int, req sdk.Request) (sdk.ModelResult, error) {
					if !strings.Contains(req.System, "Computer routing contract") {
						t.Fatal("required routing dropped at compose")
					}
					if strings.Contains(req.System, "### Basic Tools") {
						t.Fatal("fixture did not pressure preferred usage")
					}
					return sdk.ModelResult{Text: "known contract", FinishReason: sdk.FinishReasonStop}, nil
				}}
				agent := agentpkg.New(agentpkg.Deps{ContextViewApplier: ProviderRunConfigApplier(nil)})
				agent.SetToolProviders([]agenttools.ToolProvider{&computerWorkflowProvider{ContainerProvider: agenttools.NewContainerProvider(nil, nil, nil, ""), minimal: true}})
				// Compose twice, as separate turns do; a previously rendered system is
				// not reused to hide a lost required fragment on re-composition.
				for range 2 {
					cfg := agentpkg.RunConfig{Model: &sdk.Model{ID: "synthetic", Provider: provider, Type: sdk.ModelTypeChat}, System: strings.Repeat("s", 1200), Messages: []sdk.Message{sdk.UserMessage("Use my PC")}, ContextBudgetMaxTokens: window, SupportsToolCall: true}
					if typed {
						cfg.ContextSourceFrags = CollectProviderSourceFrags(t.Context(), cfg)
					}
					_, err := agent.Generate(t.Context(), cfg)
					if window == 900 {
						if !errors.Is(err, contextfrag.ErrProtectedContextOverflow) {
							t.Fatalf("overflow error=%v", err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
				}
				if window == 900 && provider.calls.Load() != 0 {
					t.Fatal("oversized required rules reached provider")
				}
			})
		}
	}
}
