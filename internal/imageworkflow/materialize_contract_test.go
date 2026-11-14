package imageworkflow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/felinics/memoh/internal/imageworkflow"
	"github.com/felinics/memoh/internal/media"
)

// F5: the input directory entry is a hardlink of the canonical original. One
// inode, two names, no second copy.
func TestF5MaterializeHardlinksTheCanonicalOriginal(t *testing.T) {
	root := t.TempDir()
	originals := filepath.Join(root, "originals")
	inputDir := filepath.Join(root, "workflows", "wf-in")
	botDir := filepath.Join(originals, contractBot)
	if err := os.MkdirAll(botDir, 0o750); err != nil {
		t.Fatal(err)
	}
	hash := testHash("canonical")
	storageKey := filepath.Join(hash[:2], hash+".jpg")
	originalPath := filepath.Join(botDir, storageKey)
	if err := os.MkdirAll(filepath.Dir(originalPath), 0o750); err != nil {
		t.Fatal(err)
	}
	payload := []byte("canonical bytes")
	if err := os.WriteFile(originalPath, payload, 0o640); err != nil { //nolint:gosec // G306: the fixture mirrors an original stored at 0640; its mode is never asserted.
		t.Fatal(err)
	}

	materialized, err := imageworkflow.MaterializeInput(context.Background(), imageworkflow.MaterializeRequest{
		OriginalsRoot: originals,
		InputDir:      inputDir,
		Asset: media.Asset{
			ContentHash: hash,
			BotID:       contractBot,
			Mime:        "image/jpeg",
			SizeBytes:   int64(len(payload)),
			StorageKey:  storageKey,
		},
	})
	if err != nil {
		t.Fatalf("MaterializeInput: %v", err)
	}
	if materialized.ContentHash != hash {
		t.Fatalf("materialized hash = %q, want %q", materialized.ContentHash, hash)
	}
	if materialized.Path != filepath.Join(inputDir, hash+".jpg") {
		t.Fatalf("materialized path = %q", materialized.Path)
	}
	originalInfo, err := os.Stat(originalPath)
	if err != nil {
		t.Fatalf("stat original: %v", err)
	}
	linkInfo, err := os.Stat(materialized.Path)
	if err != nil {
		t.Fatalf("stat materialized entry: %v", err)
	}
	if !os.SameFile(originalInfo, linkInfo) {
		t.Fatal("the input entry is a copy, not a hardlink of the original")
	}
	got, err := os.ReadFile(materialized.Path)
	if err != nil {
		t.Fatalf("read materialized entry: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("materialized bytes = %q", got)
	}
}

// F5: the link source is the asset's own BotID, so a cross-Bot dedup hit links
// the original owner's bytes rather than looking under the requesting bot.
func TestF5MaterializeUsesTheResolvedOwnerBot(t *testing.T) {
	root := t.TempDir()
	originals := filepath.Join(root, "originals")
	ownerBot := "55555555-5555-5555-5555-555555555555"
	hash := testHash("cross-bot")
	storageKey := filepath.Join(hash[:2], hash+".png")
	ownerPath := filepath.Join(originals, ownerBot, storageKey)
	if err := os.MkdirAll(filepath.Dir(ownerPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte("owner bytes"), 0o640); err != nil { //nolint:gosec // G306: the fixture mirrors an original stored at 0640; its mode is never asserted.
		t.Fatal(err)
	}
	// The requesting bot has no such file; only the resolved owner does.
	if err := os.MkdirAll(filepath.Join(originals, contractBot), 0o750); err != nil {
		t.Fatal(err)
	}
	materialized, err := imageworkflow.MaterializeInput(context.Background(), imageworkflow.MaterializeRequest{
		OriginalsRoot: originals,
		InputDir:      filepath.Join(root, "workflows", "wf-in"),
		Asset: media.Asset{
			ContentHash: hash,
			BotID:       ownerBot,
			Mime:        "image/png",
			SizeBytes:   11,
			StorageKey:  storageKey,
		},
	})
	if err != nil {
		t.Fatalf("MaterializeInput from a foreign owner: %v", err)
	}
	if _, err := os.Stat(materialized.Path); err != nil {
		t.Fatalf("materialized entry missing: %v", err)
	}
}

// F5: an existing entry for the same hash is left alone and stays one inode.
func TestF5MaterializeIsIdempotent(t *testing.T) {
	root := t.TempDir()
	originals := filepath.Join(root, "originals")
	inputDir := filepath.Join(root, "workflows", "wf-in")
	hash := testHash("again")
	storageKey := filepath.Join(hash[:2], hash+".jpg")
	originalPath := filepath.Join(originals, contractBot, storageKey)
	if err := os.MkdirAll(filepath.Dir(originalPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalPath, []byte("bytes"), 0o640); err != nil { //nolint:gosec // G306: the fixture mirrors an original stored at 0640; its mode is never asserted.
		t.Fatal(err)
	}
	request := imageworkflow.MaterializeRequest{
		OriginalsRoot: originals,
		InputDir:      inputDir,
		Asset: media.Asset{
			ContentHash: hash, BotID: contractBot, Mime: "image/jpeg", SizeBytes: 5, StorageKey: storageKey,
		},
	}
	first, err := imageworkflow.MaterializeInput(context.Background(), request)
	if err != nil {
		t.Fatalf("first MaterializeInput: %v", err)
	}
	second, err := imageworkflow.MaterializeInput(context.Background(), request)
	if err != nil {
		t.Fatalf("second MaterializeInput: %v", err)
	}
	if first.Path != second.Path {
		t.Fatalf("second materialization moved the entry: %q -> %q", first.Path, second.Path)
	}
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		t.Fatalf("read input dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("input dir holds %d entries, want 1", len(entries))
	}
}

// F5: a missing original is an error, never a silent copy or an empty entry.
func TestF5MaterializeFailsOnAMissingOriginal(t *testing.T) {
	root := t.TempDir()
	hash := testHash("absent")
	_, err := imageworkflow.MaterializeInput(context.Background(), imageworkflow.MaterializeRequest{
		OriginalsRoot: filepath.Join(root, "originals"),
		InputDir:      filepath.Join(root, "workflows", "wf-in"),
		Asset: media.Asset{
			ContentHash: hash, BotID: contractBot, Mime: "image/jpeg", SizeBytes: 5,
			StorageKey: filepath.Join(hash[:2], hash+".jpg"),
		},
	})
	if err == nil {
		t.Fatal("MaterializeInput without an original returned no error")
	}
	if errors.Is(err, imageworkflow.ErrNotImplemented) {
		t.Fatalf("MaterializeInput is still the stub: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "workflows", "wf-in", hash+".jpg")); statErr == nil {
		t.Fatal("a failed materialization still left a directory entry")
	}
}

// F5: the entry name is the hash plus the canonical extension. The assertion
// rides on the materialized path name so the whole rule has one red test.
func TestF5MaterializedNameCarriesTheExtension(t *testing.T) {
	root := t.TempDir()
	originals := filepath.Join(root, "originals")
	hash := testHash("heic")
	storageKey := filepath.Join(hash[:2], hash+".heic")
	originalPath := filepath.Join(originals, contractBot, storageKey)
	if err := os.MkdirAll(filepath.Dir(originalPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalPath, []byte("heic bytes"), 0o640); err != nil { //nolint:gosec // G306: the fixture mirrors an original stored at 0640; its mode is never asserted.
		t.Fatal(err)
	}
	materialized, err := imageworkflow.MaterializeInput(context.Background(), imageworkflow.MaterializeRequest{
		OriginalsRoot: originals,
		InputDir:      filepath.Join(root, "workflows", "wf-in"),
		Asset: media.Asset{
			ContentHash: hash, BotID: contractBot, Mime: "image/heic", SizeBytes: 10, StorageKey: storageKey,
		},
	})
	if err != nil {
		t.Fatalf("MaterializeInput: %v", err)
	}
	if filepath.Base(materialized.Path) != hash+".heic" {
		t.Fatalf("materialized name = %q, want %q", filepath.Base(materialized.Path), hash+".heic")
	}
}
