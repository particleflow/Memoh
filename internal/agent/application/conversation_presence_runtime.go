package application

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"

	agentevent "github.com/felinics/memoh/internal/agent/event"
	"github.com/felinics/memoh/internal/agent/runtime/native"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/reasoning"
)

type ConversationPresenceOptions struct {
	Policy                   conversationpresence.Policy
	MaxConcurrentPerProvider int
	// Jev, when its endpoint is set, switches the sidecar to the fast-reply v3
	// decision path (two-stage Jev selection, pulls, accounting, §7 fallback).
	// A zero endpoint keeps the structural Classify fallback (issue #105).
	Jev PresenceJevRuntimeOptions
	// GenerationModel overrides the sidecar generation model ref. Resolution
	// goes through GenerationModelResolver and is only honored inside the main
	// selection's provider; any failure or mismatch keeps the main selection.
	GenerationModel string
	// GenerationModelResolver resolves a generation model ref from the live
	// catalog. Host wiring supplies it; tests supply stubs. nil disables the
	// override entirely.
	GenerationModelResolver func(context.Context, string) (models.GetResponse, error)
	// Verify can narrow a host's capability contract. By default use explicit
	// declarations from the model/provider selected for this formal request.
	// This is catalog evidence, not a claim that an opaque gateway obeyed it.
	Verify func(models.GetResponse, sqlc.Provider) bool
}

// presenceGenerationSelection is the effective generation model for one
// sidecar run.
type presenceGenerationSelection struct {
	model    models.GetResponse
	provider sqlc.Provider
	sdk      models.SDKModelConfig
	override bool
}

// resolvePresenceGeneration picks the sidecar generation model. Default is the
// main selection (design v3 §5.2). With a configured ref the override must
// resolve to the same provider as main, so credentials, transport and the
// per-provider capacity key all stay the main provider's — an infrastructure
// channel for a faster acknowledgement model, never a new provider and never a
// Bot-side surface. The 2026-11-02 08:05 incident showed the main model's
// first-token floor (GLM-5.3 low: 4.8-6.8s observed regardless of the already
// bounded ~1.5k-token ack input) can outrun the remaining 10s window.
func (s *Service) resolvePresenceGeneration(ctx context.Context, runtime *conversationPresenceRuntime, main *presenceResolvedModel) presenceGenerationSelection {
	selection := presenceGenerationSelection{model: main.model, provider: main.provider, sdk: main.sdk}
	ref := strings.TrimSpace(runtime.options.GenerationModel)
	if ref == "" || runtime.options.GenerationModelResolver == nil || ref == strings.TrimSpace(main.model.ModelID) {
		return selection
	}
	auxModel, err := runtime.options.GenerationModelResolver(ctx, ref)
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "conversation_presence_generation_model_unavailable", slog.String("ref", ref), slog.String("error", err.Error()))
		}
		return selection
	}
	if auxModel.ProviderID != main.model.ProviderID {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "conversation_presence_generation_model_provider_mismatch", slog.String("ref", ref), slog.String("main_provider", main.model.ProviderID), slog.String("model_provider", auxModel.ProviderID))
		}
		return selection
	}
	// Same provider: reuse the resolved SDK credentials and transport, swap
	// only the wire model id.
	auxSDK := main.sdk
	auxSDK.ModelID = auxModel.ModelID
	selection.model, selection.sdk, selection.override = auxModel, auxSDK, true
	return selection
}

// ResolvePresenceGenerationModel is the host catalog resolver behind
// ConversationPresenceOptions.GenerationModelResolver.
func (s *Service) ResolvePresenceGenerationModel(ctx context.Context, ref string) (models.GetResponse, error) {
	model, _, err := s.fetchChatModel(ctx, ref)
	return model, err
}

func declaredPresenceCapabilities(model models.GetResponse, provider sqlc.Provider) bool {
	if !presenceHasFixedModel(provider.ClientType, model.ModelID) {
		return false
	}
	if model.Config.CatalogAvailable != nil && !*model.Config.CatalogAvailable {
		return false
	}
	switch models.ClientType(provider.ClientType) {
	case models.ClientTypeOpenAICompletions, models.ClientTypeOpenAIResponses, models.ClientTypeOpenAICodex, models.ClientTypeGitHubCopilot, models.ClientTypeAnthropicMessages, models.ClientTypeOpenCodeGo:
	case models.ClientTypeGoogleGenerativeAI:
		if model.Config.ThinkingMode != reasoning.ModeNone && model.Config.ReasoningDialect == "" {
			return false
		}
	default:
		return false
	}
	if model.Config.ThinkingMode == reasoning.ModeNone {
		return true
	}
	switch model.Config.ThinkingMode {
	case reasoning.ModeAlways, reasoning.ModeToggle, reasoning.ModeAdaptive:
		return len(model.Config.ReasoningEfforts) > 0
	default:
		return false
	}
}

