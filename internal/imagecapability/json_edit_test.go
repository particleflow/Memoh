package imagecapability

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/media"
)

func editConfig(t *testing.T, raw string) Config {
	t.Helper()
	cfg, enabled, err := parseConfig(sqlc.Provider{
		ClientType: "openai-completions",
		Config:     []byte(`{"base_url":"https://provider.example.invalid/v1","images_api":` + raw + `}`),
	})
	if err != nil || !enabled {
		t.Fatalf("explicit config: enabled=%v err=%v", enabled, err)
	}
	return cfg
}

func useEditConfig(p *Provider, cfg Config) {
	resolve := p.resolve
	p.resolve = func(ctx context.Context, bot string) (selection, bool, error) {
		s, enabled, err := resolve(ctx, bot)
		s.config = cfg
		return s, enabled, err
	}
}

func TestSDKImageWireContract(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("inputs_%d", count), func(t *testing.T) {
			output := pngBytes(t, 19)
			inputs := [][]byte{pngBytes(t, 12), pngBytes(t, 13)}[:count]
			var calls atomic.Int32
			p, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				operation := "generations"
				if count > 0 {
					operation = "edits"
				}
				if r.Method != http.MethodPost || r.URL.Path != "/v1/images/"+operation || r.Header.Get("Content-Type") != "application/json" {
					t.Error("wrong SDK endpoint/content type")
				}
				var wire map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
					return
				}
				want := map[string]any{"model": "synthetic-selected-image-model", "prompt": "synthetic edit", "size": "1024x1024", "n": 1, "response_format": "b64_json"}
				if count > 0 {
					refs := make([]map[string]string, 0, count)
					for _, input := range inputs {
						refs = append(refs, map[string]string{"image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(input)})
					}
					want["images"] = refs
				}
				actual, _ := json.Marshal(wire)
				expected, _ := json.Marshal(want)
				if !bytes.Equal(actual, expected) {
					t.Error("SDK wire differs from configured model/original bytes")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"model": "synthetic-gateway-label", "data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(output)}}})
			})
			useEditConfig(p, editConfig(t, `{"protocol":"openai-images","edits":true}`))
			args := map[string]any{"prompt": "synthetic edit", "size": "1024x1024"}
			name := "generate_image"
			if count > 0 {
				name = "edit_image"
				refs := make([]string, 0, count)
				for _, input := range inputs {
					asset, err := p.media.Ingest(t.Context(), media.IngestInput{BotID: "synthetic-bot", Mime: "image/png", Reader: bytes.NewReader(input)})
					if err != nil {
						t.Fatal(err)
					}
					refs = append(refs, asset.ContentHash)
				}
				args["images"] = refs
			}
			payload, _ := json.Marshal(args)
			result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, name, string(payload))
			text := result.Result.String()
			if result.IsError || calls.Load() != 1 || !strings.Contains(text, `"state":"completed"`) || !strings.Contains(text, digest(output)) || strings.Contains(text, "synthetic-gateway-label") {
				t.Fatalf("calls=%d result=%s", calls.Load(), text)
			}
			rc, asset, err := p.media.Open(t.Context(), "synthetic-bot", digest(output))
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil || !bytes.Equal(got, output) || asset.Mime != "image/png" {
				t.Fatal("persisted output differs")
			}
			t.Logf("SDK JSON POST=1 inputs=%d original_sha256=%s", count, digest(got))
		})
	}
}

func TestJSONEditAdmissionAndUnknown(t *testing.T) {
	for _, mode := range []string{"mask", "foreign", "revoked", "tool_encoding_override", "tool_default_override", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			var calls, credentials atomic.Int32
			p, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/images/edits" || r.Header.Get("Content-Type") != "application/json" {
					t.Error("wrong edit protocol")
				}
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"PRIVATE_SYNTHETIC_FAILURE"}]}`))
			})
			useEditConfig(p, editConfig(t, `{"protocol":"openai-images","edits":true}`))
			resolve := p.resolve
			p.resolve = func(ctx context.Context, bot string) (selection, bool, error) {
				s, enabled, err := resolve(ctx, bot)
				s.credentials = func(context.Context) (string, error) { credentials.Add(1); return "", nil }
				return s, enabled, err
			}
			owner := "synthetic-bot"
			if mode == "foreign" {
				owner = "other-bot"
			}
			asset, err := p.media.Ingest(t.Context(), media.IngestInput{BotID: owner, Mime: "image/png", Reader: bytes.NewReader(pngBytes(t, 7))})
			if err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"prompt": "synthetic edit", "images": []string{asset.ContentHash}}
			switch mode {
			case "mask":
				args["images"] = []string{"missing-ref"}
				args["mask"] = "missing-mask"
			case "revoked":
				p.authorize = func(context.Context, tools.SessionContext) error { return errors.New("synthetic revoked") }
			case "tool_encoding_override":
				args["edit_encoding"] = "multipart"
			case "tool_default_override":
				args["service_default"] = true
			}
			payload, _ := json.Marshal(args)
			result := call(t, p, tools.SessionContext{BotID: "synthetic-bot"}, "edit_image", string(payload))
			text := result.Result.String()
			if mode == "unknown" {
				if result.IsError || calls.Load() != 1 || credentials.Load() != 1 || !strings.Contains(text, `"state":"unknown"`) || !strings.Contains(text, `"retryable":false`) || strings.Contains(text, "PRIVATE") || strings.Contains(text, "content_hash") {
					t.Fatalf("calls=%d result=%s", calls.Load(), text)
				}
			} else {
				if !result.IsError || calls.Load() != 0 || credentials.Load() != 0 {
					t.Fatalf("calls=%d credentials=%d result=%s", calls.Load(), credentials.Load(), text)
				}
				if mode == "mask" && !strings.Contains(text, "unsupported field") {
					t.Fatal("mask must be rejected before opening inputs")
				}
			}
			t.Logf("POST=%d credential_resolutions=%d error=%v", calls.Load(), credentials.Load(), result.IsError)
		})
	}
}
