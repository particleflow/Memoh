package reasoning

import (
	"slices"
	"strings"
)

// TierDecision records one per-turn effort tiering outcome for accounting: the
// tier actually sent, the tier the ordinary resolution chain had picked (the
// ceiling the decision never raises above), and the bucket that classified the
// turn. A nil decision means no tiering happened this turn.
type TierDecision struct {
	Applied string `json:"applied"`
	Cap     string `json:"cap"`
	Source  string `json:"source"`
}

// DownshiftTier resolves the tier to send when a turn wants desired but must
// not exceed ceiling (the effort the ordinary chain resolved). It returns the
// strongest advertised level at or below the weaker of the two; when no
// advertised level is that weak, the weakest advertised level at or below
// ceiling. ok is false — and the caller must leave the resolved effort alone —
// when either value is not an active tier (toggle models run on "enabled") or
// the advertised set cannot express any level at or below ceiling, which cannot
// happen for a ceiling that itself came from the advertised set.
//
// The result never exceeds ceiling, so a caller cannot use tiering to escalate
// a user's chosen effort; it only ever holds or lowers it.
func DownshiftTier(desired, ceiling string, advertised []string) (string, bool) {
	d := slices.Index(orderedEfforts, strings.TrimSpace(desired))
	c := slices.Index(orderedEfforts, strings.TrimSpace(ceiling))
	if d < 0 || c < 0 {
		return "", false
	}
	top := min(d, c)
	levels := effectiveEfforts(advertised)
	best := -1
	for _, level := range levels {
		i := slices.Index(orderedEfforts, level)
		if i < 0 || i > top {
			continue
		}
		if best == -1 || i > best {
			best = i
		}
	}
	if best >= 0 {
		return orderedEfforts[best], true
	}
	// Nothing at or below the desired weakness: take the weakest level the
	// model can express without exceeding the ceiling.
	fallback := -1
	for _, level := range levels {
		i := slices.Index(orderedEfforts, level)
		if i < 0 || i > c {
			continue
		}
		if fallback == -1 || i < fallback {
			fallback = i
		}
	}
	if fallback < 0 {
		return "", false
	}
	return orderedEfforts[fallback], true
}
