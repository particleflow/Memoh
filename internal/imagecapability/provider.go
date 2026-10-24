package imagecapability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	sdk "github.com/felinics/twilight/sdk"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/providers"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/toolcontext"
)

type selection struct {
	config      Config
	baseURL     string
	model       string
	credentials func(context.Context) (string, error)
}

// Provider implements the existing ToolProvider port outside the Agent core.
// It replaces only the explicitly configured image capability. No config means
// delegation to the original provider, not guessed protocol selection.
type Provider struct {
	fallback  tools.ToolProvider
	media     *media.Service
	http      *http.Client
	resolve   func(context.Context, string) (selection, bool, error)
	authorize func(context.Context, tools.SessionContext) error
}

func New(fallback tools.ToolProvider, settingsSvc *settings.Service, modelsSvc *models.Service, queries dbstore.Queries, mediaSvc *media.Service, authorize func(context.Context, tools.SessionContext) error) *Provider {
	p := &Provider{fallback: fallback, media: mediaSvc, http: models.NewProviderHTTPClient(models.DefaultProviderRequestTimeout), authorize: authorize}
	p.resolve = func(ctx context.Context, botID string) (selection, bool, error) {
		if settingsSvc == nil || modelsSvc == nil || queries == nil || botID == "" {
			return selection{}, false, nil
		}
		bot, err := settingsSvc.GetBot(ctx, botID)
		if err != nil {
			return selection{}, false, errors.New("image settings are unavailable")
		}
		if bot.ImageModelID == "" {
			return selection{}, false, nil
		}
		model, err := modelsSvc.GetByID(ctx, bot.ImageModelID)
		if err != nil {
			return selection{}, false, errors.New("image model is unavailable")
		}
		provider, err := models.FetchProviderByID(ctx, queries, model.ProviderID)
		if err != nil {
			return selection{}, false, errors.New("image provider is unavailable")
		}
		cfg, enabled, err := parseConfig(provider)
		if err != nil || !enabled {
			return selection{}, enabled, err
		}
		if !model.Enable || !provider.Enable || !model.HasCompatibility(models.CompatImageOutput) {
			return selection{}, true, errors.New("configured image capability is unavailable")
		}
		return selection{
			config: cfg, baseURL: providers.ProviderConfigString(provider, "base_url"), model: model.ModelID,
			credentials: func(ctx context.Context) (string, error) {
				credentials, err := providers.NewService(nil, queries, "").ResolveModelCredentials(ctx, provider)
				return credentials.APIKey, err
			},
		}, true, nil
	}
	return p
}

func (p *Provider) Tools(ctx context.Context, session tools.SessionContext) ([]toolexec.Tool, error) {
	selected, enabled, err := p.resolve(ctx, session.BotID)
	if !enabled {
		// Until explicit selection is known, retain legacy discovery: a Bot's
		// configured image ID exposes the tool even if model/provider lookup
		// fails. Such failures belong to execution, not tool enumeration.
		return p.legacyTools(ctx, session)
	}
	if err != nil {
		return nil, err
	}
	list := []toolexec.Tool{{
		Name:        "generate_image",
		Description: "Generate one image using the explicitly configured Images API. Returns the original's persisted content_hash for the existing attachment flow. An unknown outcome or unavailable stored result must not be regenerated automatically.",
		Parameters:  toolexec.SchemaFor[request](toolexec.Strict()),
		Execute: func(exec *toolexec.ToolExecContext, input sdk.ToolArguments) (sdk.ToolOutput, error) {
			var args request
			if err := decode(input, &args, "prompt", "size"); err != nil {
				return sdk.ToolOutput{}, err
			}
			return toolexec.OutputPair(p.execute(exec, session, args, nil))
		},
	}}
	if selected.config.Edits {
		list = append(list, toolexec.Tool{
			Name:        "edit_image",
			Description: "Edit existing images through the explicitly enabled Images API. Input images are content_hash references owned by this Bot; raw paths and URLs are not accepted. Returns the persisted original output. Do not automatically repeat an unknown outcome or regenerate to retry file delivery.",
			Parameters:  toolexec.SchemaFor[editArgs](toolexec.Strict()),
			Execute: func(exec *toolexec.ToolExecContext, input sdk.ToolArguments) (sdk.ToolOutput, error) {
				var args editArgs
				if err := decode(input, &args, "prompt", "size", "images"); err != nil {
					return sdk.ToolOutput{}, err
				}
				if len(args.Images) < 1 || len(args.Images) > 4 {
					return sdk.ToolOutput{}, errors.New("edit_image requires one to four image content hashes")
				}
				return toolexec.OutputPair(p.execute(exec, session, request{Prompt: args.Prompt, Size: args.Size}, args.Images))
			},
		})
	}
	return list, nil
}

