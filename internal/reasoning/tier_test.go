package reasoning

import (
	"slices"
	"testing"
)

func TestDownshiftTier(t *testing.T) {
	glm := []string{EffortLow, EffortHigh, EffortXHigh}
	cases := []struct {
		name       string
		desired    string
		ceiling    string
		advertised []string
		want       string
		wantOK     bool
	}{
		{"chat default under xhigh cap", EffortHigh, EffortXHigh, glm, EffortHigh, true},
		{"short input under xhigh cap", EffortLow, EffortXHigh, glm, EffortLow, true},
		{"complex keeps cap", EffortXHigh, EffortXHigh, glm, EffortXHigh, true},
		{"user set low keeps everything at low", EffortHigh, EffortLow, glm, EffortLow, true},
		{"user set low even for low desire", EffortLow, EffortLow, glm, EffortLow, true},
		{"desired stronger than ceiling clamps to ceiling", EffortXHigh, EffortHigh, glm, EffortHigh, true},
		{
			// desired low is not advertised: weakest representable below the
			// ceiling is medium, still a downgrade of the cap.
			name: "desired unadvertised falls back to weakest under ceiling", desired: EffortLow,
			ceiling: EffortHigh, advertised: []string{EffortMedium, EffortHigh}, want: EffortMedium, wantOK: true,
		},
		{
			name: "toggle ceiling is unrankable", desired: EffortLow, ceiling: EffortEnabled,
			advertised: []string{EffortEnabled}, want: "", wantOK: false,
		},
		{
			name: "toggle desire is unrankable", desired: EffortEnabled, ceiling: EffortHigh,
			advertised: []string{EffortLow, EffortHigh}, want: "", wantOK: false,
		},
		{"empty advertised uses the common base", EffortHigh, EffortXHigh, nil, EffortHigh, true},
		{"max cap on full ladder", EffortLow, EffortMax, OrderedEfforts(), EffortLow, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DownshiftTier(tc.desired, tc.ceiling, tc.advertised)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("DownshiftTier(%q, %q, %v) = (%q, %v), want (%q, %v)",
					tc.desired, tc.ceiling, tc.advertised, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestDownshiftTierNeverExceedsCeiling(t *testing.T) {
	ladders := [][]string{
		{EffortMinimal, EffortLow},
		{EffortLow, EffortHigh, EffortXHigh},
		OrderedEfforts(),
		{EffortMedium, EffortMax},
	}
	for _, levels := range ladders {
		for _, ceiling := range levels {
			for _, desired := range []string{EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax} {
				got, ok := DownshiftTier(desired, ceiling, levels)
				if !ok {
					continue
				}
				if slices.Index(orderedEfforts, got) > slices.Index(orderedEfforts, ceiling) {
					t.Errorf("DownshiftTier(%q, %q, %v) = %q exceeds ceiling", desired, ceiling, levels, got)
				}
			}
		}
	}
}
