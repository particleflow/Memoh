package toolcontext

import (
	"context"
	"errors"
	"testing"
)

func TestComputerBindingRejectsClientClaims(t *testing.T) {
	claimed := Session{BotID: "bot-a", UserID: "user-a", SessionID: "conversation-a", WorkspaceTargetID: "pc-a"}
	if _, err := ResolveComputerBinding(t.Context(), claimed); !errors.Is(err, ErrComputerBinding) {
		t.Fatal("unbound client established scope from fields")
	}
	binding := ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "conversation-a", TargetID: "pc-a"}
	bound, err := WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	claimed.RunContext = bound
	got, err := ResolveComputerBinding(t.Context(), claimed)
	if err != nil || got != binding {
		t.Fatalf("trusted host binding lost: %+v %v", got, err)
	}
	for _, alter := range []func(*Session){
		func(s *Session) { s.PublicRequest = true },
		func(s *Session) { s.BotID = "bot-b" },
		func(s *Session) { s.UserID = "user-b" },
		func(s *Session) { s.SessionID = "conversation-b" },
		func(s *Session) { s.RunContext = context.Background() },
	} {
		forged := claimed
		alter(&forged)
		if _, err := ResolveComputerBinding(bound, forged); !errors.Is(err, ErrComputerBinding) {
			t.Fatal("client claims bypassed trusted binding")
		}
	}
}

func TestComputerBindingCannotBeReboundOrOutliveRun(t *testing.T) {
	for _, target := range []string{"native", "pc-a"} {
		binding := ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "conversation-a", TargetID: target}
		run, cancel := context.WithCancel(t.Context())
		bound, err := WithComputerBinding(run, binding)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		for _, replacement := range []string{"native", "pc-a", "pc-b", ""} {
			changed := binding
			changed.TargetID = replacement
			_, err := WithComputerBinding(bound, changed)
			if (replacement == target) != (err == nil) {
				cancel()
				t.Fatalf("binding %q changed to %q: %v", target, replacement, err)
			}
		}
		cancel()
		if _, err := ResolveComputerBinding(bound, Session{BotID: binding.BotID, SessionID: binding.SessionID}); !errors.Is(err, ErrComputerBinding) {
			t.Fatal("completed run retained computer authority")
		}
	}
}

func TestRestoreComputerRunKeepsAuthenticatedBinding(t *testing.T) {
	b := ComputerBinding{BotID: "bot", UserID: "user", SessionID: "session", TargetID: "pc"}
	ctx, err := WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreComputerRun(ctx, "original-run")
	want := b
	want.RunID = "original-run"
	got, ok := HostComputerBinding(restored)
	if err != nil || !ok || got != want || ComputerScope(restored) != "pc" {
		t.Fatal("restoring ledger run changed identity/scope", err)
	}
	if original, _ := HostComputerBinding(ctx); original != b {
		t.Fatal("request binding was mutated")
	}
	if _, err := RestoreComputerRun(restored, "original-run"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		ctx context.Context
		run string
	}{{t.Context(), "original-run"}, {ctx, ""}, {restored, "another-run"}, {context.WithValue(ctx, untrustedComputerKey{}, true), "original-run"}} {
		if _, err := RestoreComputerRun(test.ctx, test.run); !errors.Is(err, ErrComputerBinding) {
			t.Fatal("untrusted or changed run accepted", err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := RestoreComputerRun(canceled, "original-run"); !errors.Is(err, ErrComputerBinding) {
		t.Fatal("canceled binding restored", err)
	}
}
