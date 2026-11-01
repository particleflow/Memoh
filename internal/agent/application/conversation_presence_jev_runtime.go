package application

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/jev"
)

// runConversationPresenceJev is the fast-reply v3 sidecar path (design v3
// §2): prepare-step1 (receipt 'uncertain'), capability, mainWritten, the Jev
// decision window, prepare-step2 (receipt decision + accounting row), then
// the unchanged Coordinator claim/generate/publish pipeline with a
// precomputed decision. The legacy structural path in
// withConversationPresence stays the default when Jev is not configured.
//
//nolint:revive // sideCtx is the sidecar deadline context, deliberately distinct from the parent ctx.
func (s *Service) runConversationPresenceJev(ctx context.Context, runtime *conversationPresenceRuntime, claims *postgresPresenceClaims, req ChatRequest, model *presenceResolvedModel, rc resolvedContext, sideCtx context.Context, source inputsource.Source, done chan<- conversationpresence.Outcome) {
	settings := runtime.options.Jev
	logStage := func(status string, attrs ...any) {
		if s.logger != nil {
			s.logger.InfoContext(ctx, "conversation_presence", append([]any{slog.String("path", "jev"), slog.String("status", status)}, attrs...)...)
		}
	}
	ticket, facts, admittedAt, inserted, err := claims.prepareStep1(sideCtx, req.SourceReplyToMessageID != "")
	if err != nil || !inserted {
		logStage("prepare_skipped", slog.Bool("store_error", err != nil))
		return
	}
	identity := conversationpresence.Identity{ModelUUID: model.model.ID, ModelID: model.model.ModelID, ProviderID: model.model.ProviderID, ClientType: model.provider.ClientType, RuntimeType: "model"}
	verified := declaredPresenceCapabilities(model.model, model.provider)
	if runtime.options.Verify != nil {
		verified = verified && runtime.options.Verify(model.model, model.provider)
	}
	generator, choice, err := prepareConversationPresenceSDK(identity, model.model, model.sdk, verified)
	if err != nil || generator == nil {
		logStage("capability_unavailable", slog.String("reason", choice.Reason))
		return
	}
	// The main request must already have been written to its transport. An
	// auxiliary request cannot take the first upstream concurrency slot.
	select {
	case <-model.mainWritten:
	case <-sideCtx.Done():
		return
	}
	mainWritten := time.Now()

	pullReader, ok := claims.queries.(presencePullReader)
	if !ok {
		logStage("capability_unavailable", slog.String("reason", "pull_reader_unavailable"))
		return
	}
	scope, err := presencePullScopeFor(claims)
	if err != nil {
		logStage("capability_unavailable", slog.String("reason", "scope_unavailable"))
		return
	}
	imagePresent := presenceRequestHasImage(req)
	effort := ""
	if model.sdk.ReasoningConfig != nil {
		effort = model.sdk.ReasoningConfig.Effort
	}
	engine := &presenceJevEngine{
		client:   &jev.Client{Endpoint: settings.Endpoint, APIKey: settings.APIKey, Model: settings.Model, HTTP: presenceJevHTTPClient()},
		pulls:    pullReader,
		settings: settings,
		in: presenceJevInput{
			Text:         req.RawQuery,
			Facts:        facts,
			Reply:        req.SourceReplyToMessageID != "",
			ImagePresent: imagePresent,
			Effort:       effort,
			AdmittedAt:   admittedAt,
		},
		scope:  scope,
		logger: s.logger,
	}
	decisionStart := time.Now()
	outcome := engine.decide(sideCtx)
	if outcome.Superseded || outcome.Result == nil {
		// Window cancellation leaves no accounting row by design (§6.3): the
		// turn has no decision to record; log counters keep the share visible.
		logStage("superseded_during_jev")
		return
	}
	result := outcome.Result
	if finishErr := claims.finalizePresenceDecision(sideCtx, ticket, result, time.Since(decisionStart).Milliseconds()); finishErr != nil && s.logger != nil {
		s.logger.WarnContext(ctx, "conversation_presence_step2_failed", slog.String("error", finishErr.Error()))
	}
	recordOutcome := func(status string) {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := claims.recordPresenceOutcome(finishCtx, ticket, status); err != nil && s.logger != nil {
			s.logger.WarnContext(ctx, "conversation_presence_outcome_write_failed", slog.String("outcome", status), slog.String("error", err.Error()))
		}
	}

	coordinator := conversationpresence.Coordinator{Claims: claims, Capacity: runtime.capacity, Policy: runtime.options.Policy}
	input := conversationpresence.Input{
		Ticket:       ticket,
		Source:       source,
		Facts:        facts,
		Identity:     identity,
		Choice:       choice,
		Phase:        conversationpresence.Thinking,
		BotName:      rc.runConfig.Bot.Name,
		Locale:       "zh",
		Text:         req.RawQuery,
		ImagePresent: imagePresent,
		Precomputed:  &conversationpresence.Decision{Temperature: result.Temperature, Reason: result.Reason},
	}
	var generatorForRun conversationpresence.Generator
	switch {
	case !result.Publishable():
		// Hot decisions still run the Coordinator for its immediate not_cold
		// terminal so the accounting row closes like every other turn.
		generatorForRun = presenceFallbackGenerator{text: presenceFallbackText, notBefore: time.Now()}
	case result.Mode == presenceModeFallbackACK:
		// §7 grace: publish only when the sidecar context still covers
		// mainWritten+1.5s with no formal text; otherwise stay silent.
		if !sideCtxAffordsGrace(sideCtx) {
			logStage("fallback_silent", slog.String("fallback_reason", "budget_exhausted"))
			result.Mode = presenceModeFallbackNoop
			result.FallbackReason = "budget_exhausted"
			generatorForRun = presenceFallbackGenerator{text: presenceFallbackText, notBefore: time.Now()}
			break
		}
		generatorForRun = presenceFallbackGenerator{text: presenceFallbackText, notBefore: mainWritten.Add(presenceFallbackGrace)}
	default:
		// Generation entry guard (§4.4): below 3.3s the window cannot afford
		// the 3.0s floor; do not send a doomed request, record expired.
		if result.Phase != "" {
			input.Phase = result.Phase
		}
		if remaining := presenceWindowRemaining(sideCtx, decisionStart, settings.Timeout); remaining.Seconds() < presenceJevGuardGenerateSeconds {
			logStage("expired", slog.Float64("remaining_seconds", remaining.Seconds()))
			recordOutcome("expired")
			return
		}
		generatorForRun = &presenceDirectedGenerator{
			base:         generator,
			direction:    result.Direction,
			etaBand:      result.ETABand(),
			recentACKs:   result.RecentACKs,
			windowEnd:    decisionStart.Add(settings.Timeout),
			botName:      rc.runConfig.Bot.Name,
			locale:       "zh",
			userMessage:  req.RawQuery,
			points:       result.ContextPoints,
			urgency:      result.Urgency,
			imagePresent: imagePresent,
		}
	}
	coordinator.Generator = generatorForRun
	handle, err := coordinator.Start(sideCtx, input)
	if err != nil {
		return
	}
	select {
	case runOutcome := <-handle.Done:
		if s.logger != nil {
			s.logger.InfoContext(ctx, "conversation_presence", slog.String("path", "jev"), slog.String("status", runOutcome.Status), slog.String("temperature", string(runOutcome.Decision.Temperature)), slog.String("reason", runOutcome.Decision.Reason), slog.String("mode", result.Mode), slog.String("direction", result.Direction), slog.Int("urgency", result.Urgency), slog.String("fallback_reason", result.FallbackReason))
		}
		recordOutcome(runOutcome.Status)
		select {
		case done <- runOutcome:
		case <-ctx.Done():
		}
	case <-ctx.Done():
	}
}

