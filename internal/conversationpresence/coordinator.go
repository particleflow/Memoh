package conversationpresence

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/felinics/memoh/internal/inputsource"
)

var ErrInvalidContract = errors.New("conversation presence contract is incomplete")

// Scope is populated by the authenticated ingress. Optional team/contact values
// stay empty when the original ingress has none; this is not an access grant.
type Scope struct {
	TeamID    string
	ActorID   string
	BotID     string
	ContactID string
	SessionID string
}

// Ticket is minted by the original admission owner. Generation and Fence must
// track edits, newer inputs and ownership loss, independently of cancellation.
type Ticket struct {
	Scope        Scope
	RunID        string
	InvocationID string
	EpisodeID    string
	Generation   string
	Fence        int64
}

func (t Ticket) valid() bool {
	return t.Scope.ActorID != "" && t.Scope.BotID != "" && t.Scope.SessionID != "" &&
		t.InvocationID != "" && t.EpisodeID != "" && t.Generation != "" && t.Fence > 0
}

// Identity is a frozen model/route selection, without credentials or addresses.
// A group alias is not an effective member unless the host has established an
// exact same-member calling contract. Capability verification is per selection.
type Identity struct {
	ModelUUID         string
	ModelID           string
	ProviderID        string
	ClientType        string
	RuntimeType       string
	CapabilityVersion string
	RouteVersion      string
}

type Phase string

const (
	Accepted Phase = "accepted"
	Queued   Phase = "queued"
	Thinking Phase = "thinking"
)

func (p Phase) valid() bool { return p == Accepted || p == Queued || p == Thinking }

type Input struct {
	Ticket       Ticket
	Source       inputsource.Source
	Facts        Facts
	Identity     Identity
	Choice       ThinkingChoice
	Phase        Phase
	BotName      string
	Locale       string
	Text         string
	ImagePresent bool
}

// Piece is typed progress, not an assistant turn. Hosts must exclude it from
// formal model history, memory, titles, compaction and task completion tests.
type Piece struct {
	Kind        string             `json:"kind"`
	Text        string             `json:"text"`
	Phase       Phase              `json:"phase"`
	InputSource inputsource.Source `json:"input_source"`
}

type Usage struct {
	Known             bool
	InputTokens       int
	OutputTokens      int
	ReasoningTokens   int
	CachedInputTokens int
}

type Result struct {
	Text  string
	Phase Phase
	Usage Usage
}

type Generator interface {
	Generate(context.Context, Input, Limits) (Result, error)
}

// Claims is a required host port, not a new database/service. Implement it in
// the existing durable admission/event owner before wiring this sidecar:
//   - Claim atomically rechecks permission, full scope, fence and generation,
//     rejects closed/replayed tickets, and admits only one claim per episode.
//   - Close records invalidation even if generation/Claim has not started yet.
//   - Publish atomically rechecks those conditions and formal/terminal state,
//     and records typed progress plus its deduplication receipt at most once.
//
// Formal output must invalidate in that same host-owned serialization domain.
// A browser map or an in-memory implementation is insufficient for production.
type Claims interface {
	Claim(context.Context, Ticket) (bool, error)
	Close(context.Context, Ticket, string) error
	Publish(context.Context, Ticket, Piece) (bool, error)
}

// Capacity uses the original provider's budget/limiter after main capacity has
// priority. A release covers the real generation lifetime, including cancel.
type Capacity interface {
	TryAcquire(context.Context, Identity) (release func(), acquired bool)
}

type Limits struct {
	Deadline        time.Duration
	MaxInputRunes   int
	MaxOutputTokens int
	MaxTextRunes    int
}

type Policy struct {
	Continuity ContinuityPolicy
	Limits     Limits
}

type Outcome struct {
	Decision Decision
	Status   string
	Usage    Usage
	Piece    *Piece
	Err      error
}

type Coordinator struct {
	Claims    Claims
	Capacity  Capacity
	Generator Generator
	Policy    Policy
}

type Handle struct {
	Done   <-chan Outcome
	cancel context.CancelFunc
	ticket Ticket
	claims Claims
}

