package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	alibabaimages "github.com/felinics/twilight/provider/alibabacloud/images"
	openaiimages "github.com/felinics/twilight/provider/openai/images"
	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/imagejob"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/providers"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

const (
	maxGeneratedImageBytes = 50 << 20
	maxImageErrorBodyBytes = 512
)

type generatedImage struct {
	Data      []byte
	MediaType string
}

// imageModelTextResponseError signals that the chat-fallback image model
// replied with text instead of an image (e.g. a refusal or a request for
// clarification). This is not a tool failure — execGenerateImage relays the
// text as a normal tool result so the agent can act on it, rather than
// surfacing it as an execution error that counts against loop/retry guards.
type imageModelTextResponseError struct {
	text string
}

func (e *imageModelTextResponseError) Error() string {
	return "no image generated; model response: " + e.text
}

type ImageGenProvider struct {
	logger         *slog.Logger
	settings       *settings.Service
	models         *models.Service
	queries        dbstore.Queries
	containers     bridge.Provider
	dataMount      string
	imageJobs      *imagejob.Service
	imageAuthority *imagejob.Authority
}

func NewImageGenProvider(
	log *slog.Logger,
	settingsSvc *settings.Service,
	modelsSvc *models.Service,
	queries dbstore.Queries,
	containers bridge.Provider,
	dataMount string,
) *ImageGenProvider {
	if log == nil {
		log = slog.Default()
	}
	return &ImageGenProvider{
		logger:     log.With(slog.String("tool", "image_gen")),
		settings:   settingsSvc,
		models:     modelsSvc,
		queries:    queries,
		containers: containers,
		dataMount:  dataMount,
	}
}

func (p *ImageGenProvider) Tools(ctx context.Context, session SessionContext) ([]toolexec.Tool, error) {
	if p.settings == nil || p.models == nil || p.queries == nil {
		return nil, nil
	}
	botID := strings.TrimSpace(session.BotID)
	if botID == "" {
		return nil, nil
	}
	botSettings, err := p.settings.GetBot(ctx, botID)
	if err != nil {
		return nil, nil
	}
	if strings.TrimSpace(botSettings.ImageModelID) == "" {
		return nil, nil
	}
	description := "Generate or edit an image with the Bot configured image model, or read an existing image task result. Edit requires authorized input_refs and an explicitly edit-capable model. Returns ready NAS artifact_refs, never workspace paths or inline image data. Unknown or publication_pending: use mode=result and task_id; never resubmit the business request."
	if session.CanUseLocalMessagingShortcut() {
		description += " The generated image is automatically shown to the user in the current conversation."
	} else {
		description += " The image is not shown to the user automatically."
	}
	return p.imageTools(session, description), nil
}

type generateImageArgs struct {
	Prompt    string   `json:"prompt,omitempty" jsonschema:"Description of the requested generation or edit. Required except for result retrieval."`
	Mode      string   `json:"mode,omitempty" jsonschema:"generate (default), edit, or result; editing never falls back to generation."`
	InputRefs []string `json:"input_refs,omitempty" jsonschema:"Exactly one authorized original content_hash for edit; none for generation."`
	TaskID    string   `json:"task_id,omitempty" jsonschema:"Existing task_id for mode=result; never starts a model request."`
	Size      string   `json:"size,omitempty" jsonschema:"Optional image size, e.g. 1024x1024, 1792x1024, 1024x1792. Leave empty to use the provider default."`
}

func (p *ImageGenProvider) imageTools(session SessionContext, description string) []toolexec.Tool {
	sess := session
	return []toolexec.Tool{toolexec.Define(ToolGenerateImage().String(), description,
		func(execCtx *toolexec.ToolExecContext, args generateImageArgs) (sdk.ToolOutput, error) {
			return toolexec.OutputPair(p.execGenerateImage(execCtx.Context, sess, execCtx.ToolCallID, args))
		},
	)}
}

func (*ImageGenProvider) Usage(_ context.Context, _ SessionContext, available AvailableTools) string {
	ref, ok := available.Ref(ToolGenerateImage())
	if !ok {
		return ""
	}
	return usageSection("Cloud image tasks", []string{ref + " uses the Bot image model. Use edit with the original attachment content_hash; generation is not editing. A chat subagent may call this tool and return artifact_refs to the parent. Only ready references are deliverable; result retrieves pending publication without repeating the model. Do not send base64 or a workspace path. Unknown must not be resubmitted with a new child or tool call."})
}

