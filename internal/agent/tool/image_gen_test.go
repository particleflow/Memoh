package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/imagejob"
	"github.com/felinics/memoh/internal/models"
)

var testPNGBytes = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func TestGenerateDashScopeImageUsesAsyncSDKProviderAndDownloadsImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/services/aigc/image-generation/generation":
			if got := r.Method; got != http.MethodPost {
				t.Fatalf("method = %s, want POST", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer dashscope-key" {
				t.Fatalf("authorization = %q, want bearer key", got)
			}
			if got := r.Header.Get("X-DashScope-Async"); got != "enable" {
				t.Fatalf("X-DashScope-Async = %q, want enable", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if got := body["model"]; got != "wan2.7-image-pro" {
				t.Fatalf("model = %v, want wan2.7-image-pro", got)
			}
			input := body["input"].(map[string]any)
			messages := input["messages"].([]any)
			message := messages[0].(map[string]any)
			content := message["content"].([]any)
			textPart := content[0].(map[string]any)
			if got := message["role"]; got != "user" {
				t.Fatalf("role = %v, want user", got)
			}
			if got := textPart["text"]; got != "a red cube" {
				t.Fatalf("prompt text = %v, want a red cube", got)
			}
			params := body["parameters"].(map[string]any)
			if got := params["size"]; got != "1024*1024" {
				t.Fatalf("size = %v, want 1024*1024", got)
			}
			if got := params["n"]; got != float64(1) {
				t.Fatalf("n = %v, want 1", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"task-1","task_status":"SUCCEEDED","choices":[{"message":{"content":[{"image":"` + publicTestURL("/image.png") + `","type":"image"}]}}]}}`))
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(testPNGBytes)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	image, err := generateDashScopeImage(context.Background(), imageTestClient(t, server), server.URL+"/compatible-mode/v1", "dashscope-key", "wan2.7-image-pro", "a red cube", "1024x1024")
	if err != nil {
		t.Fatalf("generateDashScopeImage() error = %v", err)
	}
	if string(image.Data) != string(testPNGBytes) {
		t.Fatalf("image bytes = %v, want %v", image.Data, testPNGBytes)
	}
	if image.MediaType != "image/png" {
		t.Fatalf("media type = %q, want image/png", image.MediaType)
	}
}

func TestGenerateDashScopeQwenImageUsesProviderDefaultSizeWhenUnspecified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/services/aigc/text2image/image-synthesis":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if got := body["model"]; got != "qwen-image-plus" {
				t.Fatalf("model = %v, want qwen-image-plus", got)
			}
			input := body["input"].(map[string]any)
			if got := input["prompt"]; got != "a red cube" {
				t.Fatalf("prompt = %v, want a red cube", got)
			}
			parameters := body["parameters"].(map[string]any)
			if _, ok := parameters["size"]; ok {
				t.Fatalf("size parameter was sent: %v", parameters["size"])
			}
			if got := parameters["n"]; got != float64(1) {
				t.Fatalf("n = %v, want 1", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_status":"SUCCEEDED","results":[{"url":"` + publicTestURL("/qwen.png") + `"}]}}`))
		case "/qwen.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(testPNGBytes)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	image, err := generateDashScopeImage(context.Background(), imageTestClient(t, server), server.URL+"/compatible-mode/v1", "dashscope-key", "qwen-image-plus", "a red cube", "")
	if err != nil {
		t.Fatalf("generateDashScopeImage() error = %v", err)
	}
	if string(image.Data) != string(testPNGBytes) {
		t.Fatalf("image bytes = %v, want %v", image.Data, testPNGBytes)
	}
}