// Close cancels network work immediately and invalidates queued callbacks.
// The caller supplies a live host context for the durable invalidation; never
// pass a canceled network context or interpret an error as a successful close.
func (h *Handle) Close(ctx context.Context, reason string) error {
	h.cancel()
	return h.claims.Close(ctx, h.ticket, reason)
}

// Start launches only the optional sidecar. It never calls or waits for the
// formal turn. The host starts main independently and never awaits Done to
// render formal text, execute tools, commit history or release the user's turn.
func (c Coordinator) Start(parent context.Context, in Input) (*Handle, error) {
	if c.Claims == nil || c.Capacity == nil || c.Generator == nil || !in.Ticket.valid() ||
		!in.Phase.valid() || c.Policy.Limits.Deadline <= 0 || c.Policy.Limits.MaxInputRunes <= 0 || c.Policy.Limits.MaxOutputTokens <= 0 || c.Policy.Limits.MaxTextRunes <= 0 {
		return nil, ErrInvalidContract
	}
	// Never let caller mutation of a request-local reasoning pointer affect an
	// already running sidecar (or let the sidecar mutate the main configuration).
	if in.Choice.Config != nil {
		copyConfig := *in.Choice.Config
		in.Choice.Config = &copyConfig
	}
	ctx, cancel := context.WithTimeout(parent, c.Policy.Limits.Deadline)
	done := make(chan Outcome, 1)
	h := &Handle{Done: done, cancel: cancel, ticket: in.Ticket, claims: c.Claims}
	go func() {
		defer cancel()
		defer close(done)
		done <- c.run(ctx, in)
	}()
	return h, nil
}

func (c Coordinator) run(ctx context.Context, in Input) Outcome {
	d := Classify(in.Source, in.Ticket.Scope.BotID, in.Ticket.Scope.SessionID, in.Facts, c.Policy.Continuity)
	out := Outcome{Decision: d, Status: "not_cold"}
	if d.Temperature != Cold {
		return out
	}
	if in.Source.UserID != in.Ticket.Scope.ActorID && in.Source.ChannelIdentityID != in.Ticket.Scope.ActorID {
		out.Status = "source_actor_mismatch"
		return out
	}
	if !in.Choice.Usable {
		out.Status = "capability_unavailable"
		return out
	}
	if err := ctx.Err(); err != nil {
		return canceledOutcome(out, err)
	}
	claimed, err := c.Claims.Claim(ctx, in.Ticket)
	if err != nil || !claimed {
		out.Status, out.Err = "not_claimed", err
		return out
	}
	release, acquired := c.Capacity.TryAcquire(ctx, in.Identity)
	if !acquired || release == nil {
		out.Status = "capacity_unavailable"
		return out
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return canceledOutcome(out, err)
	}
	result, err := c.Generator.Generate(ctx, in, c.Policy.Limits)
	out.Usage = result.Usage
	if err != nil {
		if cause := ctx.Err(); cause != nil {
			return canceledOutcome(out, cause)
		}
		out.Status, out.Err = "generation_failed", err
		return out
	}
	if err := ctx.Err(); err != nil {
		return canceledOutcome(out, err)
	}
	text := strings.TrimSpace(result.Text)
	if result.Phase != in.Phase || !validText(text, c.Policy.Limits.MaxTextRunes) {
		out.Status = "invalid_output"
		return out
	}
	piece := Piece{Kind: "conversation_ack", Text: text, Phase: result.Phase, InputSource: in.Source.Scoped(in.Ticket.Scope.BotID, in.Ticket.Scope.SessionID)}
	published, err := c.Claims.Publish(ctx, in.Ticket, piece)
	if err != nil || !published {
		out.Status, out.Err = "suppressed", err
		return out
	}
	out.Status = "published"
	out.Piece = &piece
	return out
}

func canceledOutcome(out Outcome, cause error) Outcome {
	out.Status, out.Err = "canceled", cause
	if errors.Is(cause, context.DeadlineExceeded) {
		out.Status = "expired"
	}
	return out
}

func validText(text string, limit int) bool {
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > limit {
		return false
	}
	for _, char := range text {
		if unicode.IsControl(char) || char == '<' || char == '>' || char == '\u2028' || char == '\u2029' {
			return false
		}
	}
	return true
}
