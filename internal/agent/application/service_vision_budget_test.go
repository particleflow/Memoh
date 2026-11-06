package application

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/felinics/memoh/internal/models"
)

func countingStickerService(t *testing.T, opens *int) *Service {
	t.Helper()
	data := movingStickerTGS(t)
	return &Service{assetLoader: &fakeGatewayAssetLoader{
		openFn: func(context.Context, string, string) (io.ReadCloser, string, error) {
			*opens++
			return io.NopCloser(bytes.NewReader(data)), "application/x-tgsticker", nil
		},
		accessPathFn: func(_ context.Context, _, contentHash string) (string, error) {
			return "/data/" + contentHash, nil
		},
	}}
}

// Preparing an image costs real work — an animated sticker is rasterised — and
// a model without vision routes every image to a file reference anyway.
func TestNonVisionModelDoesNotPrepareImages(t *testing.T) {
	opens := 0
	s := countingStickerService(t, &opens)
	req := ChatRequest{BotID: "bot-1", Attachments: []ChatAttachment{
		{Type: "image", ContentHash: "sticker", Mime: "application/x-tgsticker"},
	}}

	merged, err := s.routeAndMergeAttachments(context.Background(),
		models.GetResponse{Model: models.Model{Config: models.ModelConfig{Compatibilities: []string{}}}}, req)
	if err != nil {
		t.Fatalf("routeAndMergeAttachments() error = %v", err)
	}
	if opens != 0 {
		t.Fatalf("asset opened %d time(s) for a model that cannot use images", opens)
	}
	if len(merged) != 1 {
		t.Fatalf("routeAndMergeAttachments() = %d attachments, want the file reference", len(merged))
	}
	item := merged[0].(gatewayAttachment)
	if item.Type != "file" || item.Payload != "/data/sticker" {
		t.Fatalf("routed attachment = %#v, want the original as a file reference", item)
	}

	// The same request on a vision model still renders.
	merged, err = s.routeAndMergeAttachments(context.Background(),
		models.GetResponse{Model: models.Model{Config: models.ModelConfig{
			Compatibilities: []string{models.CompatVision},
		}}}, req)
	if err != nil {
		t.Fatalf("routeAndMergeAttachments() error = %v", err)
	}
	if opens == 0 {
		t.Fatal("a vision model must still get the rendered frames")
	}
	if frames := merged[0].(gatewayAttachment).Frames; len(frames) != animationFrameCount {
		t.Fatalf("Frames = %d, want %d rendered frames", len(frames), animationFrameCount)
	}
}

// A sticker that stays in a session's context is re-prepared on every model
// call, and re-rendering it produces byte-identical frames.
func TestRenderedFramesAreCachedAcrossTurns(t *testing.T) {
	opens := 0
	s := countingStickerService(t, &opens)

	first, _, err := s.inlineStoredImageDataURLs(context.Background(), "bot-1", "sticker", "application/x-tgsticker")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != animationFrameCount || opens != 1 {
		t.Fatalf("first turn: %d frames, %d opens", len(first), opens)
	}
	for turn := range 3 {
		again, _, err := s.inlineStoredImageDataURLs(context.Background(), "bot-1", "sticker", "application/x-tgsticker")
		if err != nil {
			t.Fatal(err)
		}
		if opens != 1 {
			t.Fatalf("turn %d re-read the asset: %d opens", turn+2, opens)
		}
		if len(again) != len(first) {
			t.Fatalf("turn %d: %d frames, want %d", turn+2, len(again), len(first))
		}
		for i := range first {
			if again[i] != first[i] {
				t.Fatalf("turn %d frame %d differs from the cached render", turn+2, i)
			}
		}
	}
}

// Cached frames must not cross bots: the media store is scoped per bot.
func TestFrameCacheIsScopedPerBot(t *testing.T) {
	opens := 0
	s := countingStickerService(t, &opens)
	if _, _, err := s.inlineStoredImageDataURLs(context.Background(), "bot-a", "sticker", "application/x-tgsticker"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.inlineStoredImageDataURLs(context.Background(), "bot-b", "sticker", "application/x-tgsticker"); err != nil {
		t.Fatal(err)
	}
	if opens != 2 {
		t.Fatalf("asset opened %d time(s), want one per bot", opens)
	}
}

func TestFrameCacheEvictsOldestEntries(t *testing.T) {
	var cache animationCache
	for i := range animationCacheEntries + 10 {
		cache.put(animationCacheKey("bot", string(rune('a'+i%26))+string(rune('a'+i/26))), []string{"frame"})
	}
	cache.mu.Lock()
	entries, order, size := len(cache.entries), len(cache.order), cache.bytes
	cache.mu.Unlock()
	if entries > animationCacheEntries || order != entries {
		t.Fatalf("cache holds %d entries with %d in the order list, want at most %d and consistent",
			entries, order, animationCacheEntries)
	}
	if size != entries*len("frame") {
		t.Fatalf("byte accounting = %d, want %d", size, entries*len("frame"))
	}
}
