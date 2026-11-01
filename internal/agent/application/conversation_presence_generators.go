package application

import (
	"context"
	"fmt"
	"time"

	"github.com/felinics/memoh/internal/conversationpresence"
)

// presenceFallbackText is the §7 hardcoded constant acknowledgement
// (approved wording, design v3 §7 / O-5): a presence claim with zero action
// assertion, matching the PreviousSettled trigger.
const presenceFallbackText = "在的，你继续说。"

// presenceDirectedGenerator implements conversationpresence.Generator for the
// fast-reply v3 generated path. The variable slots are built from the same
// server data Jev saw; the two mechanical gates (leading-two-rune dedup and
// the eta duration regex) retry once each and only while the window still
// affords a generation (design v3 §5.2).
type presenceDirectedGenerator struct {
	base         *conversationPresenceSDKGenerator
	direction    string
	etaBand      string
	recentACKs   []string
	windowEnd    time.Time
	botName      string
	locale       string
	userMessage  string
	points       []string
	urgency      int
	imagePresent bool
}

func (g *presenceDirectedGenerator) Generate(ctx context.Context, input conversationpresence.Input, limits conversationpresence.Limits) (conversationpresence.Result, error) {
	slots := buildPresenceSlots(g.botName, g.locale, g.userMessage, g.points, input.Phase, g.imagePresent, g.urgency, g.etaBand, g.recentACKs)
	result, err := g.base.generateDirected(ctx, g.direction, "", slots, input.Phase, limits)
	if err != nil {
		return result, err
	}
	text := result.Text
	if lead, matched := presenceLeadMatches(text, g.recentACKs); matched {
		if g.windowAffords(ctx) {
			// The ban is a separate prompt segment appended after the
			// direction body (design v3 §5.2 gate 1), never part of the
			// direction key, so the direction stays intact on retry.
			retried, retryErr := g.base.generateDirected(ctx, g.direction, presenceBannedLeadSuffix(lead), slots, input.Phase, limits)
			if retryErr == nil {
				// One retry only: keep it when it actually clears the gate;
				// a retry that still collides adds nothing over the first
				// version, so keep the first.
				if _, still := presenceLeadMatches(retried.Text, g.recentACKs); !still {
					text = retried.Text
				}
			}
		}
	}
	if g.direction == "eta" && g.etaBand != "" && !presenceETABandAllows(text, g.etaBand) {
		if g.windowAffords(ctx) {
			if retried, retryErr := g.base.generateDirected(ctx, g.direction, "", slots, input.Phase, limits); retryErr == nil && presenceETABandAllows(retried.Text, g.etaBand) {
				text = retried.Text
			} else {
				text = "估计" + g.etaBand + "。"
			}
		} else {
			text = "估计" + g.etaBand + "。"
		}
	}
	return conversationpresence.Result{Text: text, Phase: input.Phase, Usage: result.Usage}, nil
}

// windowAffords reports whether at least the generation entry budget remains
// (design v3 §5.2: retries only while the window can still afford them).
func (g *presenceDirectedGenerator) windowAffords(ctx context.Context) bool {
	deadline := g.windowEnd
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	return time.Until(deadline).Seconds() >= presenceJevGuardGenerateSeconds
}

func presenceBannedLeadSuffix(lead string) string {
	return "\n额外禁令（本轮机械查重命中）：不得以「" + lead + "」开头。"
}

// presenceFallbackGenerator is the §7 constant generator: it returns the
// fixed text after waiting out the publish grace (mainWritten + 1.5s with no
// formal text — main text cancels the sidecar context first, so cancellation
// here is the normal superseded path).
type presenceFallbackGenerator struct {
	text      string
	notBefore time.Time
}

func (g presenceFallbackGenerator) Generate(ctx context.Context, input conversationpresence.Input, _ conversationpresence.Limits) (conversationpresence.Result, error) {
	if wait := time.Until(g.notBefore); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return conversationpresence.Result{}, fmt.Errorf("presence fallback grace: %w", ctx.Err())
		}
	}
	return conversationpresence.Result{Text: g.text, Phase: input.Phase}, nil
}
