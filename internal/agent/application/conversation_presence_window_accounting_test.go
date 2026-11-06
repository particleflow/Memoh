package application

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/inputsource"
)

// TestPresenceOutcomePatchKeepsFullTimings pins the accounting contract of
// design v3 §6.3: detail.timings carries assemble/stage1/pull/stage2 and the
// new generate_ms, and the finish write point must not erase the decision
// timings it did not produce. The real 2026-11-02 08:05 turn shows why this
// matters: the row recorded outcome=expired with no generate_ms at all, so an
// entry-guard expiry and a generation that ran 9.17s into the 10s window were
// indistinguishable from the ledger.
func TestPresenceOutcomePatchKeepsFullTimings(t *testing.T) {
	detail := presenceDecisionDetail{
		Q1Attempts: 1,
		Timings: presenceDecisionTimings{
			AssembleMs: 9,
			Stage1Ms:   535,
			Stage2Ms:   274,
			GenerateMs: 9167,
		},
	}
	patch, err := presenceOutcomePatch(detail, "expired")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	var decoded struct {
		Outcome string           `json:"outcome"`
		Timings map[string]int64 `json:"timings"`
	}
	if err := json.Unmarshal(patch, &decoded); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if decoded.Outcome != "expired" {
		t.Fatalf("outcome=%q", decoded.Outcome)
	}
	for key, want := range map[string]int64{"assemble_ms": 9, "stage1_ms": 535, "stage2_ms": 274, "generate_ms": 9167} {
		if got := decoded.Timings[key]; got != want {
			t.Fatalf("timings.%s=%d want %d (patch %s)", key, got, want, patch)
		}
	}
}

// TestPresenceOutcomePatchOmitsGenerateWhenNoGeneration keeps the fallback and
// entry-guard paths honest: no generation attempt means no generate_ms claim.
func TestPresenceOutcomePatchOmitsGenerateWhenNoGeneration(t *testing.T) {
	detail := presenceDecisionDetail{Timings: presenceDecisionTimings{AssembleMs: 7, Stage1Ms: 500, Stage2Ms: 240}}
	patch, err := presenceOutcomePatch(detail, "expired")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	var decoded struct {
		Timings map[string]int64 `json:"timings"`
	}
	if err := json.Unmarshal(patch, &decoded); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if _, present := decoded.Timings["generate_ms"]; present {
		t.Fatalf("entry-guard expiry must not claim a generation: %s", patch)
	}
	if decoded.Timings["stage1_ms"] != 500 {
		t.Fatalf("timings lost: %s", patch)
	}
}

