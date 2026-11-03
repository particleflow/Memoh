package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// B1: the same bytes ingested by a second Bot must not produce a second
// canonical copy. The ingest returns a reference to the owning Bot, exactly
// like a read does.
func TestIngestSameBytesForAnotherBotReferencesOwner(t *testing.T) {
	t.Parallel()

	f := newCrossBotFixture(t, "bot-owner")
	ctx := context.Background()
	other, err := f.service.Ingest(ctx, IngestInput{BotID: "bot-second", Mime: "image/png", Reader: bytes.NewReader(f.payload)})
	if err != nil {
		t.Fatal(err)
	}
	if other.BotID != "bot-owner" {
		t.Fatalf("second ingest owner = %q; want the first Bot (reference, not copy)", other.BotID)
	}
	if other.ContentHash != f.hash || other.StorageKey != f.storageKey {
		t.Fatalf("second ingest asset = %+v; want the owner's asset", other)
	}
	if _, err := os.Stat(filepath.Join(f.root, "bot-second")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second ingest wrote canonical bytes: %v", err)
	}

	// The reference reads back through the owning Bot's original.
	reader, asset, err := f.service.Open(ctx, "bot-second", f.hash)
	if err != nil || asset.BotID != "bot-owner" {
		t.Fatalf("reference read after re-ingest: %+v, %v", asset, err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, f.payload) {
		t.Fatalf("reference bytes = %q, %v; want the single original", got, err)
	}
}

// The re-ingesting Bot's own first copy keeps its owner: identity, not
// recency, decides where bytes live.
func TestIngestSameBytesForOwnerBotKeepsOwnCopy(t *testing.T) {
	t.Parallel()

	f := newCrossBotFixture(t, "bot-owner")
	ctx := context.Background()
	again, err := f.service.Ingest(ctx, IngestInput{BotID: "bot-owner", Mime: "image/png", Reader: bytes.NewReader(f.payload)})
	if err != nil {
		t.Fatal(err)
	}
	if again.BotID != "bot-owner" || again.StorageKey != f.storageKey {
		t.Fatalf("own re-ingest = %+v; want the same own asset", again)
	}
}

// New content still lands under the ingesting Bot's directory: modification
// and creation keep writing the acting Bot's own canonical store.
func TestIngestNewBytesLandUnderIngestingBot(t *testing.T) {
	t.Parallel()

	f := newCrossBotFixture(t, "bot-owner")
	ctx := context.Background()
	payload := []byte("fresh bytes owned by the acting Bot")
	asset, err := f.service.Ingest(ctx, IngestInput{BotID: "bot-second", Mime: "text/plain", Reader: bytes.NewReader(payload)})
	if err != nil {
		t.Fatal(err)
	}
	if asset.BotID != "bot-second" {
		t.Fatalf("new ingest owner = %q; want the acting Bot", asset.BotID)
	}
	if _, err := os.Stat(filepath.Join(f.root, "bot-second", filepath.FromSlash(asset.StorageKey))); err != nil {
		t.Fatalf("new ingest did not write the acting Bot's store: %v", err)
	}
}
