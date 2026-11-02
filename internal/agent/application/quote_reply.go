package application

import (
	"context"
	"strings"
)

// quoteReplyPreviewMax mirrors the timeline renderer's reply preview bound:
// the model sees a truncated pointer, never the quoted message's full body.
const quoteReplyPreviewMax = 100

// ResolveQuoteReplyTarget validates an inbound Web quote target against one
// session and returns the pieces the turn needs: the target's role, its
// sender label, and a truncated preview. ok=false means the id names no
// message of that session (missing, foreign, or the service is unavailable);
// the caller drops the quote and keeps the turn rather than rejecting it.
func (s *Service) ResolveQuoteReplyTarget(ctx context.Context, sessionID, replyTo string) (role, sender, preview string, ok bool) {
	target := strings.TrimSpace(replyTo)
	if s == nil || s.messageService == nil || target == "" || strings.TrimSpace(sessionID) == "" {
		return "", "", "", false
	}
	row, err := s.messageService.GetByIDBySession(ctx, strings.TrimSpace(sessionID), target)
	if err != nil || strings.TrimSpace(row.ID) == "" {
		return "", "", "", false
	}
	role = strings.TrimSpace(row.Role)
	sender = strings.TrimSpace(row.SenderDisplayName)
	preview = truncateQuoteReplyPreview(visibleMessageText(row))
	return role, sender, preview, true
}

// truncateQuoteReplyPreview caps a quote preview the way the timeline
// projection does: at most quoteReplyPreviewMax runes plus an ellipsis.
func truncateQuoteReplyPreview(text string) string {
	runes := []rune(text)
	if len(runes) <= quoteReplyPreviewMax {
		return text
	}
	return string(runes[:quoteReplyPreviewMax]) + "…"
}
