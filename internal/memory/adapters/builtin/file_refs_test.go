package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/memory/adapters"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func TestMemoryFileReferenceProjection(t *testing.T) {
	files := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
	files.SetRequirePersistence(true)
	asset, err := files.Ingest(t.Context(), media.IngestInput{BotID: "bot-1", Mime: "text/plain", Reader: strings.NewReader("synthetic reference bytes\n")})
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewGraphRuntime(nil, newFakeWikiStore(), newFakeStore())
	t.Cleanup(func() { _ = runtime.Close() })
	p := NewBuiltinProvider(nil, runtime)
	p.SetFileResolver(files)
	if _, err = p.Add(t.Context(), adapters.AddRequest{BotID: "bot-1", Message: "合成文件引用", Metadata: map[string]any{"content_hash": asset.ContentHash, "path": "/SECRET/path", "url": "https://SECRET.invalid", "owner": "other-bot"}}); err != nil {
		t.Fatal(err)
	}
	result, err := p.CallTool(t.Context(), mcp.ToolSessionContext{BotID: "bot-1"}, adapters.ToolSearchMemory, map[string]any{"query": "合成文件引用"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"file_refs"`) || !strings.Contains(string(raw), asset.ContentHash) {
		t.Fatalf("missing canonical reference in tool result: %s", raw)
	}
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "other-bot") {
		t.Fatalf("metadata leaked: %s", raw)
	}
}

func TestMemoryFileReferencesFailClosed(t *testing.T) {
	root := t.TempDir()
	files := media.NewService(nil, localfs.NewOriginals(root))
	files.SetRequirePersistence(true)
	asset, err := files.Ingest(t.Context(), media.IngestInput{BotID: "owner-bot", Mime: "text/plain", Reader: strings.NewReader("only owner has these synthetic bytes")})
	if err != nil {
		t.Fatal(err)
	}
	p := NewBuiltinProvider(nil, nil)
	p.SetFileResolver(files)
	cases := []struct {
		name, bot  string
		metadata   map[string]any
		wantStatus string
	}{
		{"no attachment", "owner-bot", map[string]any{"topic": "unchanged"}, ""},
		{"valid with untrusted fields", "owner-bot", map[string]any{"content_hash": asset.ContentHash, "owner": "SECRET", "bot_id": "SECRET", "name": "../../SECRET", "mime": "SECRET", "url": "https://SECRET.invalid"}, "available"},
		{"other bot self reports owner", "other-bot", map[string]any{"content_hash": asset.ContentHash, "bot_id": "owner-bot", "owner": "owner-bot"}, "unavailable"},
		{"missing", "owner-bot", map[string]any{"content_hash": strings.Repeat("a", 64)}, "unavailable"},
		{"path", "owner-bot", map[string]any{"content_hash": "../../SECRET"}, "unavailable"},
		{"url", "owner-bot", map[string]any{"content_hash": "https://SECRET.invalid"}, "unavailable"},
		{"uppercase", "owner-bot", map[string]any{"content_hash": strings.ToUpper(asset.ContentHash)}, "unavailable"},
		{"object", "owner-bot", map[string]any{"content_hash": map[string]any{"path": "SECRET"}}, "unavailable"},
		{"empty", "owner-bot", map[string]any{"content_hash": ""}, "unavailable"},
		{"no scope", "", map[string]any{"content_hash": asset.ContentHash}, "unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := []adapters.MemoryItem{{Memory: "unchanged", Metadata: tc.metadata, FileRefs: []adapters.MemoryFileRef{{Status: "available", ContentHash: "FORGED"}}}}
			p.projectFileRefs(t.Context(), tc.bot, items)
			refs := items[0].FileRefs
			if tc.wantStatus == "" {
				if len(refs) != 0 {
					t.Fatal(refs)
				}
				return
			}
			if len(refs) != 1 || refs[0].Status != tc.wantStatus {
				t.Fatal(refs)
			}
			raw, _ := json.Marshal(refs)
			if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "FORGED") || strings.Contains(string(raw), "owner-bot") {
				t.Fatalf("leaked untrusted fields: %s", raw)
			}
			if tc.wantStatus == "unavailable" && string(raw) != `[{"status":"unavailable"}]` {
				t.Fatalf("unavailable locator leaked: %s", raw)
			}
			if tc.wantStatus == "available" && (refs[0].ContentHash != asset.ContentHash || refs[0].Name != asset.ContentHash+".txt" || refs[0].Mime != "text/plain") {
				t.Fatal(refs)
			}
		})
	}
	// The projection must be recomputed; a successful earlier lookup is no grant.
	items := []adapters.MemoryItem{{Metadata: map[string]any{"content_hash": asset.ContentHash}}}
	p.projectFileRefs(t.Context(), "owner-bot", items)
	if items[0].FileRefs[0].Status != "available" {
		t.Fatal(items)
	}
	if err := os.Remove(filepath.Join(root, "owner-bot", asset.StorageKey)); err != nil {
		t.Fatal(err)
	}
	p.projectFileRefs(t.Context(), "owner-bot", items)
	if items[0].FileRefs[0].Status != "unavailable" || items[0].FileRefs[0].ContentHash != "" {
		t.Fatal(items)
	}
}

type failedMemoryFileResolver struct{}

func (failedMemoryFileResolver) Open(context.Context, string, string) (io.ReadCloser, media.Asset, error) {
	return nil, media.Asset{}, errors.New("SECRET provider path/token")
}

func TestMemoryFileReferenceStorageErrorDoesNotLeak(t *testing.T) {
	p := NewBuiltinProvider(nil, nil)
	items := []adapters.MemoryItem{{Metadata: map[string]any{"content_hash": strings.Repeat("a", 64)}}}
	for _, resolver := range []FileResolver{nil, failedMemoryFileResolver{}} {
		p.SetFileResolver(resolver)
		p.projectFileRefs(t.Context(), "bot-1", items)
		raw, _ := json.Marshal(items[0].FileRefs)
		if string(raw) != `[{"status":"unavailable"}]` {
			t.Fatal(string(raw))
		}
	}
}
