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
	// Verify can narrow a host's capability contract. By default use explicit
	// declarations from the model/provider selected for this formal request.
	// This is catalog evidence, not a claim that an opaque gateway obeyed it.
	Verify func(models.GetResponse, sqlc.Provider) bool
}

func declaredPresenceCapabilities(model models.GetResponse, provider sqlc.Provider) bool {
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
		ticket, facts, inserted, err := claims.prepare(sideCtx, runtime.options.Policy.Continuity, req.SourceReplyToMessageID != "")
		if err != nil || !inserted {
			if s.logger != nil {
				s.logger.InfoContext(ctx, "conversation_presence", slog.String("status", "prepare_skipped"), slog.Bool("store_error", err != nil))
			}
			return
		}
		identity := conversationpresence.Identity{ModelUUID: model.model.ID, ModelID: model.model.ModelID, ProviderID: model.model.ProviderID, ClientType: model.provider.ClientType, RuntimeType: "model"}
		verified := declaredPresenceCapabilities(model.model, model.provider)
		if runtime.options.Verify != nil {
			verified = verified && runtime.options.Verify(model.model, model.provider)
		}
		generator, choice, err := prepareConversationPresenceSDK(identity, model.model, model.sdk, verified)
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
				s.logger.InfoContext(ctx, "conversation_presence", slog.String("status", outcome.Status), slog.String("temperature", string(outcome.Decision.Temperature)), slog.Bool("usage_known", outcome.Usage.Known), slog.Int("input_tokens", outcome.Usage.InputTokens), slog.Int("output_tokens", outcome.Usage.OutputTokens))
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
				select {
				case event, open := <-main:
					if !open || !handleMain(event) {
						return
					}
				default:
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
