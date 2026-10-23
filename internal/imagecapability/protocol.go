package imagecapability

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
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

// invoke performs exactly one POST, with no redirect, retry or chat fallback.
// Any post-dispatch error is uncertain: the upstream may already have billed it.
func invoke(ctx context.Context, client *http.Client, selected selection, apiKey string, args request, images []imageBytes, mask *imageBytes) (imageBytes, error) {
	var body bytes.Buffer
	contentType, operation := "application/json", "generations"
	if len(images) == 0 {
		wire := map[string]any{"prompt": args.Prompt, "n": 1, "response_format": "b64_json"}
		if selected.config.ModelMode == "configured" {
			wire["model"] = selected.model
		}
		if args.Size != "" {
			wire["size"] = args.Size
		}
		if err := json.NewEncoder(&body).Encode(wire); err != nil {
			return imageBytes{}, err
		}
	} else {
		operation = "edits"
		writer := multipart.NewWriter(&body)
		fields := map[string]string{"prompt": args.Prompt, "n": "1", "response_format": "b64_json"}
		if selected.config.ModelMode == "configured" {
			fields["model"] = selected.model
		}
		if args.Size != "" {
			fields["size"] = args.Size
		}
		for name, value := range fields {
			if err := writer.WriteField(name, value); err != nil {
				return imageBytes{}, err
			}
		}
		for _, image := range images {
			name := "image"
			if len(images) > 1 {
				name = "image[]"
			}
			if err := writeImage(writer, name, image); err != nil {
				return imageBytes{}, err
			}
		}
		if mask != nil {
			if err := writeImage(writer, "mask", *mask); err != nil {
				return imageBytes{}, err
			}
		}
		contentType = writer.FormDataContentType()
		if err := writer.Close(); err != nil {
			return imageBytes{}, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(selected.baseURL, operation), &body)
	if err != nil {
		return imageBytes{}, err
	}
	// Do not provide replay semantics or an upstream idempotency claim.
	req.GetBody = nil
	req.Header.Set("Content-Type", contentType)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	oneShot := *client
	oneShot.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// #nosec G704 -- The destination comes only from server-managed provider config
	// validated by parseConfig; tool arguments cannot select it and redirects are disabled.
	resp, err := oneShot.Do(req)
	if err != nil {
		return imageBytes{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return imageBytes{}, errors.New("images request did not return a confirmed result")
	}
	// Bound the encoded response before allocation/decoding. The API contract
	// requests bytes, so response URLs are never fetched as an implicit fallback.
	limit := maxImageBytes*4/3 + (1 << 20)
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return imageBytes{}, errors.New("images response is incomplete or exceeds the limit")
	}
	var result struct {
		Data []struct {
			Base64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil || len(result.Data) != 1 || result.Data[0].Base64 == "" {
		return imageBytes{}, errors.New("images response has no single confirmed byte result")
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Data[0].Base64)
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

func writeImage(writer *multipart.Writer, name string, image imageBytes) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="`+name+`"; filename="image"`)
	header.Set("Content-Type", image.mime)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(image.data)
	return err
}
