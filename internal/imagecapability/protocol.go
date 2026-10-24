package imagecapability

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	openaiimages "github.com/felinics/twilight/provider/openai/images"
	sdk "github.com/felinics/twilight/sdk"
)

const maxImageBytes int64 = 50 << 20

type imageBytes struct {
	data []byte
	mime string
}

type request struct {
	Prompt string `json:"prompt"`
	Size   string `json:"size,omitempty"`
}

// The pinned SDK owns Images request encoding and response parsing. Input URLs
// are host-built data URIs from authorized Media bytes, never model-supplied URLs.
func invoke(ctx context.Context, client *http.Client, selected selection, apiKey string, args request, images []imageBytes) (imageBytes, error) {
	if strings.TrimSpace(selected.model) == "" {
		return imageBytes{}, errors.New("configured image model is required")
	}
	provider := openaiimages.New(
		openaiimages.WithBaseURL(strings.TrimRight(selected.baseURL, "/")),
		openaiimages.WithAPIKey(apiKey),
		openaiimages.WithHTTPClient(imageHTTPClient(client)),
	)
	var result *sdk.ImageResult
	var err error
	if len(images) == 0 {
		result, err = sdk.GenerateImage(ctx,
			sdk.WithImageGenerationModel(provider.GenerationModel(selected.model)),
			sdk.WithImagePrompt(args.Prompt), sdk.WithImageSize(args.Size),
			sdk.WithImageN(1), sdk.WithImageResponseFormat("b64_json"),
		)
	} else {
		inputs := make([]sdk.ImageInput, 0, len(images))
		for _, image := range images {
			inputs = append(inputs, sdk.ImageInput{URL: "data:" + image.mime + ";base64," + base64.StdEncoding.EncodeToString(image.data)})
		}
		result, err = sdk.EditImage(ctx,
			sdk.WithImageEditModel(provider.EditModel(selected.model)),
			sdk.WithEditPrompt(args.Prompt), sdk.WithEditSize(args.Size),
			sdk.WithEditImages(inputs...), sdk.WithEditN(1), sdk.WithEditResponseFormat("b64_json"),
		)
	}
	if err != nil {
		return imageBytes{}, err
	}
	// Never fetch a URL-only result or treat multiple/absent outputs as confirmed.
	if result == nil || len(result.Data) != 1 || result.Data[0].B64JSON == "" {
		return imageBytes{}, errors.New("images response has no single confirmed byte result")
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Data[0].B64JSON)
	if err != nil {
		return imageBytes{}, errors.New("images response bytes are invalid")
	}
	return validateImage(decoded)
}

func validateImage(data []byte) (imageBytes, error) {
	if len(data) == 0 || int64(len(data)) > maxImageBytes {
		return imageBytes{}, errors.New("image bytes are empty or exceed the limit")
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return imageBytes{data: data, mime: mime}, nil
	default:
		return imageBytes{}, errors.New("image bytes have an unsupported content type")
	}
}
