package audio

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
)

func TestPlaybackIdentityIsReadOnlyAndBindsEffectiveVoiceModelFormat(t *testing.T) {
	const modelID = "10000000-0000-4000-8000-000000000002"
	id, _ := db.ParseUUID(modelID)
	providerID, _ := db.ParseUUID("10000000-0000-4000-8000-000000000001")
	q := &playbackStreamQueries{provider: sqlc.Provider{ID: providerID, ClientType: string(models.ClientTypeOpenAISpeech), Enable: true, Config: []byte(`{"api_key":"synthetic-only","base_url":"https://fixture.invalid/v1"}`)}, model: sqlc.GetSpeechModelWithProviderRow{ID: id, ProviderID: providerID, ModelID: "configured-model", Enable: true, Config: []byte(`{"voice":"voice-one","response_format":"mp3"}`)}}
	calls := 0
	asrInstallTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		t.Fatal("configuration validation must never call a provider")
		return nil, nil
	})
	service := NewService(slog.New(slog.DiscardHandler), q, NewRegistry())
	first, err := service.PlaybackIdentity(context.Background(), modelID)
	if err != nil || len(first) != 64 {
		t.Fatalf("identity=%q err=%v", first, err)
	}
	q.model.Config = []byte(`{"response_format":"mp3","voice":"voice-one"}`)
	same, err := service.PlaybackIdentity(context.Background(), modelID)
	if err != nil || same != first {
		t.Fatal("JSON ordering changed effective identity")
	}
	for _, config := range []string{`{"voice":"voice-two","response_format":"mp3"}`, `{"voice":"voice-one","response_format":"wav"}`} {
		q.model.Config = []byte(config)
		changed, err := service.PlaybackIdentity(context.Background(), modelID)
		if err != nil || changed == first {
			t.Fatal("voice/format change could replay old audio")
		}
	}
	q.model.Config = []byte(`{"voice":"voice-one","response_format":"mp3"}`)
	q.model.ModelID = "different-model"
	changed, err := service.PlaybackIdentity(context.Background(), modelID)
	if err != nil || changed == first {
		t.Fatal("model change could replay old audio")
	}
	if calls != 0 {
		t.Fatal("metadata generated audio")
	}
}
