// Package conversationpresence owns optional conversational acknowledgement
// policy. It does not admit turns, run tools, write chat history or change a
// session's model preference. Hosts keep those responsibilities.
package conversationpresence

import (
	"slices"

	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/reasoning"
)

type Temperature string

const (
	Cold       Temperature = "cold"
	Hot        Temperature = "hot"
	Uncertain  Temperature = "uncertain"
	Ineligible Temperature = "ineligible"
)

// Facts are host-resolved state, not values decoded from a user message. This
// package deliberately has no magic words or topic-name special cases.
type Facts struct {
	Admitted          bool
	Duplicate         bool
	Replacement       bool
	FirstDirectInput  bool
	PendingUserInput  bool
	ReplyContinuation bool
	HistoryAvailable  bool
	PreviousSettled   bool
}

type Decision struct {
	Temperature Temperature
	Reason      string
}

// Classify reads only host-resolved Facts. Each rule removes a distinct case,
// so the final rule covers exactly "a trusted direct message whose previous
// exchange has settled": the user is now waiting for this turn to answer.
func Classify(source inputsource.Source, botID, sessionID string, f Facts) Decision {
	if !f.Admitted || f.Duplicate || f.Replacement || source.Scoped(botID, sessionID).Kind != inputsource.User {
		return Decision{Ineligible, "not_new_direct_admission"}
	}
	switch {
	case f.PendingUserInput:
		return Decision{Hot, "pending_user_input"}
	case f.ReplyContinuation:
		return Decision{Hot, "reply_continuation"}
	case !f.PreviousSettled:
		return Decision{Hot, "inflight_followup"}
	case f.FirstDirectInput:
		return Decision{Cold, "first_direct_input"}
	case !f.HistoryAvailable:
		return Decision{Uncertain, "history_unavailable"}
	default:
		return Decision{Cold, "settled_followup"}
	}
}

type ThinkingChoice struct {
	// Config is request-local and must never be stored as a user preference.
	Config *reasoning.Config
	Effort string
	Reason string
	Usable bool
}

// ChooseThinking implements the user's order: off when supported, otherwise
// low, otherwise the weakest effective advertised tier. There are no model IDs
// or vendor-name branches here. The host supplies verified effective options;
// generic fallback options inferred from an unknown catalog are not evidence.
func ChooseThinking(mode string, verified bool, advertised []string, opts reasoning.Options) ThinkingChoice {
	if !verified {
		return ThinkingChoice{Reason: "capability_unverified"}
	}
	if mode == reasoning.ModeNone {
		return ThinkingChoice{Usable: true, Reason: "already_non_reasoning"}
	}
	if !opts.Supported {
		return ThinkingChoice{Reason: "control_unavailable"}
	}
	if opts.CanDisable {
		cfg := reasoning.ResolveConfig(mode, advertised, opts, "", reasoning.EffortDisable)
		if cfg != nil && cfg.Disabled {
			return ThinkingChoice{Config: cfg, Effort: reasoning.EffortDisable, Reason: "off_preferred", Usable: true}
		}
	}
	chosen := ""
	if slices.Contains(opts.Efforts, reasoning.EffortLow) {
		chosen = reasoning.EffortLow
	} else {
		for _, effort := range reasoning.OrderedEfforts() {
			if slices.Contains(opts.Efforts, effort) {
				chosen = effort
				break
			}
		}
		if chosen == "" && slices.Contains(opts.Efforts, reasoning.EffortEnabled) {
			chosen = reasoning.EffortEnabled
		}
	}
	if chosen == "" {
		return ThinkingChoice{Reason: "control_unavailable"}
	}
	if !slices.Contains(advertised, chosen) {
		return ThinkingChoice{Reason: "tier_not_advertised"}
	}
	cfg := reasoning.ResolveConfig(mode, advertised, opts, "", chosen)
	if cfg == nil || !cfg.Active || cfg.Disabled || cfg.Effort != chosen {
		return ThinkingChoice{Reason: "requested_effort_not_effective"}
	}
	return ThinkingChoice{Config: cfg, Effort: chosen, Reason: "lowest_supported_effort", Usable: true}
}
