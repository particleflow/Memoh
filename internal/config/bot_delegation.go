package config

import (
	"fmt"
	"strings"
	"time"
)

// BotDelegationConfig configures the existing host-owned specialist execution.
// It does not change Agent runtime timeouts or the result delivery contract.
type BotDelegationConfig struct {
	ExecutionTimeout string `toml:"execution_timeout"`
}

func (c BotDelegationConfig) ExecutionTimeoutDuration() (time.Duration, error) {
	value := strings.TrimSpace(c.ExecutionTimeout)
	if value == "" {
		value = "30m"
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("bot_delegation.execution_timeout must be a non-negative duration")
	}
	return duration, nil
}