type conversationPresenceRuntime struct {
	options  ConversationPresenceOptions
	capacity *presenceCapacity
}

type presenceResolvedModel struct {
	model       models.GetResponse
	provider    sqlc.Provider
	sdk         models.SDKModelConfig
	mainWritten <-chan struct{}
}

func (s *Service) SetConversationPresence(options ConversationPresenceOptions) {
	if options.MaxConcurrentPerProvider <= 0 {
		s.conversationPresence = nil
		return
	}
	s.conversationPresence = &conversationPresenceRuntime{options: options, capacity: &presenceCapacity{limit: options.MaxConcurrentPerProvider, active: map[string]int{}}}
}

type presenceCapacity struct {
	mu     sync.Mutex
	limit  int
	active map[string]int
}

func (c *presenceCapacity) TryAcquire(ctx context.Context, identity conversationpresence.Identity) (func(), bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Err() != nil || c.active[identity.ProviderID] >= c.limit {
		return nil, false
	}
	c.active[identity.ProviderID]++
	var once sync.Once
	return func() { once.Do(func() { c.mu.Lock(); c.active[identity.ProviderID]--; c.mu.Unlock() }) }, true
}

func presenceEligible(req ChatRequest) bool {
	return req.RunHandle.FencingToken > 0 && req.RunHandle.RunID != "" && req.RunHandle.OwnerID != "" &&
		req.TurnReplacement == nil && !req.ShutdownResume && !req.ReusePersistedUserMessage && !req.SkipHistoryTurn &&
		requestInputSource(req).Kind == inputsource.User && req.SessionType != "schedule" && req.SessionType != "subagent"
}

type presenceTransport struct {
	base    http.RoundTripper
	once    sync.Once
	written chan struct{}
}

func (t *presenceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	trace := &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			t.once.Do(func() { close(t.written) })
		}
	}}
	return t.base.RoundTrip(req.WithContext(httptrace.WithClientTrace(req.Context(), trace)))
}

func (s *Service) presenceModelHook(req ChatRequest, target **presenceResolvedModel) func(models.GetResponse, sqlc.Provider, models.SDKModelConfig) models.SDKModelConfig {
	if s.conversationPresence == nil || !presenceEligible(req) {
		return nil
	}
	return func(model models.GetResponse, provider sqlc.Provider, cfg models.SDKModelConfig) models.SDKModelConfig {
		written := make(chan struct{})
		*target = &presenceResolvedModel{model: model, provider: provider, sdk: cfg, mainWritten: written}
		client := http.Client{}
		if cfg.HTTPClient != nil {
			client = *cfg.HTTPClient
		}
		transport := client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		client.Transport = &presenceTransport{base: transport, written: written}
		cfg.HTTPClient = &client
		return cfg
	}
}