func (p *ImageGenProvider) SetImageJobs(service *imagejob.Service, authority *imagejob.Authority) {
	p.imageJobs, p.imageAuthority = service, authority
}

func (p *ImageGenProvider) execGenerateImage(ctx context.Context, session SessionContext, toolCallID string, args generateImageArgs) (any, error) {
	if p.imageJobs == nil || p.imageAuthority == nil {
		return nil, imagejob.ErrDenied
	}
	binding, turn, err := p.imageAuthority.Bind(ctx, session.BotID, session.UserID, session.SessionID)
	if err != nil {
		return nil, err
	}
	var result imagejob.Result
	if args.Mode == "result" {
		if args.TaskID == "" || args.Prompt != "" || len(args.InputRefs) != 0 {
			return nil, imagejob.ErrInvalid
		}
		result, err = p.imageJobs.Result(ctx, binding, args.TaskID)
	} else {
		mode := imagejob.Mode(args.Mode)
		if mode == "" {
			mode = imagejob.Generate
		}
		if args.TaskID != "" {
			return nil, imagejob.ErrInvalid
		}
		req := imagejob.Request{Mode: mode, Prompt: strings.TrimSpace(args.Prompt), Size: strings.TrimSpace(args.Size), InputRefs: args.InputRefs}
		req.Key = imagejob.BusinessKey(turn, req)
		var adapter imagejob.Adapter
		adapter, err = p.imageAdapter(ctx, session.BotID, mode)
		if err == nil {
			result, err = p.imageJobs.Submit(ctx, binding, req, adapter)
		}
	}
	if err != nil {
		return nil, err
	}
	p.deliverImageRefs(session, toolCallID, result.Artifacts)
	return result, nil
}

func (*ImageGenProvider) deliverImageRefs(session SessionContext, toolCallID string, refs []imagejob.Artifact) {
	if !session.CanUseLocalMessagingShortcut() || len(refs) == 0 {
		return
	}
	attachments := make([]Attachment, 0, len(refs))
	for _, ref := range refs {
		attachments = append(attachments, Attachment{Type: "image", ContentHash: ref.ContentHash, Mime: ref.Mime, Size: ref.SizeBytes, Name: "image"})
	}
	session.Emitter(ToolStreamEvent{Type: StreamEventAttachment, ToolCallID: toolCallID, Attachments: attachments})
}

// CollectImageResults uses the parent's fresh DB authorization, not a copied
// child binding or untrusted child text. The only IDs supplied here were tracked
// by the image service in this particular child invocation.
func (p *ImageGenProvider) CollectImageResults(ctx context.Context, parent SessionContext, ids []string) ([]imagejob.Artifact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	binding, _, err := p.imageAuthority.Bind(ctx, parent.BotID, parent.UserID, parent.SessionID)
	if err != nil {
		return nil, err
	}
	var refs []imagejob.Artifact
	seen := map[string]bool{}
	for _, id := range ids {
		result, err := p.imageJobs.Result(ctx, binding, id)
		if err != nil {
			return nil, err
		}
		if result.State != "ready" {
			return nil, imagejob.ErrDenied
		}
		for _, ref := range result.Artifacts {
			if !seen[ref.ContentHash] {
				refs = append(refs, ref)
				seen[ref.ContentHash] = true
			}
		}
	}
	p.deliverImageRefs(parent, "", refs)
	return refs, nil
}

func (p *ImageGenProvider) imageAdapter(ctx context.Context, bot string, mode imagejob.Mode) (imagejob.Adapter, error) {
	setting, err := p.settings.GetBot(ctx, bot)
	if err != nil || setting.ImageModelID == "" {
		return nil, imagejob.ErrUnsupported
	}
	model, err := p.models.GetByID(ctx, setting.ImageModelID)
	if err != nil || !model.Enable || !model.HasCompatibility(models.CompatImageOutput) {
		return nil, imagejob.ErrUnsupported
	}
	provider, err := models.FetchProviderByID(ctx, p.queries, model.ProviderID)
	if err != nil || !provider.Enable {
		return nil, imagejob.ErrUnsupported
	}
	canEdit := model.HasCompatibility(models.CompatImageEdit) && (provider.ClientType == string(models.ClientTypeOpenAICompletions) || provider.ClientType == string(models.ClientTypeOpenAIResponses))
	if mode == imagejob.Edit && !canEdit {
		return nil, imagejob.ErrUnsupported
	}
	credentials, err := providers.NewService(nil, p.queries, "").ResolveModelCredentials(ctx, provider)
	if err != nil {
		return nil, imagejob.ErrDenied
	}
	base := providers.ProviderConfigString(provider, "base_url")
	identity := setting.ImageModelID + "/" + model.ProviderID + "/" + model.ModelID + "/" + base
	if canEdit {
		return imagejob.NewOpenAIAdapter(identity, model.ModelID, base, credentials.APIKey, models.NewProviderHTTPClient(models.DefaultProviderRequestTimeout), true, true)
	}
	return &configuredGenerationAdapter{identity: identity, invoke: func(ctx context.Context, prompt, size string) (generatedImage, error) {
		return p.generateImage(ctx, provider, credentials.APIKey, model.ModelID, prompt, size)
	}}, nil
}

