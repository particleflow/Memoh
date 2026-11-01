package config

import (
	"strings"
	"time"

	"github.com/felinics/memoh/internal/conversationpresence"
)

// ConversationPresenceConfig is server policy, not a new management surface.
// Values are tunable starting defaults, never a user latency guarantee.
type ConversationPresenceConfig struct {
	Enabled                  *bool     `toml:"enabled"`
	DeadlineSeconds          int       `toml:"deadline_seconds"`
	MaxInputRunes            int       `toml:"max_input_runes"`
	MaxOutputTokens          int       `toml:"max_output_tokens"`
	MaxTextRunes             int       `toml:"max_text_runes"`
	MaxConcurrentPerProvider int       `toml:"max_concurrent_per_provider"`
	Jev                      JevConfig `toml:"jev"`
}

// JevConfig is the fast-reply v3 decision subtable. An empty endpoint keeps
// v3 off: the whole chain then uses the structural Classify fallback.
type JevConfig struct {
	Endpoint       string   `toml:"endpoint"`
	APIKey         string   `toml:"api_key" json:"-"`
	Model          string   `toml:"model"`
	TimeoutSeconds int      `toml:"timeout_seconds"`
	PullMax        int      `toml:"pull_max"`
	PullRunes      int      `toml:"pull_runes"`
	StateMaxRunes  int      `toml:"state_max_runes"`
	Directions     []string `toml:"directions"`
}

// Enabled reports whether the Jev decision path is configured at all.
func (c JevConfig) Enabled() bool { return strings.TrimSpace(c.Endpoint) != "" }

// Effective returns the configuration with design-v3 defaults applied
// (design v3 §6.4: model jev-latest, 10s window, pull_max 4, pull_runes 500,
// state_max_runes 3200, directions soothe/progress/eta/clarify with brief
// off by default).
func (c JevConfig) Effective() JevConfig {
	out := c
	if strings.TrimSpace(out.Model) == "" {
		out.Model = "jev-latest"
	}
	if out.TimeoutSeconds <= 0 {
		out.TimeoutSeconds = 10
	}
	if out.PullMax <= 0 {
		out.PullMax = 4
	}
	if out.PullRunes <= 0 {
		out.PullRunes = 500
	}
	if out.StateMaxRunes <= 0 {
		out.StateMaxRunes = 3200
	}
	if len(out.Directions) == 0 {
		out.Directions = []string{"soothe", "progress", "eta", "clarify"}
	}
	return out
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
