package config

import "testing"

func TestEffectiveEffortTieringMode(t *testing.T) {
	cases := []struct {
		raw        string
		want       string
		recognized bool
	}{
		{"", EffortTieringModeActive, true},
		{"active", EffortTieringModeActive, true},
		{"off", EffortTieringModeOff, true},
		{"OFF", EffortTieringModeOff, true},
		{" off ", EffortTieringModeOff, true},
		{"bogus", EffortTieringModeActive, false},
	}
	for _, tc := range cases {
		got, recognized := AgentConfig{EffortTiering: tc.raw}.EffectiveEffortTieringMode()
		if got != tc.want || recognized != tc.recognized {
			t.Errorf("EffectiveEffortTieringMode(%q) = (%q, %v), want (%q, %v)",
				tc.raw, got, recognized, tc.want, tc.recognized)
		}
	}
}
