package turn

import (
	"strings"
	"testing"
	"time"
)

// A quote target renders inside the envelope as the same <in-reply-to> child
// the timeline renderer emits: id + sender attributes and a truncated preview
// as the element text, never the quoted message's full body.
func TestFormatUserHeaderRendersReplyRef(t *testing.T) {
	t.Parallel()

	input := UserMessageHeaderInput{
		MessageID:         "inv-1",
		ChannelIdentityID: "ident-1",
		DisplayName:       "Alice",
		Channel:           "web",
		ConversationType:  "private",
		ReplyTo: &UserMessageReplyRef{
			MessageID: "msg-9",
			Sender:    "Research Bot",
			Preview:   "Earlier answer worth replying to",
		},
		Time: time.Date(2026, 11, 3, 4, 0, 0, 0, time.UTC),
	}
	header := FormatUserHeader(input, "a follow-up question")

	want := "<in-reply-to id=\"msg-9\" sender=\"Research Bot\">Earlier answer worth replying to</in-reply-to>"
	if !strings.Contains(header, want) {
		t.Fatalf("header missing in-reply-to line %q in:\n%s", want, header)
	}
	if !strings.Contains(header, "<message") || !strings.Contains(header, "a follow-up question") {
		t.Fatalf("header lost its envelope or query body:\n%s", header)
	}
	// The reply line is structural: unwrapping keeps only what the user wrote.
	if got := UnwrapUserMessageEnvelope(header); got != "a follow-up question" {
		t.Fatalf("unwrap kept structural reply line, got %q", got)
	}
}

// Without a quote target the envelope is byte-identical to the old form: the
// new child stays strictly opt-in for turns that carry a reply.
func TestFormatUserHeaderWithoutReplyRefKeepsLegacyShape(t *testing.T) {
	t.Parallel()

	input := UserMessageHeaderInput{
		MessageID:         "inv-2",
		ChannelIdentityID: "ident-1",
		DisplayName:       "Alice",
		Channel:           "web",
		ConversationType:  "private",
		Time:              time.Date(2026, 11, 3, 4, 0, 0, 0, time.UTC),
	}
	header := FormatUserHeader(input, "plain turn")
	if strings.Contains(header, "in-reply-to") {
		t.Fatalf("reply line leaked into a turn without a quote:\n%s", header)
	}
}