// withConversationPresence is the single output owner. It merges typed optional
// feedback with main events without letting a second goroutine write the WS or
// runtime projection. First substantive output wins over a late acknowledgement.
func (s *Service) withConversationPresence(ctx context.Context, req ChatRequest, rc resolvedContext, main <-chan native.StreamEvent) <-chan native.StreamEvent {
	if s.conversationPresence == nil || rc.presence == nil || !presenceEligible(req) {
		return main
	}
	runtime := s.conversationPresence
	model := rc.presence
	source := requestInputSource(req)
	actor := source.UserID
	if actor == "" {
		actor = source.ChannelIdentityID
	}
	claims := &postgresPresenceClaims{queries: s.queries, handle: req.RunHandle, actor: actor, source: source, authorize: func(authCtx context.Context) (bool, error) {
		if s.botPermissions == nil {
			return false, nil
		}
		return s.botPermissions.HasBotPermission(authCtx, req.BotID, req.UserID, bots.PermissionChat)
	}}
	sideCtx, cancel := context.WithTimeout(ctx, runtime.options.Policy.Limits.Deadline)
	done := make(chan conversationpresence.Outcome, 1)
	go func() {
		defer close(done)
		if runtime.options.Jev.Endpoint != "" {
			s.runConversationPresenceJev(ctx, runtime, claims, req, model, rc, sideCtx, source, done)
			return
		}
		ticket, facts, decision, inserted, err := claims.prepare(sideCtx, req.SourceReplyToMessageID != "")
		if err != nil || !inserted {
			if s.logger != nil {
				s.logger.InfoContext(ctx, "conversation_presence", slog.String("status", "prepare_skipped"), slog.Bool("store_error", err != nil), slog.String("temperature", string(decision.Temperature)), slog.String("reason", decision.Reason))
			}
			return
		}
		generation := s.resolvePresenceGeneration(ctx, runtime, model)
		identity := conversationpresence.Identity{ModelUUID: generation.model.ID, ModelID: generation.model.ModelID, ProviderID: generation.model.ProviderID, ClientType: generation.provider.ClientType, RuntimeType: "model"}
		verified := declaredPresenceCapabilities(generation.model, generation.provider)
		if runtime.options.Verify != nil {
			verified = verified && runtime.options.Verify(generation.model, generation.provider)
		}
		generator, choice, err := prepareConversationPresenceSDK(identity, generation.model, generation.sdk, verified)
		if err != nil || generator == nil {
			if s.logger != nil {
				s.logger.InfoContext(ctx, "conversation_presence", slog.String("status", "capability_unavailable"), slog.String("reason", choice.Reason))
			}
			return
		}
		// The main request must already have been written to its transport. An
		// auxiliary request cannot take the first upstream concurrency slot.
		select {
		case <-model.mainWritten:
		case <-sideCtx.Done():
			return
		}
		coordinator := conversationpresence.Coordinator{Claims: claims, Capacity: runtime.capacity, Generator: generator, Policy: runtime.options.Policy}
		imagePresent := false
		for _, attachment := range req.Attachments {
			if attachment.Type == "image" || strings.HasPrefix(attachment.Mime, "image/") {
				imagePresent = true
				break
			}
		}
		handle, err := coordinator.Start(sideCtx, conversationpresence.Input{Ticket: ticket, Source: source, Facts: facts, Identity: identity, Choice: choice, Phase: conversationpresence.Thinking, BotName: rc.runConfig.Bot.Name, Text: req.RawQuery, ImagePresent: imagePresent})
		if err != nil {
			return
		}
		select {
		case outcome := <-handle.Done:
			if s.logger != nil {
				s.logger.InfoContext(ctx, "conversation_presence", slog.String("status", outcome.Status), slog.String("temperature", string(outcome.Decision.Temperature)), slog.String("reason", outcome.Decision.Reason), slog.Bool("usage_known", outcome.Usage.Known), slog.Int("input_tokens", outcome.Usage.InputTokens), slog.Int("output_tokens", outcome.Usage.OutputTokens))
			}
			select {
			case done <- outcome:
			case <-ctx.Done():
			}
		case <-ctx.Done():
		}
	}()
	out := make(chan native.StreamEvent)
	go func() {
		defer close(out)
		defer cancel()
		closed, shown := false, false
		closeFeedback := func(reason string) {
			if closed {
				return
			}
			closed = true
			cancel()
			// Local publication is disabled synchronously. Durable closure uses
			// the same session fence, but never makes formal text await a DB call.
			go func() {
				closeCtx, release := context.WithTimeout(context.WithoutCancel(ctx), runtime.options.Policy.Limits.Deadline)
				defer release()
				if err := claims.Close(closeCtx, claims.baseTicket(), reason); err != nil && s.logger != nil {
					s.logger.WarnContext(closeCtx, "conversation_presence_close_failed", slog.String("reason", reason))
				}
			}()
		}
		defer closeFeedback("stream_ended")
		send := func(event native.StreamEvent) bool {
			select {
			case out <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		handleMain := func(event native.StreamEvent) bool {
			if presenceSupersededBy(event) {
				closeFeedback(string(event.Type))
				if shown {
					if !send(native.StreamEvent{Type: agentevent.RuntimeStatus}) {
						return false
					}
					shown = false
				}
			}
			return send(event)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-main:
				if !ok || !handleMain(event) {
					return
				}
			case outcome, ok := <-done:
				done = nil
				if !ok || closed || outcome.Piece == nil || outcome.Status != "published" {
					continue
				}
				// Prefer already available formal output over auxiliary completion.
				// A random select tie must not flash a stale acknowledgement.
				if !forwardReadyPresenceMain(main, handleMain) {
					return
				}
				if closed {
					continue
				}
				if !send(native.StreamEvent{Type: native.EventProgress, Progress: *outcome.Piece}) {
					return
				}
				shown = true
			}
		}
	}()
	return out
}

func isConversationPresenceEvent(event native.StreamEvent) bool {
	if event.Type != native.EventProgress {
		return false
	}
	piece, ok := event.Progress.(conversationpresence.Piece)
	return ok && piece.Kind == "conversation_ack"
}

func presenceSupersededBy(event native.StreamEvent) bool {
	switch event.Type {
	case native.EventTextDelta:
		return event.Delta != ""
	case native.EventAgentEnd, native.EventAgentAbort, native.EventError, native.EventUserInputRequest, native.EventToolApprovalRequest, native.EventAttachment, native.EventRuntimeNotice:
		return true
	default:
		return false
	}
}

// forwardReadyPresenceMain forwards already ready main output before a pending
// acknowledgement, including output preceded by non-substantive events.
func forwardReadyPresenceMain(main <-chan native.StreamEvent, handle func(native.StreamEvent) bool) bool {
	for {
		select {
		case event, open := <-main:
			if !open || !handle(event) {
				return false
			}
		default:
			return true
		}
	}
}