func (p *Provider) legacyTools(ctx context.Context, session tools.SessionContext) ([]toolexec.Tool, error) {
	if p.fallback == nil {
		return nil, nil
	}
	list, err := p.fallback.Tools(ctx, session)
	if err != nil {
		return nil, err
	}
	list = append([]toolexec.Tool(nil), list...)
	for i := range list {
		if list[i].Name != "generate_image" {
			continue
		}
		legacy := list[i].Execute
		list[i].Execute = func(exec *toolexec.ToolExecContext, input sdk.ToolArguments) (sdk.ToolOutput, error) {
			// A failed lookup during discovery must not allow a recovered or
			// changed explicit configuration to enter the paid legacy route.
			_, enabled, err := p.resolve(exec.Context, session.BotID)
			if err != nil {
				return sdk.ToolOutput{}, err
			}
			if enabled {
				return sdk.ToolOutput{}, errors.New("image protocol changed; refresh the available tools before execution")
			}
			return legacy(exec, input)
		}
	}
	return list, nil
}

type editArgs struct {
	Prompt string   `json:"prompt" jsonschema:"Description of the requested edit"`
	Size   string   `json:"size,omitempty"`
	Images []string `json:"images" jsonschema:"One to four existing image content_hash values owned by this Bot"`
}

func decode(input sdk.ToolArguments, target any, allowed ...string) error {
	var fields map[string]json.RawMessage
	if err := input.Unmarshal(&fields); err != nil || fields == nil {
		return errors.New("image tool arguments must be an object")
	}
	for key := range fields {
		found := false
		for _, name := range allowed {
			found = found || key == name
		}
		if !found {
			return errors.New("image tool arguments contain an unsupported field")
		}
	}
	if err := input.Unmarshal(target); err != nil {
		return errors.New("image tool arguments have invalid types")
	}
	return nil
}

