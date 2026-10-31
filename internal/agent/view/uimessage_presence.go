package view

import (
	"encoding/json"
	"strings"

	"github.com/felinics/memoh/internal/conversationpresence"
)

// conversationPresence reuses the typed, transient status surface. It never
// creates a text block that could be mistaken for model prose or replayed into
// the formal model context. Existing status clearing also clears this fragment.
func (c *UIMessageStreamConverter) conversationPresence(progress any) []UIMessage {
	raw, err := json.Marshal(progress)
	if err != nil {
		return nil
	}
	var piece conversationpresence.Piece
	if json.Unmarshal(raw, &piece) != nil || piece.Kind != "conversation_ack" || strings.TrimSpace(piece.Text) == "" {
		return nil
	}
	if c.status == nil {
		c.status = &UIMessage{ID: c.allocBlockID(UIMessageStatus, ""), Type: UIMessageStatus}
	}
	c.status.Name = "conversation_ack"
	c.status.Args = map[string]string{"text": piece.Text}
	return []UIMessage{*c.status}
}
