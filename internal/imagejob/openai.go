package imagejob

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg" // Register the formats admitted by validateImage.
	_ "image/png"  // Register the formats admitted by validateImage.
	"io"
	"net/http"
	"net/url"

	openaiimages "github.com/felinics/twilight/provider/openai/images"
	sdk "github.com/felinics/twilight/sdk"
)

// OpenAIAdapter is constructed only after explicit model/adapter capability
// resolution by the host. An image-output/vision flag alone does not enable edit.
type OpenAIAdapter struct {
	provider      *openaiimages.Provider
	ModelID       string
	ModelIdentity string
	CanGenerate   bool
	CanEdit       bool
}

// NewOpenAIAdapter requires an explicitly configured endpoint and capability;
// it never supplies a default paid provider or infers edit from a model name.
func NewOpenAIAdapter(identity, model, endpoint, key string, client *http.Client, generate, edit bool) (*OpenAIAdapter, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || identity == "" || model == "" {
		return nil, ErrInvalid
	}
	copyClient := http.Client{}
	if client != nil {
		copyClient = *client
	}
	transport := copyClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	copyClient.Transport = imageResponseTransport{base: transport}
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &OpenAIAdapter{provider: openaiimages.New(openaiimages.WithBaseURL(endpoint), openaiimages.WithAPIKey(key), openaiimages.WithHTTPClient(&copyClient)), ModelID: model, ModelIdentity: identity, CanGenerate: generate, CanEdit: edit}, nil
}

type (
	imageResponseTransport struct{ base http.RoundTripper }
	limitedImageBody       struct {
		io.Reader
		io.Closer
	}
)

func (t imageResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		resp.Body = limitedImageBody{Reader: io.LimitReader(resp.Body, int64(base64.StdEncoding.EncodedLen(int(MaxImageBytes)))+(64<<10)), Closer: resp.Body}
	}
	return resp, nil
}

func (a *OpenAIAdapter) Identity() string { return a.ModelIdentity }
func (a *OpenAIAdapter) Supports(mode Mode) bool {
	return a.provider != nil && a.ModelID != "" && ((mode == Generate && a.CanGenerate) || (mode == Edit && a.CanEdit))
}

func (a *OpenAIAdapter) Invoke(ctx context.Context, mode Mode, prompt, size string, inputs []Input) (Image, error) {
	if !a.Supports(mode) {
		return Image{}, ErrUnsupported
	}
	var res *sdk.ImageResult
	var err error
	switch mode {
	case Edit:
		if len(inputs) != 1 {
			return Image{}, ErrInvalid
		}
		// GPT image models return b64_json by default and do not advertise the
		// legacy response_format parameter. Require the configured adapter's
		// base64 result contract; a URL response is unknown, never resubmitted.
		res, err = sdk.EditImage(ctx, sdk.WithImageEditModel(a.provider.EditModel(a.ModelID)), sdk.WithEditPrompt(prompt), sdk.WithEditSize(size), sdk.WithEditN(1), sdk.WithEditImages(sdk.ImageInput{Data: inputs[0].Data, MediaType: inputs[0].Mime, Filename: "input"}))
	case Generate:
		if len(inputs) != 0 {
			return Image{}, ErrInvalid
		}
		res, err = sdk.GenerateImage(ctx, sdk.WithImageGenerationModel(a.provider.GenerationModel(a.ModelID)), sdk.WithImagePrompt(prompt), sdk.WithImageSize(size), sdk.WithImageN(1))
	default:
		return Image{}, ErrUnsupported
	}
	if err != nil {
		return Image{}, err
	}
	if res == nil || len(res.Data) != 1 || res.Data[0].URL != "" || len(res.Data[0].B64JSON) > base64.StdEncoding.EncodedLen(int(MaxImageBytes)) {
		return Image{}, ErrInvalid
	}
	data, err := base64.StdEncoding.Strict().DecodeString(res.Data[0].B64JSON)
	if err != nil {
		return Image{}, ErrInvalid
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Image{}, ErrInvalid
	}
	result := Image{Data: data, Mime: "image/" + format}
	return result, validateImage(result.Data, result.Mime)
}

func validateImage(data []byte, mime string) error {
	if len(data) == 0 || int64(len(data)) > MaxImageBytes {
		return ErrInvalid
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || mime != "image/"+format || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 64<<20 {
		return fmt.Errorf("%w: unsupported image bytes", ErrInvalid)
	}
	return nil
}
