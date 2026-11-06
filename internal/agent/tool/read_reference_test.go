package tools

import (
	"context"
	"strings"
	"testing"
)

// videoProbeBytes builds a binary fixture whose head carries the mp4 ftyp box.
func videoProbeBytes(brand string, size int) []byte {
	data := make([]byte, size)
	copy(data, []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p'})
	copy(data[8:], brand)
	// Ensure the probe sees binary content.
	data[size-1] = 0x00
	data[size-2] = 0x00
	return data
}

// webmProbeBytes builds a binary fixture starting with the EBML magic.
func webmProbeBytes(size int) []byte {
	data := make([]byte, size)
	copy(data, []byte{0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x86, 0x81})
	data[size-1] = 0x00
	return data
}

func TestReadVideoReferenceResult(t *testing.T) {
	t.Parallel()

	t.Run("media path returns a delegation reference with content hash", func(t *testing.T) {
		t.Parallel()
		provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
			".memoh/media/ab/0123456789abcdef0123456789abcdef01234567.mp4": videoProbeBytes("isom", 4096),
		})}, nil, "")

		result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/.memoh/media/ab/0123456789abcdef0123456789abcdef01234567.mp4"})
		if err != nil {
			t.Fatalf("execRead() error = %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected plain map result without media envelope, got %T", result)
		}
		if got := m["ok"]; got != true {
			t.Fatalf("expected ok=true, got %v", got)
		}
		if got := m["kind"]; got != "video" {
			t.Fatalf("expected kind=video, got %v", got)
		}
		if got := m["mime"]; got != "video/mp4" {
			t.Fatalf("expected video/mp4, got %v", got)
		}
		if got, _ := m["size"].(int64); got != 4096 {
			t.Fatalf("expected size 4096, got %v (%T)", m["size"], m["size"])
		}
		if got := m["content_hash"]; got != "0123456789abcdef0123456789abcdef01234567" {
			t.Fatalf("expected content hash from the storage key, got %v", got)
		}
		if got := m["handoff"]; got != "delegate_to_bot" {
			t.Fatalf("expected handoff=delegate_to_bot, got %v", got)
		}
		if _, hasContent := m["content"]; hasContent {
			t.Fatal("video reference must not carry file content")
		}
	})

	t.Run("workspace path returns a reference without hash or handoff", func(t *testing.T) {
		t.Parallel()
		provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
			"workspace/clip.mp4": videoProbeBytes("isom", 2048),
		})}, nil, "")

		result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/workspace/clip.mp4"})
		if err != nil {
			t.Fatalf("execRead() error = %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected plain map result, got %T", result)
		}
		if got := m["kind"]; got != "video" {
			t.Fatalf("expected kind=video, got %v", got)
		}
		if _, has := m["content_hash"]; has {
			t.Fatalf("non-media path must not carry a content hash, got %v", m["content_hash"])
		}
		if _, has := m["handoff"]; has {
			t.Fatalf("non-media path must not carry a handoff hint, got %v", m["handoff"])
		}
	})

	t.Run("webm sniffed from content", func(t *testing.T) {
		t.Parallel()
		provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
			"workspace/clip.webm": webmProbeBytes(4096),
		})}, nil, "")

		result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/workspace/clip.webm"})
		if err != nil {
			t.Fatalf("execRead() error = %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map result, got %T", result)
		}
		if got := m["kind"]; got != "video" {
			t.Fatalf("expected kind=video, got %v", got)
		}
	})
}

func TestReadVideoUnrecognizedContainer(t *testing.T) {
	t.Parallel()

	garbage := make([]byte, 4096)
	garbage[0] = 0x00
	garbage[1] = 0x01
	provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
		"workspace/clip.mp4": garbage,
	})}, nil, "")

	_, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/workspace/clip.mp4"})
	if err == nil {
		t.Fatal("expected unrecognized container error")
	}
	if !strings.Contains(err.Error(), "video container not recognized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadVideoHashDerivation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want string
	}{
		{"media path", "/data/.memoh/media/ab/0123456789abcdef0123456789abcdef01234567.mp4", "0123456789abcdef0123456789abcdef01234567"},
		{"legacy media path", "/data/media/0123456789abcdef0123456789abcdef01234567.mov", "0123456789abcdef0123456789abcdef01234567"},
		{"workspace path", "/data/workspace/clip.mp4", ""},
		{"relative path", "clip.mp4", ""},
	}
	for _, tc := range cases {
		if got := mediaContentHash(tc.path); got != tc.want {
			t.Fatalf("%s: mediaContentHash(%q) = %q, want %q", tc.name, tc.path, got, tc.want)
		}
	}
}

func TestReadDescriptionMentionsVideoReference(t *testing.T) {
	t.Parallel()

	desc := readToolDescription(t, false)
	if !strings.Contains(desc, "Video files return a reference") {
		t.Fatalf("expected read description to mention the video reference, got:\n%s", desc)
	}
}
