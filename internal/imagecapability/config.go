// Package imagecapability adapts explicitly configured Images APIs to Memoh's
// existing tool and attachment ports. It does not own an agent runtime or jobs.
package imagecapability

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/providers"
)

// ConfigKey is a new optional protocol contract in the existing provider config.
// Its absence leaves the existing image tool's behavior unchanged.
const ConfigKey = "images_api"

type Config struct {
	Protocol string `json:"protocol"`
	Edits    bool   `json:"edits,omitempty"`
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
	// Read old policies explicitly; an upgrade must not silently change a paid request.
	var stored struct {
		Config
		ModelMode    json.RawMessage `json:"model_mode"`
		EditEncoding json.RawMessage `json:"edit_encoding"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil || stored.Protocol != "openai-images" {
		return Config{}, true, errors.New("images_api requires protocol openai-images")
	}
	var modelMode, editEncoding string
	if stored.ModelMode != nil && (json.Unmarshal(stored.ModelMode, &modelMode) != nil || modelMode != "configured") {
		return Config{}, true, errors.New("legacy images_api model policy requires administrator migration to the configured Bot image model; no image request was sent")
	}
	if (stored.EditEncoding != nil && (json.Unmarshal(stored.EditEncoding, &editEncoding) != nil || editEncoding != "json-images")) ||
		(stored.Edits && stored.ModelMode != nil && stored.EditEncoding == nil) {
		return Config{}, true, errors.New("legacy images_api encoding requires administrator migration to JSON references; no image request was sent")
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
	return stored.Config, true, nil
}
