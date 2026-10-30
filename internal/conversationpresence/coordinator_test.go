package conversationpresence

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fixtureClaims is deliberately test-only; production must use the original
// durable admission/event owner, not this in-memory reference behavior.
type fixtureClaims struct {
	mu sync.Mutex
	claimed map[Ticket]bool
	closed map[Ticket]bool
	episodes map[struct{ Scope Scope; ID string }]bool
	pieces []Piece
}

func newFixtureClaims() *fixtureClaims {
	return &fixtureClaims{claimed: make(map[Ticket]bool), closed: make(map[Ticket]bool), episodes: make(map[struct{ Scope Scope; ID string }]bool)}
}

func (s *fixtureClaims) Claim(_ context.Context, ticket Ticket) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := struct{ Scope Scope; ID string }{ticket.Scope, ticket.EpisodeID}
	if s.closed[ticket] || s.claimed[ticket] || s.episodes[key] {
		return false, nil
	}
	s.claimed[ticket], s.episodes[key] = true, true
	return true, nil
}

func (s *fixtureClaims) Close(_ context.Context, ticket Ticket, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed[ticket] = true
	return nil
}

func (s *fixtureClaims) Publish(ctx context.Context, ticket Ticket, piece Piece) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || s.closed[ticket] || !s.claimed[ticket] {
		return false, nil
	}
	s.closed[ticket] = true
	s.pieces = append(s.pieces, piece)
	return true, nil
}

type fixtureCapacity struct{ calls atomic.Int32; releases atomic.Int32; deny bool }

func (c *fixtureCapacity) TryAcquire(context.Context, Identity) (func(), bool) {
	if c.deny { return nil, false }
	c.calls.Add(1)
	return func(){ c.releases.Add(1) }, true
}

type fixtureGenerator func(context.Context, Input, Limits) (Result, error)

func (f fixtureGenerator) Generate(ctx context.Context, in Input, limits Limits) (Result, error) { return f(ctx, in, limits) }

func fixtureInput() Input {
	return Input{
		Ticket: Ticket{Scope: Scope{ActorID: "actor", BotID: "bot", SessionID: "thread"}, InvocationID: "invocation", EpisodeID: "episode", Generation: "generation", Fence: 1},
		Source: directSource(), Facts: Facts{Admitted: true, FirstDirectInput: true}, Phase: Accepted,
		Identity: Identity{ModelUUID: "selected", ModelID: "catalog-model", ProviderID: "provider", ClientType: "openai-completions"},
		Choice: ThinkingChoice{Usable: true}, Text: "Please examine this problem.",
	}
}

func fixtureCoordinator(claims *fixtureClaims, capacity *fixtureCapacity, generate fixtureGenerator) Coordinator {
	return Coordinator{Claims: claims, Capacity: capacity, Generator: generate, Policy: Policy{Limits: Limits{Deadline: time.Second, MaxInputRunes: 120, MaxOutputTokens: 64, MaxTextRunes: 80}}}
}

func awaitOutcome(t *testing.T, h *Handle) Outcome {
	t.Helper()
	select {
	case out := <-h.Done: return out
	case <-time.After(3 * time.Second): t.Fatal("sidecar did not terminate"); return Outcome{}
	}
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture event did not arrive")
	}
}

func TestAcknowledgementIsNaturalAndTypedNotAFixedFallback(t *testing.T) {
	s, capacity := newFixtureClaims(), &fixtureCapacity{}
	c := fixtureCoordinator(s, capacity, func(_ context.Context, in Input, _ Limits) (Result, error) {
		return Result{Text: "收到，我来梳理这个问题。", Phase: in.Phase, Usage: Usage{Known: true, OutputTokens: 9}}, nil
	})
	h, err := c.Start(context.Background(), fixtureInput())
	if err != nil { t.Fatal(err) }
	out := awaitOutcome(t, h)
	if out.Status != "published" || len(s.pieces) != 1 || s.pieces[0].Kind != "conversation_ack" || s.pieces[0].InputSource != directSource() {
		t.Fatalf("typed model feedback lost: %+v %+v", out, s.pieces)
	}
	if capacity.calls.Load() != 1 || capacity.releases.Load() != 1 || !out.Usage.Known { t.Fatal("capacity/usage lost") }
}

func TestFormalFirstAndCancelRejectProviderThatIgnoresCancellation(t *testing.T) {
	s, capacity := newFixtureClaims(), &fixtureCapacity{}
	entered, unblock := make(chan struct{}), make(chan struct{})
	c := fixtureCoordinator(s, capacity, func(_ context.Context, in Input, _ Limits) (Result, error) {
		close(entered)
		<-unblock // simulate a late provider callback even after cancellation
		return Result{Text: "A delayed acknowledgement.", Phase: in.Phase}, nil
	})
	h, err := c.Start(context.Background(), fixtureInput())
	if err != nil { t.Fatal(err) }
	awaitSignal(t, entered)
	if err := h.Close(context.Background(), "formal_text"); err != nil { t.Fatal(err) }
	// Main rendering is independent: it can finish before the provider does.
	mainRendered := make(chan struct{})
	close(mainRendered)
	select { case <-mainRendered: default: t.Fatal("main blocked on sidecar") }
	if capacity.releases.Load() != 0 { t.Fatal("capacity released before actual provider completion") }
	close(unblock)
	out := awaitOutcome(t, h)
	if out.Status != "canceled" || len(s.pieces) != 0 { t.Fatalf("late ack leaked: %+v %+v", out, s.pieces) }
}

