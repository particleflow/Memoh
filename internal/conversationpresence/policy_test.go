package conversationpresence

import (
	"testing"
	"time"

	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/reasoning"
)

func directSource() inputsource.Source {
	return inputsource.Source{Kind: inputsource.User, BotID: "bot", SessionID: "thread", UserID: "actor"}
}

func TestContinuityUsesStateInsteadOfAClockAlone(t *testing.T) {
	now := time.Unix(20000, 0)
	policy := ContinuityPolicy{IdleAfter: 3 * time.Second, IdleStartsEpisode: true}
	for _, tc := range []struct {
		name  string
		facts Facts
		want  Temperature
	}{
		{"first", Facts{Admitted: true, FirstDirectInput: true}, Cold},
		{"reply across idle", Facts{Admitted: true, PendingUserInput: true, Now: now, LastDirectInput: now.Add(-time.Hour)}, Hot},
		{"short explicit new topic", Facts{Admitted: true, ExplicitRestart: true, RecentExchange: true}, Cold},
		{"idle but unresolved", Facts{Admitted: true, HistoryAvailable: true, Now: now, LastDirectInput: now.Add(-time.Hour)}, Uncertain},
		{"idle and settled", Facts{Admitted: true, HistoryAvailable: true, PreviousSettled: true, Now: now, LastDirectInput: now.Add(-time.Hour)}, Cold},
		{"new page old run", Facts{Admitted: true, InFlightFollowUp: true}, Hot},
		{"missing history", Facts{Admitted: true}, Uncertain},
		{"duplicate", Facts{Admitted: true, FirstDirectInput: true, Duplicate: true}, Ineligible},
		{"retry", Facts{Admitted: true, FirstDirectInput: true, Replacement: true}, Ineligible},
		{"not accepted", Facts{FirstDirectInput: true}, Ineligible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(directSource(), "bot", "thread", tc.facts, policy); got.Temperature != tc.want {
				t.Fatalf("classification = %+v, want %s", got, tc.want)
			}
		})
	}
	for _, after := range []time.Duration{time.Second, time.Hour} {
		facts := Facts{Admitted: true, HistoryAvailable: true, PreviousSettled: true, Now: now, LastDirectInput: now.Add(-time.Minute)}
		got := Classify(directSource(), "bot", "thread", facts, ContinuityPolicy{IdleAfter: after, IdleStartsEpisode: true})
		if (got.Temperature == Cold) != (after == time.Second) {
			t.Fatalf("configured window ignored: after %s, %+v", after, got)
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
		got := Classify(source, "bot", "thread", Facts{Admitted: true, FirstDirectInput: true}, ContinuityPolicy{})
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
