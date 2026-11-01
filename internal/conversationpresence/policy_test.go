package conversationpresence

import (
	"testing"

	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/reasoning"
)

func directSource() inputsource.Source {
	return inputsource.Source{Kind: inputsource.User, BotID: "bot", SessionID: "thread", UserID: "actor"}
}

func TestClassifyUsesOnlyHostResolvedState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		facts  Facts
		want   Temperature
		reason string
	}{
		{"first", Facts{Admitted: true, FirstDirectInput: true, PreviousSettled: true, HistoryAvailable: true}, Cold, "first_direct_input"},
		{"pending user input", Facts{Admitted: true, PendingUserInput: true, PreviousSettled: true, HistoryAvailable: true}, Hot, "pending_user_input"},
		{"reply continuation", Facts{Admitted: true, ReplyContinuation: true, PreviousSettled: true, HistoryAvailable: true}, Hot, "reply_continuation"},
		{"unsettled previous run", Facts{Admitted: true, HistoryAvailable: true}, Hot, "inflight_followup"},
		{"settled follow-up after recent exchange", Facts{Admitted: true, PreviousSettled: true, HistoryAvailable: true}, Cold, "settled_followup"},
		{"missing history", Facts{Admitted: true, PreviousSettled: true}, Uncertain, "history_unavailable"},
		{"duplicate", Facts{Admitted: true, FirstDirectInput: true, Duplicate: true}, Ineligible, "not_new_direct_admission"},
		{"retry", Facts{Admitted: true, FirstDirectInput: true, Replacement: true}, Ineligible, "not_new_direct_admission"},
		{"not accepted", Facts{FirstDirectInput: true}, Ineligible, "not_new_direct_admission"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(directSource(), "bot", "thread", tc.facts)
			if got.Temperature != tc.want || got.Reason != tc.reason {
				t.Fatalf("classification = %+v, want %s/%s", got, tc.want, tc.reason)
			}
		})
	}
}

// The rule set must partition Facts: every accepted direct-input combination
// reaches exactly one reason, and no rule is unreachable.
func TestClassifyPartitionsEveryAcceptedDirectInput(t *testing.T) {
	reasons := map[string]bool{}
	for mask := 0; mask < 1<<5; mask++ {
		facts := Facts{Admitted: true}
		for bit, field := range []*bool{&facts.FirstDirectInput, &facts.PendingUserInput, &facts.ReplyContinuation, &facts.PreviousSettled, &facts.HistoryAvailable} {
			*field = mask&(1<<bit) != 0
		}
		got := Classify(directSource(), "bot", "thread", facts)
		if got.Reason == "" || got.Temperature == Ineligible {
			t.Fatalf("facts %+v did not classify: %+v", facts, got)
		}
		reasons[got.Reason] = true
	}
	for _, reason := range []string{"first_direct_input", "pending_user_input", "reply_continuation", "inflight_followup", "history_unavailable", "settled_followup"} {
		if !reasons[reason] {
			t.Fatalf("reason %q is unreachable; reachable=%v", reason, reasons)
		}
	}
}

func TestOnlyTrustedDirectSourceIsEligible(t *testing.T) {
	for _, source := range []inputsource.Source{
		{Kind: inputsource.Schedule, BotID: "bot", SessionID: "thread", TaskID: "task"},
		{Kind: inputsource.Result, BotID: "bot", SessionID: "thread", TaskID: "task"},
		{Kind: inputsource.User, BotID: "other", SessionID: "thread", UserID: "actor"},
		{Kind: inputsource.User, BotID: "bot", SessionID: "thread"},
	} {
		got := Classify(source, "bot", "thread", Facts{Admitted: true, FirstDirectInput: true})
		if got.Temperature != Ineligible {
			t.Fatalf("source %+v became eligible: %+v", source, got)
		}
	}
}

func TestThinkingPrefersOffThenLowWithoutModelNames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     string
		levels   []string
		off      string
		verified bool
		want     string
		usable   bool
	}{
		{"off before low", reasoning.ModeToggle, []string{reasoning.EffortDisable, "low", "high"}, reasoning.OffSupportAccepted, true, reasoning.EffortDisable, true},
		{"low before other tiers", reasoning.ModeToggle, []string{"minimal", "low", "high"}, reasoning.OffSupportRejected, true, "low", true},
		{"lowest when low missing", reasoning.ModeToggle, []string{"minimal", "high"}, reasoning.OffSupportRejected, true, "minimal", true},
		{"catalog order not tier order", reasoning.ModeToggle, []string{"max", "minimal", "high"}, reasoning.OffSupportRejected, true, "minimal", true},
		{"forced thinking low", reasoning.ModeToggle, []string{"low", "high", "max"}, reasoning.OffSupportRejected, true, "low", true},
		{"unknown capability", reasoning.ModeToggle, []string{"low", "high"}, reasoning.OffSupportRejected, false, "", false},
		{"no controllable tier", reasoning.ModeAlways, nil, reasoning.OffSupportRejected, true, "", false},
		{"already no thinking", reasoning.ModeNone, nil, "", true, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := reasoning.OptionsFor(tc.mode, tc.levels, "openai-completions", tc.off)
			got := ChooseThinking(tc.mode, tc.verified, tc.levels, opts)
			if got.Usable != tc.usable || got.Effort != tc.want {
				t.Fatalf("choice = %+v, want %q usable=%v", got, tc.want, tc.usable)
			}
			if tc.want == reasoning.EffortDisable && (got.Config == nil || !got.Config.Disabled) {
				t.Fatalf("off did not resolve off: %+v", got)
			}
		})
	}
	// A projection inconsistent with the underlying catalog must not silently
	// run at the catalog's stored/default high tier.
	got := ChooseThinking(reasoning.ModeToggle, true, []string{"high"}, reasoning.Options{Supported: true, Efforts: []string{"low"}})
	if got.Usable {
		t.Fatalf("inconsistent low projection accepted: %+v", got)
	}
	got = ChooseThinking(reasoning.ModeToggle, true, nil, reasoning.OptionsFor(reasoning.ModeToggle, nil, "openai-completions", reasoning.OffSupportRejected))
	if got.Usable {
		t.Fatalf("synthetic fallback tiers accepted as advertised capabilities: %+v", got)
	}
}
