package config

import (
	"time"

	"github.com/felinics/memoh/internal/conversationpresence"
)

// ConversationPresenceConfig is server policy, not a new management surface.
// Values are tunable starting defaults, never a user latency guarantee.
type ConversationPresenceConfig struct {
	Enabled                  *bool `toml:"enabled"`
	DeadlineSeconds          int   `toml:"deadline_seconds"`
	MaxInputRunes            int   `toml:"max_input_runes"`
	MaxOutputTokens          int   `toml:"max_output_tokens"`
	MaxTextRunes             int   `toml:"max_text_runes"`
	MaxConcurrentPerProvider int   `toml:"max_concurrent_per_provider"`
}

func (c ConversationPresenceConfig) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

func (c ConversationPresenceConfig) Policy() conversationpresence.Policy {
	deadline := c.DeadlineSeconds
	if deadline <= 0 {
		deadline = 15
	}
	input, output, text := c.MaxInputRunes, c.MaxOutputTokens, c.MaxTextRunes
	if input <= 0 {
		input = 1200
	}
	if output <= 0 {
		output = 1024
	}
	if text <= 0 {
		text = 160
	}
	return conversationpresence.Policy{Limits: conversationpresence.Limits{Deadline: time.Duration(deadline) * time.Second, MaxInputRunes: input, MaxOutputTokens: output, MaxTextRunes: text}}
}

func (c ConversationPresenceConfig) ConcurrentLimit() int {
	if c.MaxConcurrentPerProvider > 0 {
		return c.MaxConcurrentPerProvider
	}
	return 1
}
