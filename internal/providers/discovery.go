package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/models"
)

// Discovery errors deliberately contain no upstream error, URL or response body.
var (
	ErrDiscoveryUnavailable = errors.New("provider is unavailable")
	ErrDiscoveryUnsupported = errors.New("read-only model discovery is not supported for this adapter")
	ErrDiscoveryUpstream    = errors.New("provider model metadata could not be read")
	ErrDiscoveryStorage     = errors.New("provider metadata could not be read")
)

// DiscoveryCapability is evidence, not a live inference test. Absence of a
// declaration is unknown, including when another capability is declared.
type DiscoveryCapability struct {
	Name   string `json:"name" enums:"vision,tool-call,image-output,image-edit,reasoning,file-input"`
	Status string `json:"status" enums:"declared,unknown"`
	Source string `json:"source" enums:"provider_template,unknown"`
}

type DiscoveredModel struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Capabilities []DiscoveryCapability `json:"capabilities"`
}

// DiscoveryAdapter describes code implemented by Memoh, not model/provider
// support, credentials, availability or entitlement to use an endpoint.
type DiscoveryAdapter struct {
	Type             string `json:"type" enums:"openai-completions,openai-responses,anthropic-messages,google-generative-ai,opencode-go"`
	ImagesGeneration bool   `json:"images_generation"`
	ImagesEdit       bool   `json:"images_edit"`
	Source           string `json:"source" enums:"memoh_adapter"`
}

type DiscoverModelsResponse struct {
	Models       []DiscoveredModel `json:"models"`
	Adapter      DiscoveryAdapter  `json:"adapter"`
	Completeness string            `json:"completeness" enums:"unknown"`
}

// listModelMetadata is the pure GET portion shared with model import. Credential
// refresh, catalog enrichment, embedding dimension probes and writes belong to
// callers; they are never performed by this function.
func listModelMetadata(ctx context.Context, baseURL string, creds ModelCredentials, clientType models.ClientType, client *http.Client) ([]sdk.Model, error) {
	provider := models.NewSDKProvider(baseURL, creds.APIKey, creds.CodexAccountID, clientType, probeTimeout, client)
	return provider.ListModels(ctx)
}

// DiscoverModels reads the existing team's provider and exact-ID catalog through
// the same scoped queries as provider management. It never imports, updates,
// refreshes OAuth tokens, probes inference, or persists upstream data.
func (s *Service) DiscoverModels(ctx context.Context, id string) (DiscoverModelsResponse, error) {
	var empty DiscoverModelsResponse
	providerID, err := db.ParseUUID(id)
	if err != nil {
		return empty, ErrDiscoveryUnavailable
	}
	provider, err := s.queries.GetProviderByID(ctx, providerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrDiscoveryUnavailable
	}
	if err != nil {
		return empty, ErrDiscoveryStorage
	}
	if !provider.Enable {
		return empty, ErrDiscoveryUnavailable
	}
	ct := models.ClientType(provider.ClientType)
	switch ct {
	case models.ClientTypeOpenAICompletions, models.ClientTypeOpenAIResponses,
		models.ClientTypeAnthropicMessages, models.ClientTypeGoogleGenerativeAI, models.ClientTypeOpenCodeGo:
	default:
		// In particular, do not enter Codex/Copilot's refresh/exchange paths.
		return empty, ErrDiscoveryUnsupported
	}
	baseURL := strings.TrimRight(ProviderConfigString(provider, "base_url"), "/")
	key := ProviderConfigString(provider, "api_key")
	client := models.NewProviderHTTPClient(probeTimeout)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	field := "data"
	if ct == models.ClientTypeGoogleGenerativeAI {
		field = "models"
	}
	client.Transport = metadataTransport{base: client.Transport, listField: field}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	remote, err := listModelMetadata(ctx, baseURL, ModelCredentials{APIKey: key}, ct, client)
	if err != nil {
		return empty, ErrDiscoveryUpstream
	}
	if len(remote) > 10000 {
		return empty, ErrDiscoveryUpstream
	}
	// A catalog describes only exact model IDs returned by this live endpoint.
	// Do not guess from names, SDK default chat types, or protocol compatibility.
	declared := make(map[string][]string)
	if provider.ProviderTemplateID.Valid {
		catalog, err := s.queries.ListProviderTemplateModels(ctx, provider.ProviderTemplateID)
		if err != nil {
			return empty, ErrDiscoveryStorage
		}
		for _, entry := range catalog {
			declared[entry.ModelID] = configStringSlice(providerConfig(entry.Config), "compatibilities")
		}
	}
	out := DiscoverModelsResponse{
		Models: []DiscoveredModel{}, Completeness: "unknown",
		Adapter: DiscoveryAdapter{Type: string(ct), Source: "memoh_adapter"},
	}
	// These are the Images API adapters already selected by image_gen.go. Their
	// presence does not prove that any listed model implements either operation.
	if ct == models.ClientTypeOpenAICompletions || ct == models.ClientTypeOpenAIResponses {
		out.Adapter.ImagesGeneration, out.Adapter.ImagesEdit = true, true
	}
	for _, model := range remote {
		name := model.DisplayName
		if name == "" {
			name = model.ID
		}
		if !safeMetadataText(model.ID, key, baseURL) || !safeMetadataText(name, key, baseURL) {
			return empty, ErrDiscoveryUpstream
		}
		item := DiscoveredModel{ID: model.ID, Name: name, Capabilities: []DiscoveryCapability{}}
		for _, capability := range []string{models.CompatVision, models.CompatToolCall, models.CompatImageOutput, models.CompatImageEdit, models.CompatReasoning, models.CompatFileInput} {
			fact := DiscoveryCapability{Name: capability, Status: "unknown", Source: "unknown"}
			for _, value := range declared[model.ID] {
				if value == capability {
					fact.Status, fact.Source = "declared", "provider_template"
				}
			}
			item.Capabilities = append(item.Capabilities, fact)
		}
		out.Models = append(out.Models, item)
	}
	return out, nil
}

func safeMetadataText(value, key, baseURL string) bool {
	if strings.TrimSpace(value) == "" || len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	// Defend against accidental upstream reflection of the configured secrets.
	return (key == "" || !strings.Contains(value, key)) && (baseURL == "" || !strings.Contains(value, baseURL))
}

// The SDK parses only the provider's model-list fields. This guard additionally
// bounds its body read, refuses redirects/non-GETs, and rejects a missing list
// instead of disguising an error object as an empty catalog. It is used only by
// discovery, so existing import behavior is unchanged.
type metadataTransport struct {
	base      http.RoundTripper
	listField string
}

func (t metadataTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet || !strings.HasSuffix(req.URL.Path, "/models") {
		return nil, ErrDiscoveryUpstream
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, ErrDiscoveryUpstream
	}
	defer func(body io.ReadCloser) { _ = body.Close() }(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, ErrDiscoveryUpstream
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteModelsResponseLimit+1))
	if err != nil || len(body) > remoteModelsResponseLimit {
		return nil, ErrDiscoveryUpstream
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil {
		return nil, ErrDiscoveryUpstream
	}
	list := bytes.TrimSpace(object[t.listField])
	if len(list) == 0 || list[0] != '[' {
		return nil, ErrDiscoveryUpstream
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}
