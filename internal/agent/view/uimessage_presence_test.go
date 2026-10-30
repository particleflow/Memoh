package view

import (
	"testing"

	"github.com/felinics/memoh/internal/conversationpresence"
)

func TestConversationPresenceIsTransientTypedStatus(t *testing.T) {
	converter := NewUIMessageStreamConverter()
	ack := converter.HandleEvent(UIMessageStreamEvent{Type: "progress", Progress: conversationpresence.Piece{Kind: "conversation_ack", Text: "收到，我来梳理。"}})
	if len(ack) != 1 || ack[0].Type != UIMessageStatus || ack[0].Content != "" || ack[0].Args["text"] != "收到，我来梳理。" {
		t.Fatalf("ack became formal text: %+v", ack)
	}
	cleared := converter.HandleEvent(UIMessageStreamEvent{Type: "runtime_status"})
	if len(cleared) != 1 || cleared[0].ID != ack[0].ID || cleared[0].Name != "" {
		t.Fatalf("clear did not target original status: %+v", cleared)
	}
	text := converter.HandleEvent(UIMessageStreamEvent{Type: "text_delta", Delta: "Formal answer."})
	if len(text) != 1 || text[0].Type != UIMessageText || text[0].Content != "Formal answer." {
		t.Fatal(text)
	}
	if got := converter.HandleEvent(UIMessageStreamEvent{Type: "progress", Progress: map[string]any{"kind": "other_progress", "text": "ignored"}}); len(got) != 0 {
		t.Fatalf("unrelated progress exposed: %+v", got)
	}
}
