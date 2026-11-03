package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func TestOriginalAdmissionAcceptsOwnedAndReferencedHashesButNotUnknown(t *testing.T) {
	root := t.TempDir()
	service := media.NewService(nil, localfs.NewOriginals(root))
	service.SetRequirePersistence(true)
	handler := &LocalChannelHandler{mediaService: service}
	attachment := turn.Attachment{
		Type: "file", Name: "original.txt", Mime: "text/plain",
		Base64: "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("private original")),
	}
	prepared, err := handler.ingestWSInboundAttachments(context.Background(), "bot-a", []turn.Attachment{attachment})
	if err != nil || len(prepared) != 1 || prepared[0].ContentHash == "" || prepared[0].Base64 != "" {
		t.Fatalf("upload was not replaced with an original reference: %+v, %v", prepared, err)
	}
	// A hash another Bot owns resolves by reference: the message keeps the
	// hash identity, the owner keeps the only canonical copy, and nothing is
	// written for the referencing Bot.
	referenced, err := handler.ingestWSInboundAttachments(context.Background(), "bot-b", prepared)
	if err != nil || len(referenced) != 1 || referenced[0].ContentHash != prepared[0].ContentHash {
		t.Fatalf("other Bot's reference was refused: %+v, %v", referenced, err)
	}
	if _, err := os.Stat(filepath.Join(root, "bot-b")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("referencing admission wrote canonical bytes: %v", err)
	}
	unknown := prepared[0]
	unknown.ContentHash = strings.Repeat("c", 64)
	if _, err := handler.ingestWSInboundAttachments(context.Background(), "bot-b", []turn.Attachment{unknown}); err == nil {
		t.Fatal("unknown hash reference was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := handler.ingestWSInboundAttachments(ctx, "bot-a", []turn.Attachment{attachment}); err == nil || result != nil {
		t.Fatal("canceled attachment was accepted")
	}
	badRoot := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(badRoot, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	broken := media.NewService(nil, localfs.NewOriginals(badRoot))
	broken.SetRequirePersistence(true)
	admission := &wsMessageAdmission{
		handler: &LocalChannelHandler{mediaService: broken}, botID: "bot-a",
		attachments: []turn.Attachment{attachment},
	}
	view, err := admission.build(context.Background(), sessionruntime.RunHandle{})
	if err == nil || view.RequestUserTurn != nil || admission.attachmentsPrepared {
		t.Fatalf("failed upload admitted a sendable user turn: %+v, %v", view, err)
	}
}

func TestOriginalOutboundPreservesMetadataAndNeverFallsBackToInlineBytes(t *testing.T) {
	root := t.TempDir()
	service := media.NewService(nil, localfs.NewOriginals(root))
	service.SetRequirePersistence(true)
	handler := &LocalChannelHandler{mediaService: service}
	event := json.RawMessage(`{"type":"attachment_delta","attachments":[{"type":"file","base64":"data:text/plain;base64,dGVzdA==","mime":"text/plain","annotation":"keep"}]}`)
	result := handler.wsIngestAttachments(context.Background(), "bot-a", event)
	if len(result) != 1 || !strings.Contains(string(result[0]), `"annotation":"keep"`) ||
		!strings.Contains(string(result[0]), `"content_hash"`) || strings.Contains(string(result[0]), "base64") {
		t.Fatalf("outbound original reference: %s", result)
	}
	if err := os.WriteFile(filepath.Join(root, "bot-blocked"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	result = handler.wsIngestAttachments(context.Background(), "bot-blocked", event)
	if len(result) != 1 || !strings.Contains(string(result[0]), `"type":"error"`) ||
		strings.Contains(string(result[0]), "base64") || strings.Contains(string(result[0]), root) {
		t.Fatalf("failed original leaked attachment bytes or diagnostic path: %s", result)
	}
}