func (p *Provider) execute(exec *toolexec.ToolExecContext, session tools.SessionContext, args request, refs []string) (any, error) {
	if err := p.checkAccess(exec.Context, session); err != nil {
		return nil, err
	}
	if p.media == nil || !p.media.RequiresPersistence() {
		return nil, errors.New("persistent attachment storage is unavailable")
	}
	args.Prompt = strings.TrimSpace(args.Prompt)
	if args.Prompt == "" || len(args.Prompt) > 32000 || len(args.Size) > 64 {
		return nil, errors.New("image prompt or size is invalid")
	}
	// Resolve again at execution time to observe disable/reconfiguration.
	selected, enabled, err := p.resolve(exec.Context, session.BotID)
	if err != nil || !enabled || (len(refs) > 0 && !selected.config.Edits) {
		return nil, errors.New("configured image capability is unavailable")
	}
	var images []imageBytes
	var total int64
	for _, ref := range refs {
		image, err := p.openImage(exec.Context, session, ref)
		if err != nil {
			return nil, err
		}
		total += int64(len(image.data))
		if total > maxImageBytes {
			return nil, errors.New("combined edit inputs exceed the limit")
		}
		images = append(images, image)
	}
	apiKey, err := selected.credentials(exec.Context)
	if err != nil {
		return nil, errors.New("image provider authorization is unavailable")
	}
	if err := exec.Err(); err != nil {
		return nil, errors.New("image request was canceled before dispatch")
	}
	if err := p.checkAccess(exec.Context, session); err != nil {
		return nil, err
	}
	image, err := invoke(exec.Context, p.http, selected, apiKey, args, images)
	if err != nil {
		// No private provider response, URL, credential, or transport error is
		// returned to the model. A normal structured result avoids retry guards.
		return map[string]any{"state": "unknown", "retryable": false, "tool_call_id": exec.ToolCallID, "note": "The image request has no confirmed result and may already have generated an image. Do not automatically repeat it or switch protocols."}, nil
	}
	if err := p.checkAccess(exec.Context, session); err != nil {
		return map[string]any{"state": "generated", "storage_state": "unconfirmed", "retryable": false, "tool_call_id": exec.ToolCallID, "note": "Image bytes were returned, but current authorization was not confirmed. Do not regenerate."}, nil
	}
	asset, err := p.media.Ingest(exec.Context, media.IngestInput{BotID: session.BotID, Mime: image.mime, Reader: bytes.NewReader(image.data), MaxBytes: maxImageBytes})
	if err != nil {
		return map[string]any{"state": "generated", "storage_state": "unconfirmed", "retryable": false, "tool_call_id": exec.ToolCallID, "note": "Image generation returned bytes but persistence was not confirmed. Do not regenerate to retry file delivery."}, nil
	}
	result := map[string]any{"state": "completed", "content_hash": asset.ContentHash, "media_type": asset.Mime, "size_bytes": asset.SizeBytes, "tool_call_id": exec.ToolCallID}
	if session.CanUseLocalMessagingShortcut() {
		session.Emitter(tools.ToolStreamEvent{
			Type: tools.StreamEventAttachment, ToolCallID: exec.ToolCallID,
			Attachments: []tools.Attachment{{Type: "image", ContentHash: asset.ContentHash, Mime: asset.Mime, Size: asset.SizeBytes, Name: "image"}},
		})
		result["delivered"] = "current_conversation"
	}
	return result, nil
}

func (p *Provider) checkAccess(ctx context.Context, session tools.SessionContext) error {
	if ctx.Err() != nil || p.authorize == nil || session.BotID == "" || session.SessionID == "" || session.UserID == "" {
		return errors.New("image capability requires an authorized Bot conversation")
	}
	if toolcontext.CheckDelegation(ctx) != nil || p.authorize(ctx, session) != nil {
		return errors.New("image capability is not authorized for this conversation")
	}
	return nil
}

func (p *Provider) openImage(ctx context.Context, session tools.SessionContext, hash string) (imageBytes, error) {
	if err := p.checkAccess(ctx, session); err != nil {
		return imageBytes{}, err
	}
	reader, _, err := p.media.Open(ctx, session.BotID, hash)
	if err != nil {
		return imageBytes{}, errors.New("input image reference is unavailable for this Bot")
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, maxImageBytes+1))
	if err != nil {
		return imageBytes{}, errors.New("input image could not be read")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != hash {
		return imageBytes{}, errors.New("input image integrity could not be verified")
	}
	return validateImage(data)
}

func (p *Provider) Usage(ctx context.Context, session tools.SessionContext, available tools.AvailableTools) string {
	_, enabled, err := p.resolve(ctx, session.BotID)
	if !enabled {
		if usage, ok := p.fallback.(tools.ToolUsage); ok {
			return usage.Usage(ctx, session, available)
		}
		return ""
	}
	if err != nil {
		return ""
	}
	if session.CanUseLocalMessagingShortcut() {
		return "Images API tools persist original output bytes and show their attachment in this conversation. Do not send the same attachment here again. Unknown outcomes must not be automatically repeated."
	}
	return "Images API tools persist original output bytes and return content_hash references for the existing attachment flow. They do not automatically show images in this session. Unknown outcomes must not be automatically repeated."
}