// presenceJevHTTPClient is the shared Jev gateway client: no redirects, no
// cookies, keep-alives only. NAS loopback traffic never needs more.
func presenceJevHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: false}}
}

func sideCtxAffordsGrace(ctx context.Context) bool {
	if deadline, ok := ctx.Deadline(); ok {
		return time.Until(deadline) >= presenceFallbackGrace
	}
	return true
}

// presenceWindowRemaining computes the effective Jev window remainder: the
// configured window shrinks to the sidecar context when mainWritten waits
// consumed most of it (design v3 §2/§4.4).
func presenceWindowRemaining(sideCtx context.Context, decisionStart time.Time, timeout time.Duration) time.Duration {
	remaining := time.Until(decisionStart.Add(timeout))
	if deadline, ok := sideCtx.Deadline(); ok {
		if until := time.Until(deadline); until < remaining {
			remaining = until
		}
	}
	return remaining
}

func presenceRequestHasImage(req ChatRequest) bool {
	for _, attachment := range req.Attachments {
		if attachment.Type == "image" || strings.HasPrefix(attachment.Mime, "image/") {
			return true
		}
	}
	return false
}

func presencePullScopeFor(claims *postgresPresenceClaims) (presencePullScope, error) {
	sessionID, err := db.ParseUUID(claims.handle.SessionID)
	if err != nil {
		return presencePullScope{}, err
	}
	runID, err := db.ParseUUID(claims.handle.RunID)
	if err != nil {
		return presencePullScope{}, err
	}
	turnID, err := db.ParseUUID(claims.handle.TurnID)
	if err != nil {
		turnID = pgtype.UUID{}
	}
	return presencePullScope{SessionID: sessionID, RunID: runID, TurnID: turnID, ActorID: claims.actor}, nil
}
