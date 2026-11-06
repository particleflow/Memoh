package inbound

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/felinics/memoh/internal/channel"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func TestInboundOriginalPersistenceFailureStopsAttachmentAdmission(t *testing.T) {
	root := filepath.Join(t.TempDir(), "unavailable")
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	service := media.NewService(nil, localfs.NewOriginals(root))
	service.SetRequirePersistence(true)
	p := &ChannelInboundProcessor{mediaService: service}
	attachments := []channel.Attachment{{
		Type: channel.AttachmentType("file"), Name: "test.txt", Mime: "text/plain",
		Base64: "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("test original")),
	}}
	got, err := p.ingestInboundAttachments(context.Background(), channel.ChannelConfig{}, channel.InboundMessage{}, "bot-a", attachments)
	if err == nil || got != nil {
		t.Fatalf("failed persistence admitted inline bytes: %+v, %v", got, err)
	}
}
