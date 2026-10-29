package audio

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/models"
)

type playbackStreamQueries struct {
	dbstore.Queries // Unexpected writes/reads panic: this fixture cannot mutate configuration.
	provider        sqlc.Provider
	model           sqlc.GetSpeechModelWithProviderRow
}

func (q *playbackStreamQueries) GetProviderByID(context.Context, pgtype.UUID) (sqlc.Provider, error) {
	return q.provider, nil
}

func (q *playbackStreamQueries) GetSpeechModelWithProvider(context.Context, pgtype.UUID) (sqlc.GetSpeechModelWithProviderRow, error) {
	return q.model, nil
}

func TestPlaybackStreamPreservesConfiguredVoiceAndForwardsShortReadBeforeEOF(t *testing.T) {
	const modelID = "10000000-0000-4000-8000-000000000002"
	providerID, _ := db.ParseUUID("10000000-0000-4000-8000-000000000001")
	id, _ := db.ParseUUID(modelID)
	q := &playbackStreamQueries{
		provider: sqlc.Provider{ID: providerID, ClientType: string(models.ClientTypeOpenAISpeech), Enable: true, Config: []byte(`{"api_key":"synthetic-only","base_url":"https://fixture.invalid/v1","voice":"provider-default"}`)},
		model:    sqlc.GetSpeechModelWithProviderRow{ID: id, ProviderID: providerID, ModelID: "existing-clone-model", Enable: true, Config: []byte(`{"voice":"existing-cloned-voice","response_format":"mp3","speed":1.2}`)},
	}
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	calls := 0
	asrInstallTransport(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPost || r.URL.String() != "https://fixture.invalid/v1/audio/speech" {
			t.Fatal("provider endpoint changed")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "existing-clone-model" || body["voice"] != "existing-cloned-voice" || body["response_format"] != "mp3" || body["speed"] != 1.2 || body["input"] != "visible label" {
			t.Fatalf("configured synthesis changed: %v", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"audio/mpeg"}}, Body: reader, Request: r}, nil
	})
	service := NewService(slog.New(slog.DiscardHandler), q, NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := service.StreamPlayback(ctx, modelID, "visible label")
	if err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() { _, err := writer.Write([]byte("short-first-frame")); written <- err }()
	select {
	case chunk := <-stream.Stream:
		if string(chunk) != "short-first-frame" {
			t.Fatalf("unexpected first bytes %q", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter buffered first short read until full 32KiB or EOF")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for range stream.Stream {
	}
	if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if calls != 1 || stream.ContentType != "audio/mpeg" {
		t.Fatal("unexpected provider calls or format")
	}
}
