package audio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/models"
)

// Test-only fixtures. No registered provider/model or credential is created.
const (
	asrFixtureProviderID = "10000000-0000-4000-8000-000000000001"
	asrFixtureModelID    = "10000000-0000-4000-8000-000000000002"
	asrFixtureUpstreamID = "synthetic/asr-fixture-not-a-real-model"
)

type asrQueries struct {
	dbstore.Queries           // unexpected reads/writes panic instead of reaching a database
	provider                  sqlc.Provider
	model                     sqlc.GetTranscriptionModelWithProviderRow
	modelReads, providerReads int
}

func (q *asrQueries) GetProviderByID(_ context.Context, id pgtype.UUID) (sqlc.Provider, error) {
	q.providerReads++
	if id != q.provider.ID {
		return sqlc.Provider{}, errors.New("foreign synthetic provider")
	}
	return q.provider, nil
}

func (q *asrQueries) GetTranscriptionModelWithProvider(_ context.Context, id pgtype.UUID) (sqlc.GetTranscriptionModelWithProviderRow, error) {
	q.modelReads++
	if id != q.model.ID {
		return sqlc.GetTranscriptionModelWithProviderRow{}, errors.New("foreign synthetic model")
	}
	return q.model, nil
}

func asrFixture(clientType models.ClientType) *asrQueries {
	providerID, _ := db.ParseUUID(asrFixtureProviderID)
	modelID, _ := db.ParseUUID(asrFixtureModelID)
	return &asrQueries{provider: sqlc.Provider{ID: providerID, ClientType: string(clientType), Enable: true, Config: []byte(`{"api_key":"synthetic-local-fixture","base_url":"https://asr.example.invalid/v1","language":"en","unrelated":"retained"}`)}, model: sqlc.GetTranscriptionModelWithProviderRow{ID: modelID, ProviderID: providerID, ModelID: asrFixtureUpstreamID, Enable: true, Type: "transcription", Config: []byte(`{"language":"zh","temperature":0.2,"response_format":"verbose_json"}`)}}
}

type asrTransport func(*http.Request) (*http.Response, error)

func (f asrTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func asrInstallTransport(t *testing.T, f asrTransport) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
}

func asrResponse(r *http.Request, code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestSharedChatProviderTranscription(t *testing.T) {
	for _, kind := range []models.ClientType{models.ClientTypeOpenAICompletions, models.ClientTypeOpenAIResponses} {
		t.Run(string(kind), func(t *testing.T) {
			for _, mode := range []string{"success", "forbidden", "lost_response", "malformed_response"} {
				t.Run(mode, func(t *testing.T) {
					// Same saved provider identity; native transcription model explicitly selected.
					q := asrFixture(kind)
					q.model.ProviderType = string(kind)
					before, _ := json.Marshal(q.provider)
					calls := 0
					input := []byte{'R', 'I', 'F', 'F', 0, 1, 2, 3, 'W', 'A', 'V', 'E', 0, 255, 7, 9}
					var receivedHash [32]byte
					asrInstallTransport(t, func(r *http.Request) (*http.Response, error) {
						calls++
						if r.Method != http.MethodPost || r.URL.String() != "https://asr.example.invalid/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer synthetic-local-fixture" {
							t.Fatal("unexpected endpoint/method/auth reference")
						}
						if err := r.ParseMultipartForm(1024 * 1024); err != nil {
							t.Fatal(err)
						}
						defer func() {
							if err := r.MultipartForm.RemoveAll(); err != nil {
								t.Error(err)
							}
						}()
						expected := map[string]string{"model": asrFixtureUpstreamID, "language": "ja", "prompt": "synthetic recognition hint", "temperature": "0.2", "response_format": "verbose_json"}
						if len(r.MultipartForm.Value) != len(expected) {
							t.Fatal("unrelated provider config entered request")
						}
						for k, v := range expected {
							if r.FormValue(k) != v {
								t.Errorf("multipart %s=%q want %q", k, r.FormValue(k), v)
							}
						}
						file, header, err := r.FormFile("file")
						if err != nil {
							t.Fatal(err)
						}
						defer func() {
							if err := file.Close(); err != nil {
								t.Error(err)
							}
						}()
						got, err := io.ReadAll(file)
						if err != nil {
							t.Fatal(err)
						}
						if header.Filename != "synthetic.wav" || !bytes.Equal(got, input) {
							t.Fatal("audio bytes/filename changed")
						}
						receivedHash = sha256.Sum256(got)
						switch mode {
						case "forbidden":
							return asrResponse(r, 403, `{"error":{"message":"synthetic denied","type":"permission_error"}}`), nil
						case "lost_response":
							return nil, io.ErrUnexpectedEOF
						case "malformed_response":
							return asrResponse(r, 200, `{"text":`), nil
						default:
							return asrResponse(r, 200, `{"text":"synthetic transcript","language":"ja","duration":0.5,"words":[{"word":"synthetic","start":0,"end":0.5}]}`), nil
						}
					})
					service := NewService(slog.New(slog.DiscardHandler), q, NewRegistry())
					result, err := service.Transcribe(t.Context(), asrFixtureModelID, input, "synthetic.wav", "audio/wav", map[string]any{"language": "ja", "prompt": "synthetic recognition hint"})
					if mode == "success" {
						if err != nil || result.Text != "synthetic transcript" || result.Language != "ja" || result.DurationSeconds != 0.5 || len(result.Words) != 1 {
							t.Fatalf("result=%+v err=%v", result, err)
						}
					} else if err == nil || result != nil {
						t.Fatalf("failure became success: result=%+v err=%v", result, err)
					}
					after, _ := json.Marshal(q.provider)
					if calls != 1 || receivedHash != sha256.Sum256(input) || !bytes.Equal(before, after) {
						t.Fatal("retry, changed bytes or modified configuration")
					}
					t.Logf("mode=%s POST=1 automatic_retry=0 audio_sha256=%x model_ID_preserved=true DB_writes=0", mode, receivedHash)
				})
			}
		})
	}
}

func TestSharedTranscriptionDoesNotRegisterChatOrSynthesis(t *testing.T) {
	registry := NewRegistry()
	for _, kind := range []models.ClientType{models.ClientTypeOpenAICompletions, models.ClientTypeOpenAIResponses} {
		if _, err := registry.Get(kind); err == nil {
			t.Fatal("chat alias entered registry/bootstrap")
		}
		def, err := registry.GetTranscription(kind)
		if err != nil || def.TranscriptionFactory == nil {
			t.Fatalf("transcription protocol: %v", err)
		}
		q := asrFixture(kind)
		q.model.ProviderType = string(kind)
		service := NewService(slog.New(slog.DiscardHandler), q, registry)
		caps, err := service.GetTranscriptionModelCapabilities(t.Context(), asrFixtureModelID)
		if err != nil || len(caps.ConfigSchema.Fields) == 0 {
			t.Fatalf("native config schema unavailable: %v", err)
		}
	}
	for _, def := range registry.List() {
		if def.ClientType == models.ClientTypeOpenAICompletions || def.ClientType == models.ClientTypeOpenAIResponses {
			t.Fatal("chat alias would seed preset models")
		}
	}
	if _, err := registry.GetTranscription(models.ClientTypeEdgeSpeech); apperror.CodeOf(err) != apperror.CodeRequestFieldInvalid {
		t.Fatal("unsupported transcription accepted")
	}
	if _, err := registry.GetTranscription(models.ClientTypeAnthropicMessages); apperror.CodeOf(err) != apperror.CodeRequestFieldInvalid {
		t.Fatal("unsupported chat protocol accepted")
	}
}
