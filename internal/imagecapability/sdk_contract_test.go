package imagecapability

import (
	"testing"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

func TestSDKConfigCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		valid        bool
	}{
		{"native_model", `{"protocol":"openai-images"}`, true},
		{"references", `{"protocol":"openai-images","edits":true}`, true},
		{"legacy_configured_generation", `{"protocol":"openai-images","model_mode":"configured"}`, true},
		{"legacy_configured_json", `{"protocol":"openai-images","model_mode":"configured","edits":true,"edit_encoding":"json-images"}`, true},
		{"legacy_omit", `{"protocol":"openai-images","model_mode":"omit"}`, false},
		{"legacy_implicit_multipart", `{"protocol":"openai-images","model_mode":"configured","edits":true}`, false},
		{"legacy_multipart", `{"protocol":"openai-images","edits":true,"edit_encoding":"multipart"}`, false},
		{"unknown_mode", `{"protocol":"openai-images","model_mode":"other"}`, false},
		{"unknown_encoding", `{"protocol":"openai-images","edit_encoding":"other"}`, false},
		{"null_mode", `{"protocol":"openai-images","model_mode":null}`, false},
		{"null_encoding", `{"protocol":"openai-images","edit_encoding":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, enabled, err := parseConfig(sqlc.Provider{ClientType: "openai-completions", Config: []byte(`{"base_url":"https://provider.example.invalid/v1","images_api":` + tc.config + `}`)})
			if !enabled || (err == nil) != tc.valid {
				t.Fatalf("explicit=%v valid=%v err=%v", enabled, tc.valid, err)
			}
		})
	}
}
