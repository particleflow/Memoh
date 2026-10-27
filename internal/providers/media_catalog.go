package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/models"
)

// MediaCatalogProtocolMagpie selects Magpie's documented video-inclusive catalog.
// It is an explicit discovery choice, never inferred from a provider name or host.
const MediaCatalogProtocolMagpie = "magpie"

var (
	ErrMediaCatalogConfig    = errors.New("media catalog requires an enabled OpenAI-compatible provider with a valid saved base URL")
	ErrMediaCatalogResponse  = errors.New("media catalog returned an invalid or oversized response")
	ErrMediaCatalogTransport = errors.New("media catalog request failed")
)

// MediaCatalogHTTPError intentionally retains only status, never a response body,
// URL or transport error that could expose provider configuration in logs.
type MediaCatalogHTTPError struct{ Status int }

func (e *MediaCatalogHTTPError) Error() string {
	return fmt.Sprintf("media catalog upstream status %d", e.Status)
}

type MediaCatalogModel struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind,omitempty"`
	Input            []string `json:"input"`
	Output           []string `json:"output"`
	CapabilitySource string   `json:"capability_source"`
	ContextWindow    *int     `json:"-" swaggerignore:"true"`
	MaxOutputTokens  *int     `json:"-" swaggerignore:"true"`
}

type MediaCatalogResponse struct {
	ProviderID string              `json:"provider_id"`
	Protocol   string              `json:"protocol"`
	CheckedAt  time.Time           `json:"checked_at"`
	Models     []MediaCatalogModel `json:"models"`
	// Audio input metadata does not prove a model implements the transcription API.
	TranscriptionStatus string `json:"transcription_status"`
}

// DiscoverMediaCatalog reads one saved provider and its remote catalog. It does
// not import models, refresh credentials, test a model, or write configuration.
func (s *Service) DiscoverMediaCatalog(ctx context.Context, id string) (MediaCatalogResponse, error) {
	providerID, err := db.ParseUUID(id)
	if err != nil {
		return MediaCatalogResponse{}, err
	}
	provider, err := s.queries.GetProviderByID(ctx, providerID)
	if err != nil {
		return MediaCatalogResponse{}, err
	}
	clientType := models.ClientType(provider.ClientType)
	if !provider.Enable || (clientType != models.ClientTypeOpenAICompletions && clientType != models.ClientTypeOpenAIResponses) {
		return MediaCatalogResponse{}, ErrMediaCatalogConfig
	}
	base := strings.TrimRight(strings.TrimSpace(ProviderConfigString(provider, "base_url")), "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return MediaCatalogResponse{}, ErrMediaCatalogConfig
	}
	// Only the static OpenAI-compatible credential path above is allowed. OAuth
	// refresh/exchange client types are excluded before credential resolution.
	creds, err := s.ResolveModelCredentials(ctx, provider)
	if err != nil {
		return MediaCatalogResponse{}, ErrMediaCatalogConfig
	}
	requestCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, base+"/models", http.NoBody)
	if err != nil {
		return MediaCatalogResponse{}, ErrMediaCatalogConfig
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Magpie-Videomakers", "1")
	if creds.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+creds.APIKey)
	}
	client := *s.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// No cookies from another provider and no redirects to another destination.
	client.Jar = nil
	resp, err := client.Do(req) //nolint:gosec // Destination comes only from the saved provider, as in existing model discovery.
	if err != nil {
		if ctx.Err() != nil {
			return MediaCatalogResponse{}, ctx.Err()
		}
		return MediaCatalogResponse{}, ErrMediaCatalogTransport
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return MediaCatalogResponse{}, &MediaCatalogHTTPError{Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteModelsResponseLimit+1))
	if err != nil || len(body) > remoteModelsResponseLimit {
		return MediaCatalogResponse{}, ErrMediaCatalogResponse
	}
	var catalog struct {
		Data *[]struct {
			ID              string `json:"id"`
			Kind            string `json:"kind"`
			ContextWindow   *int   `json:"context_window"`
			MaxOutputTokens *int   `json:"max_output_tokens"`
			Modalities      struct {
				Input  []string `json:"input"`
				Output []string `json:"output"`
			} `json:"modalities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil || catalog.Data == nil {
		return MediaCatalogResponse{}, ErrMediaCatalogResponse
	}
	out := MediaCatalogResponse{ProviderID: id, Protocol: MediaCatalogProtocolMagpie, CheckedAt: time.Now().UTC(), Models: []MediaCatalogModel{}, TranscriptionStatus: "unverified"}
	seen := make(map[string]bool)
	for _, row := range *catalog.Data {
		// IDs are displayed as text, not URLs. Reject secret/address echoes instead
		// of silently altering a model ID or passing arbitrary upstream text on.
		if !safeCatalogID(row.ID, creds.APIKey, u.Hostname()) || seen[row.ID] {
			return MediaCatalogResponse{}, ErrMediaCatalogResponse
		}
		seen[row.ID] = true
		model := MediaCatalogModel{ID: row.ID, Input: catalogModalities(row.Modalities.Input), Output: catalogModalities(row.Modalities.Output), CapabilitySource: "unspecified"}
		if row.ContextWindow != nil && *row.ContextWindow > 0 {
			model.ContextWindow = row.ContextWindow
		}
		if row.MaxOutputTokens != nil && *row.MaxOutputTokens > 0 {
			model.MaxOutputTokens = row.MaxOutputTokens
		}
		switch row.Kind {
		case "video", "image", "audio", "transcription", "chat", "embedding", "rerank", "decision":
			model.Kind = row.Kind
		}
		switch {
		case model.Kind != "" && (len(model.Input) > 0 || len(model.Output) > 0):
			model.CapabilitySource = "catalog_kind_and_modalities"
		case model.Kind != "":
			model.CapabilitySource = "catalog_kind"
		case len(model.Input) > 0 || len(model.Output) > 0:
			model.CapabilitySource = "catalog_modalities"
		}
		out.Models = append(out.Models, model)
	}
	return out, nil
}

func safeCatalogID(id, key, host string) bool {
	if strings.TrimSpace(id) != id || id == "" || len(id) > 512 || strings.Contains(id, "://") || strings.Contains(id, host) || (key != "" && strings.Contains(id, key)) {
		return false
	}
	return !strings.ContainsFunc(id, unicode.IsControl)
}

func catalogModalities(values []string) []string {
	out := []string{}
	for _, value := range values {
		switch value {
		case "text", "image", "audio", "video", "pdf":
			found := false
			for _, existing := range out {
				if existing == value {
					found = true
					break
				}
			}
			if !found {
				out = append(out, value)
			}
		}
	}
	return out
}