type configuredGenerationAdapter struct {
	identity string
	invoke   func(context.Context, string, string) (generatedImage, error)
}

func (a *configuredGenerationAdapter) Identity() string { return a.identity }
func (*configuredGenerationAdapter) Supports(mode imagejob.Mode) bool {
	return mode == imagejob.Generate
}

func (a *configuredGenerationAdapter) Invoke(ctx context.Context, mode imagejob.Mode, prompt, size string, inputs []imagejob.Input) (imagejob.Image, error) {
	if mode != imagejob.Generate || len(inputs) != 0 {
		return imagejob.Image{}, imagejob.ErrUnsupported
	}
	im, err := a.invoke(ctx, prompt, size)
	return imagejob.Image{Data: im.Data, Mime: im.MediaType}, err
}

// generateImage asks the image model for one image. A generated image may be
// billed even when the call that produced it fails, so a call is made again
// never inside this adapter. The
// DashScope task API is not retried at all: its create and poll requests fail
// alike, and creating the task again after a failed poll would generate a
// second image.
func (p *ImageGenProvider) generateImage(ctx context.Context, provider sqlc.Provider, apiKey, modelID, prompt, size string) (generatedImage, error) {
	baseURL := providers.ProviderConfigString(provider, "base_url")
	httpClient := models.NewProviderHTTPClient(models.DefaultProviderRequestTimeout)

	switch {
	case shouldUseDashScopeImageGeneration(provider.ClientType, baseURL, modelID):
		return generateDashScopeImage(ctx, httpClient, baseURL, apiKey, modelID, prompt, size)
	case shouldUseOpenAIImagesGeneration(provider.ClientType, baseURL, modelID):
		return generateOpenAIImagesImage(ctx, p.logger, httpClient, baseURL, apiKey, modelID, prompt, size)
	default:
		return generateChatImage(ctx, p.logger, provider, apiKey, modelID, prompt, size)
	}
}

// shouldUseDashScopeImageGeneration routes to the DashScope native images API
// only when the provider's base URL confirms DashScope. Routing by bare model
// name (e.g. "qwen-image", "wan*") would send the DashScope request format to
// any OpenAI-compatible aggregator that happens to host a same-named model,
// which such aggregators serve through chat completions instead — so the base
// URL is the authoritative signal and the chat fallback handles the rest.
func shouldUseDashScopeImageGeneration(clientType, baseURL, _ string) bool {
	ct := models.ClientType(clientType)
	if ct != models.ClientTypeOpenAICompletions && ct != models.ClientTypeOpenAIResponses {
		return false
	}
	lowerBase := strings.ToLower(strings.TrimSpace(baseURL))
	return strings.Contains(lowerBase, "dashscope") ||
		strings.Contains(lowerBase, "maas.aliyuncs.com")
}

// shouldUseOpenAIImagesGeneration routes to the OpenAI /images/generations API
// only for base URLs known to serve it. As with DashScope, routing by bare
// model name alone is unsafe on unknown bases, so those fall through to the
// chat fallback rather than being forced onto the images endpoint.
func shouldUseOpenAIImagesGeneration(clientType, baseURL, _ string) bool {
	ct := models.ClientType(clientType)
	if ct != models.ClientTypeOpenAICompletions && ct != models.ClientTypeOpenAIResponses {
		return false
	}
	lowerBase := strings.ToLower(strings.TrimSpace(baseURL))
	if strings.Contains(lowerBase, "openrouter.ai") {
		return false
	}
	return strings.Contains(lowerBase, "api.openai.com") ||
		strings.Contains(lowerBase, "volces.com") ||
		strings.Contains(lowerBase, "bytepluses.com") ||
		strings.Contains(lowerBase, "siliconflow")
}