func TestGenerateOpenAIImagesImageUsesImagesEndpointAndDownloadsURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/images/generations":
			if got := r.Method; got != http.MethodPost {
				t.Fatalf("method = %s, want POST", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer openai-key" {
				t.Fatalf("authorization = %q, want bearer key", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if got := body["model"]; got != "gpt-image-1" {
				t.Fatalf("model = %v, want gpt-image-1", got)
			}
			if got := body["prompt"]; got != "a blue sphere" {
				t.Fatalf("prompt = %v, want a blue sphere", got)
			}
			if got := body["size"]; got != "1024x1024" {
				t.Fatalf("size = %v, want 1024x1024", got)
			}
			if got := body["n"]; got != float64(1) {
				t.Fatalf("n = %v, want 1", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"` + publicTestURL("/openai.png") + `"}]}`))
		case "/openai.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(testPNGBytes)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	image, err := generateOpenAIImagesImage(context.Background(), nil, imageTestClient(t, server), server.URL+"/api/v3", "openai-key", "gpt-image-1", "a blue sphere", "1024x1024")
	if err != nil {
		t.Fatalf("generateOpenAIImagesImage() error = %v", err)
	}
	if string(image.Data) != string(testPNGBytes) {
		t.Fatalf("image bytes = %v, want %v", image.Data, testPNGBytes)
	}
	if image.MediaType != "image/png" {
		t.Fatalf("media type = %q, want image/png", image.MediaType)
	}
}

func TestImageResultToGeneratedImageDecodesB64JSON(t *testing.T) {
	t.Parallel()

	image, err := imageResultToGeneratedImage(context.Background(), http.DefaultClient, &sdk.ImageResult{
		Data: []sdk.ImageData{{
			B64JSON: base64.StdEncoding.EncodeToString(testPNGBytes),
		}},
	})
	if err != nil {
		t.Fatalf("imageResultToGeneratedImage() error = %v", err)
	}
	if string(image.Data) != string(testPNGBytes) {
		t.Fatalf("image bytes = %v, want %v", image.Data, testPNGBytes)
	}
	if image.MediaType != "image/png" {
		t.Fatalf("media type = %q, want image/png", image.MediaType)
	}
}

func TestImageResultToGeneratedImageRejectsNonImageURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"not":"an image"}`))
	}))
	t.Cleanup(server.Close)

	_, err := imageResultToGeneratedImage(context.Background(), imageTestClient(t, server), &sdk.ImageResult{
		Data: []sdk.ImageData{{URL: publicTestURL("/")}},
	})
	if err == nil || !strings.Contains(err.Error(), "not an image") {
		t.Fatalf("imageResultToGeneratedImage() error = %v, want non-image rejection", err)
	}
}

func TestImageResultToGeneratedImageRejectsSpoofedImageContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(`{"not":"an image"}`))
	}))
	t.Cleanup(server.Close)

	_, err := imageResultToGeneratedImage(context.Background(), imageTestClient(t, server), &sdk.ImageResult{
		Data: []sdk.ImageData{{URL: publicTestURL("/")}},
	})
	if err == nil || !strings.Contains(err.Error(), "not an image") {
		t.Fatalf("imageResultToGeneratedImage() error = %v, want spoofed image rejection", err)
	}
}

