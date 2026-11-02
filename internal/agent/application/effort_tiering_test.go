package application

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agent/sessionmode"
	sessionpkg "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/reasoning"
)

var tierGLMEfforts = []string{reasoning.EffortLow, reasoning.EffortHigh, reasoning.EffortXHigh}

func activeConfig(t *testing.T, effort string) *models.ReasoningConfig {
	t.Helper()
	return &models.ReasoningConfig{Active: true, Effort: effort}
}

// The decision table from the design: first match wins, every bucket is decided
// from signals that exist before the provider request, and the resolved effort
// (the cap) is never raised.
func TestApplyEffortTieringDecisionTable(t *testing.T) {
	cases := []struct {
		name string
		in   effortTierInputs
		cap  string
		// wantSource "" asserts no auto decision at all.
		wantSource  string
		wantApplied string
	}{
		{
			name: "switch off keeps resolved effort",
			in:   effortTierInputs{Enabled: false, SessionType: "chat", InputText: "帮我把电脑里下载文件夹中的文件发给我"},
			cap:  reasoning.EffortXHigh, wantSource: "",
		},
		{
			name: "inactive reasoning is untouched",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: "hi"},
			cap:  "", wantSource: "",
		},
		{
			name: "explicit per-message effort wins",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: "随便聊聊", RequestedEffort: reasoning.EffortXHigh},
			cap:  reasoning.EffortXHigh, wantSource: "",
		},
		{
			name: "session-remembered effort wins",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: "随便聊聊", SessionEffort: reasoning.EffortMax},
			cap:  reasoning.EffortMax, wantSource: "",
		},
		{
			name: "schedule turns always low",
			in:   effortTierInputs{Enabled: true, SessionType: sessionmode.Schedule, InputText: "每天早上发一份天气摘要，要求内容详尽全面"},
			cap:  reasoning.EffortXHigh, wantSource: tierSourceScheduleSubagent, wantApplied: reasoning.EffortLow,
		},
		{
			name: "subagent turns always low",
			in:   effortTierInputs{Enabled: true, SessionType: sessionpkg.TypeSubagent, InputText: "深入分析所有文件并输出详尽报告"},
			cap:  reasoning.EffortXHigh, wantSource: tierSourceScheduleSubagent, wantApplied: reasoning.EffortLow,
		},
		{
			name: "short simple question goes low",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: "好的，用第二个方案"},
			cap:  reasoning.EffortXHigh, wantSource: tierSourceShortInput, wantApplied: reasoning.EffortLow,
		},
		{
			name: "short question with attachment keeps cap",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: "这是什么？", Attachments: 1},
			cap:  reasoning.EffortXHigh, wantSource: tierSourceComplex, wantApplied: reasoning.EffortXHigh,
		},
		{
			name: "explicit thoroughness keeps cap",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: "请仔细对比这两份合同的每一条条款，给出彻底的风险分析"},
			cap:  reasoning.EffortXHigh, wantSource: tierSourceComplex, wantApplied: reasoning.EffortXHigh,
		},
		{
			name: "long task input keeps cap",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: strings.Repeat("请整理这段材料并归纳要点。", 60)},
			cap:  reasoning.EffortXHigh, wantSource: tierSourceComplex, wantApplied: reasoning.EffortXHigh,
		},
		{
			name: "ordinary chat defaults to high under xhigh cap",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: "帮我把电脑里下载文件夹中秋秋唱的江湖梦发给我"},
			cap:  reasoning.EffortXHigh, wantSource: tierSourceChatDefault, wantApplied: reasoning.EffortHigh,
		},
		{
			name: "user set low means everything low",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: "帮我把电脑里下载文件夹中秋秋唱的江湖梦发给我"},
			cap:  reasoning.EffortLow, wantSource: tierSourceChatDefault, wantApplied: reasoning.EffortLow,
		},
		{
			name: "high cap never escalates to xhigh",
			in:   effortTierInputs{Enabled: true, SessionType: "chat", InputText: "请仔细对比这两份合同的每一条条款"},
			cap:  reasoning.EffortHigh, wantSource: tierSourceComplex, wantApplied: reasoning.EffortHigh,
		},
		{
			name: "resumed turns without input default to high",
			in:   effortTierInputs{Enabled: true, SessionType: "chat"},
			cap:  reasoning.EffortXHigh, wantSource: tierSourceChatDefault, wantApplied: reasoning.EffortHigh,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := activeConfig(t, tc.cap)
			got := applyEffortTiering(cfg, tierGLMEfforts, tc.in)
			if tc.wantSource == "" {
				if got != nil {
					t.Fatalf("applyEffortTiering returned %+v, want no decision", got)
				}
				if cfg.Effort != tc.cap {
					t.Fatalf("effort changed to %q, want untouched %q", cfg.Effort, tc.cap)
				}
				return
			}
			if got == nil {
				t.Fatalf("applyEffortTiering returned no decision, want source %q", tc.wantSource)
			}
			if got.Source != tc.wantSource {
				t.Errorf("source = %q, want %q", got.Source, tc.wantSource)
			}
			if got.Applied != tc.wantApplied {
				t.Errorf("applied = %q, want %q", got.Applied, tc.wantApplied)
			}
			if got.Cap != tc.cap {
				t.Errorf("cap = %q, want %q", got.Cap, tc.cap)
			}
			if cfg.Effort != tc.wantApplied {
				t.Errorf("cfg.Effort = %q, want %q", cfg.Effort, tc.wantApplied)
			}
		})
	}
}

