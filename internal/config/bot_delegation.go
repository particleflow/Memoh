package config

import (
	"errors"
	"strings"
	"time"
)

// BotDelegationConfig configures existing host-owned execution and reporting.
type BotDelegationConfig struct {
	ExecutionTimeout      string `toml:"execution_timeout"`
	ResultSummaryAttempts *int   `toml:"result_summary_attempts"`
}

const DefaultResultSummaryAttempts = 3

func (c BotDelegationConfig) SummaryAttemptLimit() (int, error) {
	if c.ResultSummaryAttempts == nil {
		return DefaultResultSummaryAttempts, nil
	}
	if *c.ResultSummaryAttempts < 1 {
		return 0, errors.New("bot_delegation.result_summary_attempts must be a positive integer")
	}
	return *c.ResultSummaryAttempts, nil
}

func (c BotDelegationConfig) ExecutionTimeoutDuration() (time.Duration, error) {
	value := strings.TrimSpace(c.ExecutionTimeout)
	if value == "" {
		value = "30m"
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, errors.New("bot_delegation.execution_timeout must be a non-negative duration")
	}
	return duration, nil
}