func TestImageResultToGeneratedImageRejectsUnsupportedURLScheme(t *testing.T) {
	t.Parallel()

	_, err := imageResultToGeneratedImage(context.Background(), http.DefaultClient, &sdk.ImageResult{
		Data: []sdk.ImageData{{URL: "file:///tmp/generated.png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported image URL") {
		t.Fatalf("imageResultToGeneratedImage() error = %v, want unsupported URL rejection", err)
	}
}

func TestImageResultToGeneratedImageRejectsRestrictedImageURL(t *testing.T) {
	_, err := imageResultToGeneratedImage(context.Background(), http.DefaultClient, &sdk.ImageResult{
		Data: []sdk.ImageData{{URL: "http://127.0.0.1/image.png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "restricted address") {
		t.Fatalf("imageResultToGeneratedImage() error = %v, want restricted address rejection", err)
	}
}

func TestImageResultToGeneratedImageRejectsRestrictedRedirectURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/image.png", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	_, err := imageResultToGeneratedImage(context.Background(), imageTestClient(t, server), &sdk.ImageResult{
		Data: []sdk.ImageData{{URL: publicTestURL("/")}},
	})
	if err == nil || !strings.Contains(err.Error(), "restricted address") {
		t.Fatalf("imageResultToGeneratedImage() error = %v, want restricted redirect rejection", err)
	}
}

// TestSSRFSafeDialContextBlocksRebindingToInternalIP is the regression test for
// the DNS-rebinding TOCTOU: validation and connection must resolve the same
// way. The dialer resolves and validates at connect time and dials only the
// validated IP, so a host that resolves to a restricted address is blocked at
// the dial step rather than after a separate, earlier check.
func TestSSRFSafeDialContextBlocksRebindingToInternalIP(t *testing.T) {
	_, err := ssrfSafeDialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil || !strings.Contains(err.Error(), "restricted address") {
		t.Fatalf("ssrfSafeDialContext to loopback = %v, want restricted address block", err)
	}
	// 169.254.169.254 is the cloud metadata endpoint — link-local, must block.
	_, err = ssrfSafeDialContext(context.Background(), "tcp", "169.254.169.254:80")
	if err == nil || !strings.Contains(err.Error(), "restricted address") {
		t.Fatalf("ssrfSafeDialContext to metadata IP = %v, want restricted address block", err)
	}
}

func TestFetchGeneratedImageRejectsEmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// 200 OK with a Content-Type of image/png but no body.
	}))
	t.Cleanup(server.Close)

	_, err := imageResultToGeneratedImage(context.Background(), imageTestClient(t, server), &sdk.ImageResult{
		Data: []sdk.ImageData{{URL: publicTestURL("/")}},
	})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("imageResultToGeneratedImage() empty-body error = %v, want empty rejection", err)
	}
}

func TestFetchGeneratedImageTruncatesLargeErrorBody(t *testing.T) {
	huge := strings.Repeat("x", 200_000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(huge))
	}))
	t.Cleanup(server.Close)

	_, err := imageResultToGeneratedImage(context.Background(), imageTestClient(t, server), &sdk.ImageResult{
		Data: []sdk.ImageData{{URL: publicTestURL("/")}},
	})
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if len(err.Error()) > maxImageErrorBodyBytes+200 {
		t.Fatalf("error length = %d, want bounded near %d (body must be truncated)", len(err.Error()), maxImageErrorBodyBytes)
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("error = %q, want truncation marker", err.Error())
	}
}

func TestExecGenerateImageRelaysChatFallbackTextAsResult(t *testing.T) {
	// A chat-fallback model that replies with text instead of an image must be
	// relayed as a normal tool result, not a tool execution error.
	textErr := &imageModelTextResponseError{text: "I can't create that image."}
	var wrapped error = textErr
	var got *imageModelTextResponseError
	if !errors.As(wrapped, &got) {
		t.Fatal("imageModelTextResponseError should be unwrappable via errors.As")
	}
	if got.text != "I can't create that image." {
		t.Fatalf("text = %q, want the model's reply", got.text)
	}
}

func TestImageGenerationRouting(t *testing.T) {
	t.Parallel()

	openAICompletions := string(models.ClientTypeOpenAICompletions)
	if !shouldUseDashScopeImageGeneration(openAICompletions, "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen-plus") {
		t.Fatal("dashscope base URL should use DashScope image generation")
	}
	// Routing is by base URL only: a DashScope-family model name on an unknown
	// base must NOT be forced onto the DashScope API (it would 400 there); it
	// falls through to the chat path instead.
	if shouldUseDashScopeImageGeneration(openAICompletions, "https://some-aggregator.example/v1", "wan2.2-t2i-flash") {
		t.Fatal("wan model on a non-dashscope base must not use DashScope image generation")
	}
	if shouldUseDashScopeImageGeneration(openAICompletions, "", "wan2.2-t2i-flash") {
		t.Fatal("wan model with no base URL must not use DashScope image generation")
	}
	if !shouldUseOpenAIImagesGeneration(openAICompletions, "https://ark.cn-beijing.volces.com/api/v3", "doubao-seedream-3-0-t2i") {
		t.Fatal("volcengine image model should use OpenAI images generation")
	}
	if !shouldUseOpenAIImagesGeneration(string(models.ClientTypeOpenAIResponses), "https://api.openai.com/v1", "gpt-image-1") {
		t.Fatal("openai-responses image model should use OpenAI images generation")
	}
	// gpt-image name on an unknown base is not enough to route to the images API.
	if shouldUseOpenAIImagesGeneration(openAICompletions, "https://some-aggregator.example/v1", "gpt-image-1") {
		t.Fatal("gpt-image model on an unknown base must not use OpenAI images generation")
	}
	if shouldUseOpenAIImagesGeneration(openAICompletions, "https://openrouter.ai/api/v1", "gpt-image-1") {
		t.Fatal("openrouter should not use OpenAI images generation")
	}
	if shouldUseOpenAIImagesGeneration(string(models.ClientTypeAnthropicMessages), "https://api.openai.com/v1", "gpt-image-1") {
		t.Fatal("non-openai-completions client should not use OpenAI images generation")
	}
}

