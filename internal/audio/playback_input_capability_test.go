package audio

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
)

func TestPlaybackInputCapabilityIsExplicitProtocolReadOnlyAndHonorsDisabledOrSSML(t *testing.T) {
	var dials atomic.Int32
	previous := websocket.DefaultDialer
	copyDialer := *previous
	copyDialer.NetDialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial")
	}
	websocket.DefaultDialer = &copyDialer
	t.Cleanup(func() { websocket.DefaultDialer = previous })
	id, _ := db.ParseUUID("10000000-0000-4000-8000-000000000002")
	providerID, _ := db.ParseUUID("10000000-0000-4000-8000-000000000001")
	q := &playbackStreamQueries{provider: sqlc.Provider{ID: providerID, ClientType: string(models.ClientTypeAlibabaSpeech), Enable: true, Config: []byte(`{"api_key":"synthetic-only","base_url":"wss://fixture.invalid/api-ws/v1/inference/"}`)}, model: sqlc.GetSpeechModelWithProviderRow{ID: id, ProviderID: providerID, ModelID: "configured-model-no-name-heuristic", Enable: true, Config: []byte(`{"voice":"configured-fixture-voice","format":"mp3"}`)}}
	service := NewService(slog.New(slog.DiscardHandler), q, NewRegistry())
	identity, mime, err := service.PlaybackInputInfo(context.Background(), id.String())
	if err != nil || mime != "audio/mpeg" || len(identity) != 64 {
		t.Fatalf("info mime=%s err=%v", mime, err)
	}
	old, err := service.PlaybackIdentity(context.Background(), id.String())
	if err != nil || old != identity {
		t.Fatal("input identity diverged from effective existing config")
	}
	q.model.Config = []byte(`{"voice":"configured-fixture-voice","format":"mp3","enable_ssml":true}`)
	if _, mime, err = service.PlaybackInputInfo(context.Background(), id.String()); err != nil || mime != "" {
		t.Fatal("SSML advertised incremental input")
	}
	q.model.Config = []byte(`{"voice":"configured-fixture-voice","format":"mp3"}`)
	q.provider.Enable = false
	if _, mime, err = service.PlaybackInputInfo(context.Background(), id.String()); err != nil || mime != "" {
		t.Fatal("disabled provider advertised input")
	}
	if _, _, err = service.OpenPlaybackInput(context.Background(), id.String()); err == nil {
		t.Fatal("disabled provider opened input")
	}
	q.provider.Enable = true
	q.provider.ClientType = string(models.ClientTypeOpenAISpeech)
	q.model.ModelID = "cosyvoice-looking-model-name"
	if _, mime, err = service.PlaybackInputInfo(context.Background(), id.String()); err != nil || mime != "" {
		t.Fatal("model name guessed native protocol")
	}
	if dials.Load() != 0 {
		t.Fatalf("metadata caused %d network calls", dials.Load())
	}
}