// With the switch off the function must leave every input combination exactly
// as the ordinary resolution chain produced it.
func TestApplyEffortTieringOffMatchesBaseline(t *testing.T) {
	inputs := []effortTierInputs{
		{SessionType: "chat", InputText: "好的"},
		{SessionType: sessionmode.Schedule, InputText: "run the report"},
		{SessionType: sessionpkg.TypeSubagent, InputText: "deep analysis"},
		{SessionType: "chat", InputText: "请仔细分析", Attachments: 2},
		{SessionType: "chat"},
	}
	for _, cap := range tierGLMEfforts {
		for _, in := range inputs {
			in.Enabled = false
			cfg := activeConfig(t, cap)
			if got := applyEffortTiering(cfg, tierGLMEfforts, in); got != nil {
				t.Errorf("disabled tiering produced decision %+v for %q", got, in.InputText)
			}
			if cfg.Effort != cap {
				t.Errorf("disabled tiering changed effort %q -> %q", cap, cfg.Effort)
			}
		}
	}
}

// Toggle models ("enabled") cannot be tiered; the resolved decision must pass
// through untouched.
func TestApplyEffortTieringToggleModelUntouched(t *testing.T) {
	cfg := &models.ReasoningConfig{Active: true, Effort: reasoning.EffortEnabled}
	if got := applyEffortTiering(cfg, []string{reasoning.EffortEnabled}, effortTierInputs{
		Enabled: true, SessionType: "chat", InputText: "好的",
	}); got != nil {
		t.Fatalf("toggle model tiered: %+v", got)
	}
	if cfg.Effort != reasoning.EffortEnabled {
		t.Fatalf("toggle effort changed to %q", cfg.Effort)
	}
}

// A tier the model does not advertise downgrades to the weakest level the
// model can express below the cap, never to an invalid value.
func TestApplyEffortTieringUnadvertisedLow(t *testing.T) {
	cfg := activeConfig(t, reasoning.EffortHigh)
	got := applyEffortTiering(cfg, []string{reasoning.EffortMedium, reasoning.EffortHigh}, effortTierInputs{
		Enabled: true, SessionType: "chat", InputText: "好的",
	})
	if got == nil {
		t.Fatal("want a decision for a short input on a medium/high model")
	}
	if got.Applied != reasoning.EffortMedium || cfg.Effort != reasoning.EffortMedium {
		t.Fatalf("applied = %q, cfg.Effort = %q, want medium", got.Applied, cfg.Effort)
	}
}

// The applied tier must reach the persisted assistant rows so acceptance can
// be measured from history alone.
func TestStoreRoundOptionsReasoningEffortMetadata(t *testing.T) {
	messages := []ModelMessage{
		{Role: "user", Content: json.RawMessage(`"hi"`)},
		{Role: "assistant", Content: json.RawMessage(`"hello"`)},
		{Role: "assistant", Content: json.RawMessage(`"more"`)},
	}
	opts := storeRoundOptions{
		ReasoningEffortTier: &reasoning.TierDecision{
			Applied: reasoning.EffortHigh, Cap: reasoning.EffortXHigh, Source: tierSourceChatDefault,
		},
	}
	stamped := opts.withReasoningEffortMetadata(messages)
	count := 0
	for index, meta := range stamped.MessageMetadataByIndex {
		value, ok := meta["reasoning_effort"].(reasoning.TierDecision)
		if !ok {
			t.Fatalf("metadata[%d][reasoning_effort] has type %T, want TierDecision", index, meta["reasoning_effort"])
		}
		if value.Applied != reasoning.EffortHigh || value.Cap != reasoning.EffortXHigh || value.Source != tierSourceChatDefault {
			t.Fatalf("metadata[%d] = %+v", index, value)
		}
		if messages[index].Role != "assistant" {
			t.Fatalf("metadata stamped on %q row", messages[index].Role)
		}
		count++
	}
	if count == 0 {
		t.Fatal("no assistant row carries the reasoning_effort metadata")
	}
	// A nil decision must not create any metadata map.
	empty := storeRoundOptions{}.withReasoningEffortMetadata(messages)
	if len(empty.MessageMetadataByIndex) != 0 {
		t.Fatalf("nil decision produced metadata %+v", empty.MessageMetadataByIndex)
	}
}
