package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadBotDelegationExecutionTimeout(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    time.Duration
		invalid bool
	}{
		{"", 30 * time.Minute, false},
		{"2h", 2 * time.Hour, false},
		{"0s", 0, false},
		{"-1s", 0, true},
		{"later", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(file, fmt.Appendf(nil, "[bot_delegation]\nexecution_timeout = %q\n", tc.value), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(file)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid deadline accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := cfg.BotDelegation.ExecutionTimeoutDuration()
			if err != nil || got != tc.want {
				t.Fatalf("duration %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	got, err := (BotDelegationConfig{}).ExecutionTimeoutDuration()
	if err != nil || got != 30*time.Minute {
		t.Fatalf("omitted section: %v %v", got, err)
	}
}
