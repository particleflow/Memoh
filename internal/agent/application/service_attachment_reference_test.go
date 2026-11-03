package application

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/runtime/native"
	turnpkg "github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/models"
)

// With the inline content channels closed (TF-16 design section 4, D9 + O-1),
// an attachment turn must resolve with the attachment reaching the model only
// as the header reference row: the gateway routing still resolves the stored
// original's access path, the user header carries an <attachment path> row,
// and nothing materializes image or file parts into the provider messages.
// (RunConfig.InlineImages/InlineAttachments no longer exist — that is the
// compile-time half of this assertion.)
func TestAttachmentTurnResolvesWithoutInlineImages(t *testing.T) {
	s := imageInputService(t, map[string][]byte{"photo": rasterPNG(t)})
	req := ChatRequest{
		BotID:       "bot-1",
		Query:       "这是什么",
		Attachments: []ChatAttachment{{Type: "image", ContentHash: "photo", Mime: "image/png", Name: "photo.png"}},
	}
	model := models.GetResponse{Model: models.Model{Config: models.ModelConfig{
		Compatibilities: []string{models.CompatVision},
	}}}

	merged, err := s.routeAndMergeAttachments(context.Background(), model, req)
	if err != nil {
		t.Fatalf("routeAndMergeAttachments() error = %v", err)
	}
	if len(merged) != 1 {
		t.Fatalf("routeAndMergeAttachments() = %d attachments, want 1", len(merged))
	}
	paths := extractAttachmentPaths(merged)
	if len(paths) != 1 || !strings.HasPrefix(paths[0], "/data/.memoh/media/photo") {
		t.Fatalf("extractAttachmentPaths() = %v, want the stored original's access path", paths)
	}

	headerified := turnpkg.FormatUserHeader(turnpkg.UserMessageHeaderInput{
		MessageID:       "msg-1",
		Channel:         "telegram",
		AttachmentPaths: paths,
	}, req.Query)
	if !strings.Contains(headerified, `<attachment path="`+paths[0]) {
		t.Fatalf("headerified query missing the attachment reference row:\n%s", headerified)
	}
	if !strings.Contains(headerified, req.Query) {
		t.Fatalf("headerified query lost the user text:\n%s", headerified)
	}

	cfg := native.RunConfig{Query: headerified}
	cfg = (&Service{}).prepareRunConfig(context.Background(), cfg)
	for _, message := range cfg.Messages {
		for _, part := range message.Content {
			switch part.(type) {
			case sdk.ImagePart, sdk.FilePart:
				t.Fatalf("inline media part reached the provider messages: %#v", cfg.Messages)
			}
		}
	}
	if !messagesContainText(cfg.Messages, `<attachment path=`) {
		t.Fatalf("provider messages missing the reference row the model reads from:\n%s", headerified)
	}
}

func messagesContainText(messages []sdk.Message, needle string) bool {
	for _, message := range messages {
		for _, part := range message.Content {
			if text, ok := part.(sdk.TextPart); ok && strings.Contains(text.Text, needle) {
				return true
			}
		}
	}
	return false
}