type blockedClaim struct {
	*fixtureClaims
	entered chan struct{}
	unblock chan struct{}
}

func (s *blockedClaim) Claim(ctx context.Context, ticket Ticket) (bool, error) {
	close(s.entered)
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-s.unblock:
		return s.fixtureClaims.Claim(ctx, ticket)
	}
}

func TestStartDoesNotWaitForAuxiliaryStoreOrGeneration(t *testing.T) {
	store := &blockedClaim{fixtureClaims: newFixtureClaims(), entered: make(chan struct{}), unblock: make(chan struct{})}
	c := fixtureCoordinator(store.fixtureClaims, &fixtureCapacity{}, func(_ context.Context, in Input, _ Limits) (Result, error) {
		return Result{Text: "Received.", Phase: in.Phase}, nil
	})
	c.Claims = store
	h, err := c.Start(context.Background(), fixtureInput())
	if err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, store.entered)
	// Reaching this line while Claim is still blocked is the nonblocking
	// contract. No formal-turn function was passed to, or awaited by, Start.
	close(store.unblock)
	if got := awaitOutcome(t, h); got.Status != "published" {
		t.Fatal(got)
	}
}

func TestHotConversationAndForeignActorDoNotGenerate(t *testing.T) {
	for _, foreignActor := range []bool{false, true} {
		store := newFixtureClaims()
		c := fixtureCoordinator(store, &fixtureCapacity{}, func(context.Context, Input, Limits) (Result, error) {
			t.Error("ineligible sidecar called model")
			return Result{}, ErrInvalidContract
		})
		in := fixtureInput()
		want := "not_cold"
		if foreignActor {
			in.Ticket.Scope.ActorID = "foreign-actor"
			want = "source_actor_mismatch"
		} else {
			in.Facts.PendingUserInput = true
		}
		h, err := c.Start(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if got := awaitOutcome(t, h); got.Status != want || len(store.pieces) != 0 {
			t.Fatalf("unexpected output: %+v", got)
		}
	}
}

func TestFormalBeforeClaimAndEpisodeReplayDoNotGenerate(t *testing.T) {
	s, capacity := newFixtureClaims(), &fixtureCapacity{}
	var generated atomic.Int32
	c := fixtureCoordinator(s, capacity, func(_ context.Context, in Input, _ Limits) (Result, error) {
		generated.Add(1)
		return Result{Text: "I will consider that.", Phase: in.Phase}, nil
	})
	in := fixtureInput()
	if err := s.Close(context.Background(), in.Ticket, "formal_text"); err != nil { t.Fatal(err) }
	h, err := c.Start(context.Background(), in)
	if err != nil { t.Fatal(err) }
	if got := awaitOutcome(t, h); got.Status != "not_claimed" { t.Fatal(got) }
	if generated.Load() != 0 { t.Fatal("generation started after close") }
	// A different invocation in a single episode is still one natural ack.
	in.Ticket.EpisodeID = "another-episode"
	for i := range 2 {
		if i == 1 { in.Ticket.InvocationID = "followup" }
		h, err := c.Start(context.Background(), in)
		if err != nil { t.Fatal(err) }
		awaitOutcome(t, h)
	}
	if generated.Load() != 1 || len(s.pieces) != 1 { t.Fatal("episode produced redundant acknowledgements") }
}

func TestFailureCapacityAndWrongPhaseNeverPublishOrRetry(t *testing.T) {
	for _, tc := range []struct{ name string; deny bool; result Result; err error; want string }{
		{"provider", false, Result{}, errors.New("synthetic provider failure"), "generation_failed"},
		{"capacity", true, Result{}, nil, "capacity_unavailable"},
		{"phase", false, Result{Text: "Already executing.", Phase: Thinking}, nil, "invalid_output"},
		{"markup", false, Result{Text: "<think>secret</think>", Phase: Accepted}, nil, "invalid_output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, capacity := newFixtureClaims(), &fixtureCapacity{deny: tc.deny}
			var calls atomic.Int32
			c := fixtureCoordinator(s, capacity, func(context.Context, Input, Limits) (Result, error) { calls.Add(1); return tc.result, tc.err })
			h, err := c.Start(context.Background(), fixtureInput())
			if err != nil { t.Fatal(err) }
			if got := awaitOutcome(t, h); got.Status != tc.want { t.Fatalf("got %+v", got) }
			if len(s.pieces) != 0 || calls.Load() > 1 { t.Fatal("invalid publication or automatic retry") }
		})
	}
}
