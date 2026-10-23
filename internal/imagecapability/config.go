// Package imagecapability adapts explicitly configured Images APIs to Memoh's
// existing tool and attachment ports. It does not own an agent runtime or jobs.
package imagecapability

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/providers"
)

// ConfigKey is a new optional protocol contract in the existing provider config.
// Its absence leaves the existing image tool's behavior unchanged.
const ConfigKey = "images_api"

type Config struct {
	Protocol  string `json:"protocol"`
	ModelMode string `json:"model_mode"`
	Edits     bool   `json:"edits,omitempty"`
}

func parseConfig(provider sqlc.Provider) (Config, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(provider.Config, &fields); err != nil {
		return Config{}, false, errors.New("image provider configuration is invalid")
	}
	raw, enabled := fields[ConfigKey]
	if !enabled {
		return Config{}, false, nil
	}
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil || cfg.Protocol != "openai-images" || (cfg.ModelMode != "configured" && cfg.ModelMode != "omit") {
		return Config{}, true, errors.New("images_api requires protocol openai-images and model_mode configured or omit")
	}
	switch models.ClientType(provider.ClientType) {
	case models.ClientTypeOpenAICompletions, models.ClientTypeOpenAIResponses:
	default:
		return Config{}, true, errors.New("images_api requires an API-key OpenAI-compatible provider")
	}
	u, err := url.Parse(providers.ProviderConfigString(provider, "base_url"))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Config{}, true, errors.New("images_api requires an explicit HTTP base URL without userinfo, query or fragment")
	}
	return cfg, true, nil
}

func endpoint(base, operation string) string {
	return strings.TrimRight(base, "/") + "/images/" + operation
}