// TestPresenceExpiredGenerationRecordsItsWallTime is the design-correct answer
// to a slow acknowledgement: design v3 §4.4/§7 make the generation deadline the
// Jev window end, so a model call still running at the window end terminates as
// expired and is not retried or replaced by the constant fallback. What the
// ledger must not do is hide that a request was sent. The 2026-11-02 08:05 turn
// spent 9.17s in generation and recorded a bare outcome=expired.
func TestPresenceExpiredGenerationRecordsItsWallTime(t *testing.T) {
	detail := presenceDecisionDetail{Timings: presenceDecisionTimings{AssembleMs: 9, Stage1Ms: 535, Stage2Ms: 274}}
	slow := presenceTimedGenerator{detail: &detail, base: fixtureSlowGenerator{}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := slow.Generate(ctx, conversationpresence.Input{Phase: conversationpresence.Thinking}, conversationpresence.Limits{MaxInputRunes: 100, MaxOutputTokens: 64, MaxTextRunes: 160}); err == nil {
		t.Fatal("a generator that outlives its context must return the context error")
	}
	if detail.Timings.GenerateMs < 50 {
		t.Fatalf("generate_ms=%d did not record the attempt", detail.Timings.GenerateMs)
	}
	patch, err := presenceOutcomePatch(detail, "expired")
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Outcome string           `json:"outcome"`
		Timings map[string]int64 `json:"timings"`
	}
	if err := json.Unmarshal(patch, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Outcome != "expired" || decoded.Timings["generate_ms"] < 50 || decoded.Timings["stage1_ms"] != 535 {
		t.Fatalf("expired generation accounting lost data: %s", patch)
	}
}

type fixtureSlowGenerator struct{}

func (fixtureSlowGenerator) Generate(ctx context.Context, _ conversationpresence.Input, _ conversationpresence.Limits) (conversationpresence.Result, error) {
	<-ctx.Done()
	return conversationpresence.Result{}, ctx.Err()
}

// TestPresenceWindowRemainingAnchorsAtDecisionStart locks the effective window
// arithmetic: the Jev window opens at decision assembly, so a generation that
// receives the remainder is bounded by decisionStart+Timeout, not by the
// longer sidecar deadline.
func TestPresenceWindowRemainingAnchorsAtDecisionStart(t *testing.T) {
	now := time.Now()
	sideCtx, cancel := context.WithDeadline(context.Background(), now.Add(15*time.Second))
	defer cancel()
	decisionStart := now.Add(-3 * time.Second)
	remaining := presenceWindowRemaining(sideCtx, decisionStart, 10*time.Second)
	if remaining > 7*time.Second || remaining < 6*time.Second {
		t.Fatalf("remaining=%s want ~7s (10s window minus 3s elapsed)", remaining)
	}
	tightCtx, cancelTight := context.WithDeadline(context.Background(), now.Add(2*time.Second))
	defer cancelTight()
	if remaining := presenceWindowRemaining(tightCtx, now, 10*time.Second); remaining > 2*time.Second || remaining < time.Second {
		t.Fatalf("tight remaining=%s want ~2s", remaining)
	}
}

// TestPresenceEligibleAdmitsOnlyNewDirectInput pins the full pre-gate list from
// the 2026-11-02 field report. The report counted 26 message rows and only one
// presence run; the rows were one user message plus 25 assistant/tool rows, so
// the gate never dropped a user input. This test keeps the intended exclusions
// (retry/edit, resume, steered continuation) explicit instead of letting a
// future reader re-read the row count as a gate bug.
func TestPresenceEligibleAdmitsOnlyNewDirectInput(t *testing.T) {
	base := func() ChatRequest {
		return ChatRequest{
			BotID:       "bot",
			ThreadID:    "session",
			UserID:      "actor",
			RunHandle:   sessionruntime.RunHandle{BotID: "bot", SessionID: "session", RunID: "run", OwnerID: "owner", FencingToken: 1},
			SessionType: "chat",
		}
	}
	if !presenceEligible(base()) {
		t.Fatal("a new direct user turn must be eligible")
	}
	cases := []struct {
		name string
		mut  func(*ChatRequest)
	}{
		{"retry_or_edit_replacement", func(r *ChatRequest) { r.TurnReplacement = &message.TurnReplacement{} }},
		{"shutdown_resume", func(r *ChatRequest) { r.ShutdownResume = true }},
		{"reuse_persisted_user_message", func(r *ChatRequest) { r.ReusePersistedUserMessage = true }},
		{"skip_history_turn", func(r *ChatRequest) { r.SkipHistoryTurn = true }},
		{"schedule_turn", func(r *ChatRequest) { r.SessionType = "schedule" }},
		{"subagent_turn", func(r *ChatRequest) { r.SessionType = "subagent" }},
		{"machine_input_source", func(r *ChatRequest) { r.InputSource = &inputsource.Source{Kind: inputsource.Result} }},
		{"missing_fence", func(r *ChatRequest) { r.RunHandle.FencingToken = 0 }},
		{"missing_owner", func(r *ChatRequest) { r.RunHandle.OwnerID = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base()
			tc.mut(&req)
			if presenceEligible(req) {
				t.Fatalf("%s must stay outside the presence gate", tc.name)
			}
		})
	}
}