func generateDashScopeImage(ctx context.Context, httpClient *http.Client, baseURL, apiKey, modelID, prompt, size string) (generatedImage, error) {
	opts := []alibabaimages.Option{
		alibabaimages.WithAPIKey(apiKey),
		alibabaimages.WithHTTPClient(httpClient),
	}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, alibabaimages.WithBaseURL(strings.TrimSpace(baseURL)))
	}
	provider := alibabaimages.New(opts...)
	result, err := sdk.GenerateImage(ctx,
		sdk.WithImageGenerationModel(provider.GenerationModel(modelID)),
		sdk.WithImagePrompt(prompt),
		sdk.WithImageSize(size),
		sdk.WithImageN(1),
	)
	if err != nil {
		return generatedImage{}, err
	}
	return imageResultToGeneratedImage(ctx, httpClient, result)
}

func generateOpenAIImagesImage(ctx context.Context, _ *slog.Logger, httpClient *http.Client, baseURL, apiKey, modelID, prompt, size string) (generatedImage, error) {
	opts := []openaiimages.Option{
		openaiimages.WithAPIKey(apiKey),
		openaiimages.WithHTTPClient(httpClient),
	}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, openaiimages.WithBaseURL(strings.TrimRight(strings.TrimSpace(baseURL), "/")))
	}
	provider := openaiimages.New(opts...)
	imageModel := provider.GenerationModel(modelID)
	result, err := sdk.GenerateImage(ctx, sdk.WithImageGenerationModel(imageModel), sdk.WithImagePrompt(prompt), sdk.WithImageSize(size), sdk.WithImageN(1))
	if err != nil {
		return generatedImage{}, err
	}
	return imageResultToGeneratedImage(ctx, httpClient, result)
}

func generateChatImage(ctx context.Context, _ *slog.Logger, provider sqlc.Provider, apiKey, modelID, prompt, size string) (generatedImage, error) {
	if strings.TrimSpace(size) == "" {
		size = "1024x1024"
	}
	sdkModel := models.NewSDKChatModel(models.SDKModelConfig{
		ModelID:               modelID,
		ClientType:            provider.ClientType,
		APIKey:                apiKey,
		BaseURL:               providers.ProviderConfigString(provider, "base_url"),
		ChatCompletionsCompat: providers.ProviderConfigString(provider, models.ChatCompletionsCompatConfigKey),
	})

	userMsg := fmt.Sprintf("Generate an image with the following description. Size: %s\n\n%s", size, prompt)
	system, messages, _ := models.ApplyPromptCache(
		sdkModel,
		providers.ProviderConfigString(provider, "prompt_cache_ttl"),
		"",
		[]sdk.Message{sdk.UserMessage(userMsg)},
		nil,
	)
	request := sdk.Request{
		System:   system,
		Messages: messages,
	}
	result, err := sdkModel.Generate(ctx, request)
	if err != nil {
		return generatedImage{}, err
	}

	if len(result.Files) == 0 {
		if result.Text != "" {
			return generatedImage{}, &imageModelTextResponseError{text: result.Text}
		}
		return generatedImage{}, errors.New("no image was generated by the model")
	}

	file := result.Files[0]
	imgBytes, err := base64.StdEncoding.DecodeString(file.Data)
	if err != nil {
		return generatedImage{}, fmt.Errorf("failed to decode generated image: %w", err)
	}
	return generatedImage{
		Data:      imgBytes,
		MediaType: normalizeImageMediaType(file.MediaType, imgBytes),
	}, nil
}

func imageResultToGeneratedImage(ctx context.Context, httpClient *http.Client, result *sdk.ImageResult) (generatedImage, error) {
	if result == nil || len(result.Data) == 0 {
		return generatedImage{}, errors.New("no image was generated by the model")
	}
	first := result.Data[0]
	if strings.TrimSpace(first.B64JSON) != "" {
		data, err := base64.StdEncoding.DecodeString(first.B64JSON)
		if err != nil {
			return generatedImage{}, fmt.Errorf("failed to decode generated image: %w", err)
		}
		return generatedImage{Data: data, MediaType: normalizeImageMediaType("", data)}, nil
	}
	if strings.TrimSpace(first.URL) != "" {
		return fetchGeneratedImageURL(ctx, httpClient, first.URL)
	}
	return generatedImage{}, errors.New("image response did not include image data or URL")
}

