package application

import (
	"encoding/json"
	"testing"

	"github.com/felinics/memoh/internal/conversationpresence"
)

// TestConversationPresencePostgresDecisionAccounting covers the fast-reply v3
// accounting timeline (design v3 §6.3): step1 inserts the receipt as
// uncertain, step2 updates the decision and inserts the decision row with a
// NULL outcome, and the finish write point fills the outcome exactly once.
func TestConversationPresencePostgresDecisionAccounting(t *testing.T) {
	claims, ctx, pool := presencePostgresFixture(t)
	ticket, facts, admittedAt, inserted, err := claims.prepareStep1(ctx, false)
	if err != nil || !inserted {
		t.Fatalf("step1: inserted=%v error=%v", inserted, err)
	}
	if !facts.Admitted || !facts.FirstDirectInput {
		t.Fatalf("facts %+v", facts)
	}
	if admittedAt.IsZero() {
		t.Fatal("admittedAt anchor missing")
	}
	var temperature string
	if err := pool.QueryRow(ctx, `SELECT temperature FROM conversation_presence WHERE run_id=$1`, claims.handle.RunID).Scan(&temperature); err != nil || temperature != "uncertain" {
		t.Fatalf("step1 temperature=%s error=%v (want uncertain)", temperature, err)
	}

	result := &presenceJevResult{
		Mode:        presenceModeFastReply,
		Temperature: conversationpresence.Cold,
		Reason:      "jev_fast_reply",
		Direction:   "progress",
		Urgency:     2,
		Phase:       conversationpresence.Thinking,
		Detail: presenceDecisionDetail{
			Stage1: []presenceStage1Attempt{{Attempt: 0, Choice: "need_context", NextSource: "task_state", Probability: 0.7, LatencyMs: 12}},
			Stage2: &presenceStage2Record{Direction: "progress", DirectionProbability: 0.8, Urgency: 2, Value: 1.1},
			Pulls:  []presencePullRecord{{Source: "task_state", Chars: 42, LatencyMs: 3}},
			Usage:  &presenceJevUsage{InputTokens: 574, OutputTokens: 12},
		},
	}
	if err := claims.finalizePresenceDecision(ctx, &ticket, result, 845); err != nil {
		t.Fatalf("step2: %v", err)
	}
	var state, finalTemperature, episode string
	if err := pool.QueryRow(ctx, `SELECT state, temperature, episode_id::text FROM conversation_presence WHERE run_id=$1`, claims.handle.RunID).Scan(&state, &finalTemperature, &episode); err != nil {
		t.Fatal(err)
	}
	if state != "eligible" || finalTemperature != "cold" {
		t.Fatalf("receipt after step2: state=%s temperature=%s", state, finalTemperature)
	}
	if episode != claims.handle.RunID {
		t.Fatalf("cold decision must re-point the episode to this run: %s", episode)
	}
	var mode, direction string
	var outcome *string
	var detailRaw []byte
	if err := pool.QueryRow(ctx, `SELECT final_mode, direction, detail->>'outcome', detail FROM conversation_presence_decisions WHERE invocation_id=$1`, ticket.InvocationID).Scan(&mode, &direction, &outcome, &detailRaw); err != nil {
		t.Fatal(err)
	}
	if mode != presenceModeFastReply || direction != "progress" || outcome != nil {
		t.Fatalf("decision row mode=%s direction=%s outcome=%v", mode, direction, outcome)
	}
	var detail presenceDecisionDetail
	if err := json.Unmarshal(detailRaw, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Stage1) != 1 || detail.Stage1[0].NextSource != "task_state" || detail.Stage2 == nil || len(detail.Pulls) != 1 || detail.Usage.InputTokens != 574 {
		t.Fatalf("detail round-trip mismatch: %+v", detail)
	}

	// Claim requires cold + eligible: the step2 temperature is what unlocks it.
	if ok, err := claims.Claim(ctx, ticket); err != nil || !ok {
		t.Fatalf("claim after step2: %v %v", ok, err)
	}
	// Finish write point fills the outcome once; a second write is a no-op.
	if err := claims.recordPresenceOutcome(ctx, ticket, "published"); err != nil {
		t.Fatalf("outcome write: %v", err)
	}
	if err := claims.recordPresenceOutcome(ctx, ticket, "superseded"); err != nil {
		t.Fatalf("second outcome write must be fail-open idempotent: %v", err)
	}
	var finalOutcome string
	if err := pool.QueryRow(ctx, `SELECT detail->>'outcome' FROM conversation_presence_decisions WHERE invocation_id=$1`, ticket.InvocationID).Scan(&finalOutcome); err != nil {
		t.Fatal(err)
	}
	if finalOutcome != "published" {
		t.Fatalf("outcome overwritten: %q", finalOutcome)
	}
}

// TestConversationPresencePostgresDecisionRowSurvivesClose keeps the §6.3
// superseded accounting: a row closed after step2 keeps its decision trail
// even though the receipt can no longer flip temperature.
func TestConversationPresencePostgresDecisionRowSurvivesClose(t *testing.T) {
	claims, ctx, pool := presencePostgresFixture(t)
	ticket, _, _, inserted, err := claims.prepareStep1(ctx, false)
	if err != nil || !inserted {
		t.Fatalf("step1: inserted=%v error=%v", inserted, err)
	}
	if err := claims.Close(ctx, ticket, "formal_first"); err != nil {
		t.Fatal(err)
	}
	result := &presenceJevResult{Mode: presenceModeNoFastReply, Temperature: conversationpresence.Hot, Reason: "jev_no_fast_reply", Phase: conversationpresence.Thinking}
	if err := claims.finalizePresenceDecision(ctx, &ticket, result, 320); err != nil {
		t.Fatalf("step2 after close: %v", err)
	}
	var temperature, state string
	if err := pool.QueryRow(ctx, `SELECT temperature, state FROM conversation_presence WHERE run_id=$1`, claims.handle.RunID).Scan(&temperature, &state); err != nil {
		t.Fatal(err)
	}
	if temperature != "uncertain" || state != "closed" {
		t.Fatalf("closed receipt must not flip: temperature=%s state=%s", temperature, state)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conversation_presence_decisions WHERE invocation_id=$1`, ticket.InvocationID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("decision row must survive close: count=%d error=%v", count, err)
	}
	// And the finish write point still closes the accounting row.
	if err := claims.recordPresenceOutcome(ctx, ticket, "superseded"); err != nil {
		t.Fatal(err)
	}
	var outcome []byte
	if err := pool.QueryRow(ctx, `SELECT detail->'outcome' FROM conversation_presence_decisions WHERE invocation_id=$1`, ticket.InvocationID).Scan(&outcome); err != nil || string(outcome) != `"superseded"` {
		t.Fatalf("outcome %s error=%v", outcome, err)
	}
}