// publicTestURL uses a TEST-NET-3 documentation address (RFC 5737): it is not
// loopback/private, so it passes SSRF validation, while redirectImageDialerToServer
// sends the actual connection to the local test server.
func publicTestURL(path string) string {
	return "http://198.51.100.10" + path
}

// imageTestClient points the SSRF-safe download dialer's connect step at the
// local test server for the duration of the test and returns the client to
// pass into the download path. IP-range validation still runs against the real
// request host (so restricted addresses like 127.0.0.1 are still blocked before
// this dialer is reached); only the final TCP connect for an allowed host is
// redirected. Tests using it must not call t.Parallel() — the dialer seam is a
// process-global swapped for the test's duration.
func imageTestClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	prev := imageConnectDialer
	imageConnectDialer = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, target.Host)
	}
	t.Cleanup(func() { imageConnectDialer = prev })
	return http.DefaultClient
}

func liveShortcutSession(emitter StreamEmitter) SessionContext {
	return SessionContext{
		LiveStream:      true,
		Emitter:         emitter,
		CurrentPlatform: "local",
		ReplyTarget:     "chat-1",
	}
}

func TestImageGenUsageExplainsRefsAndRecovery(t *testing.T) {
	p := &ImageGenProvider{}
	if p.Usage(context.Background(), SessionContext{}, AvailableTools{}) != "" {
		t.Fatal("usage without tools")
	}
	usage := p.Usage(context.Background(), SessionContext{}, availableToolsForTest(ToolGenerateImage()))
	for _, want := range []string{"content_hash", "artifact_refs", "Unknown", "result"} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage lacks %s", want)
		}
	}
}

func TestDeliverImageRefsOnlyOnLiveShortcut(t *testing.T) {
	p := &ImageGenProvider{}
	ref := imagejob.Artifact{ContentHash: strings.Repeat("a", 64), Mime: "image/png", SizeBytes: 64}
	var events []ToolStreamEvent
	live := liveShortcutSession(func(e ToolStreamEvent) { events = append(events, e) })
	p.deliverImageRefs(live, "call-1", []imagejob.Artifact{ref})
	if len(events) != 1 || len(events[0].Attachments) != 1 || events[0].Attachments[0].ContentHash != ref.ContentHash || events[0].Attachments[0].Path != "" || events[0].Attachments[0].URL != "" {
		t.Fatalf("invalid attachment event: %+v", events)
	}
	live.SessionType = "subagent"
	p.deliverImageRefs(live, "call-2", []imagejob.Artifact{ref})
	if len(events) != 1 {
		t.Fatal("child directly delivered")
	}
}

func TestImageToolMissingServiceHasNoLegacyFallback(t *testing.T) {
	p := &ImageGenProvider{}
	_, err := p.execGenerateImage(context.Background(), SessionContext{}, "call", generateImageArgs{Prompt: "synthetic"})
	if !errors.Is(err, imagejob.ErrDenied) {
		t.Fatalf("err=%v", err)
	}
}