func fetchGeneratedImageURL(ctx context.Context, httpClient *http.Client, rawURL string) (generatedImage, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return generatedImage{}, fmt.Errorf("parse image URL: %w", err)
	}
	if err := validateImageDownloadURL(ctx, parsed); err != nil {
		return generatedImage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return generatedImage{}, fmt.Errorf("create image download request: %w", err)
	}
	resp, err := imageDownloadClient(httpClient).Do(req) //nolint:gosec // G704: provider-returned image URL; the download transport validates every dialed IP against restricted ranges at connect time (ssrfSafeDialContext), closing the DNS-rebinding TOCTOU
	if err != nil {
		return generatedImage{}, fmt.Errorf("download image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGeneratedImageBytes+1))
	if err != nil {
		return generatedImage{}, fmt.Errorf("read image response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return generatedImage{}, fmt.Errorf("download image failed with status %d: %s", resp.StatusCode, truncateForError(string(data), maxImageErrorBodyBytes))
	}
	if len(data) > maxGeneratedImageBytes {
		return generatedImage{}, fmt.Errorf("download image exceeded %d bytes", maxGeneratedImageBytes)
	}
	if len(data) == 0 {
		return generatedImage{}, errors.New("downloaded image response was empty")
	}
	mediaType, ok := detectImageMediaType(resp.Header.Get("Content-Type"), data)
	if !ok {
		return generatedImage{}, fmt.Errorf("downloaded content is not an image: %s", strings.TrimSpace(resp.Header.Get("Content-Type")))
	}
	return generatedImage{
		Data:      data,
		MediaType: mediaType,
	}, nil
}

// imageDownloadClient wraps the provider HTTP client with an SSRF-safe
// transport. IP validation happens inside the dialer (ssrfSafeDialContext), so
// the address that is actually connected to is the one that was checked — this
// closes the DNS-rebinding TOCTOU that a validate-then-Do split leaves open,
// and covers every redirect hop because the transport dials each one.
func imageDownloadClient(base *http.Client) *http.Client {
	clone := &http.Client{
		Transport:     imageDownloadTransport(),
		CheckRedirect: checkImageRedirect,
	}
	if base != nil {
		clone.Timeout = base.Timeout
	}
	return clone
}

func checkImageRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("unsupported redirect scheme: %s", req.URL.Scheme)
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}

func imageDownloadTransport() *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		clone := base.Clone()
		clone.DialContext = ssrfSafeDialContext
		return clone
	}
	return &http.Transport{DialContext: ssrfSafeDialContext}
}

// ssrfSafeDialContext resolves the destination host, rejects any address in a
// restricted range, and dials the validated IP literal directly. Because the
// connection targets the exact IP that passed validation, a short-TTL rebinding
// record cannot swap in an internal address between check and connect. TLS
// verification still uses the original hostname (the transport sets ServerName
// from the request URL, not from the dialed address).
func ssrfSafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve image URL host %s: %w", host, err)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve image URL host %s: no addresses", host)
	}
	var lastErr error
	for _, ip := range ips {
		if isRestrictedImageDownloadIP(ip) {
			lastErr = fmt.Errorf("blocked image URL host %s resolved to restricted address %s", host, ip.String())
			continue
		}
		conn, err := imageConnectDialer(ctx, network, net.JoinHostPort(ip.String(), port))
		if err != nil {
			lastErr = err
			continue
		}
		return conn, nil
	}
	return nil, lastErr
}

// imageConnectDialer performs the TCP connect after an address has passed SSRF
// validation. It is a package variable so tests can redirect the connect to a
// local server while still exercising the real IP-range validation above.
var imageConnectDialer = (&net.Dialer{}).DialContext

// validateImageDownloadURL is a cheap pre-flight check for a clear early error;
// the authoritative IP-range enforcement lives in ssrfSafeDialContext, which
// runs for the initial request and every redirect hop.
func validateImageDownloadURL(_ context.Context, parsed *url.URL) error {
	if parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.TrimSpace(parsed.Hostname()) == "" {
		return fmt.Errorf("unsupported image URL: %s", parsed)
	}
	return nil
}

func isRestrictedImageDownloadIP(ip net.IP) bool {
	return ip == nil ||
		ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}

func detectImageMediaType(_ string, data []byte) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	detected := http.DetectContentType(data)
	if strings.HasPrefix(detected, "image/") {
		return detected, true
	}
	return "", false
}

// truncateForError bounds an untrusted response body before it is embedded in
// an error message, so an expired-URL error page or junk payload cannot balloon
// the tool result, conversation stream, and logs.
func truncateForError(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…(truncated)"
}

func normalizeImageMediaType(mediaType string, data []byte) string {
	if detected, ok := detectImageMediaType(mediaType, data); ok {
		return detected
	}
	return "image/png"
}
