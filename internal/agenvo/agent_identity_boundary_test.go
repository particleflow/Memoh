package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// This is a counterexample for a proposed get-then-prompt follow-up, not a
// production follow-up implementation. Agenvo v0.2.0 and Herdr v0.9.5 accept a
// live target reference, not an expected Agent incarnation. The fixture's
// incarnation is deliberately private: it is not an invented wire field.
// See docs/native-agent-followup-boundary.md for the pinned upstream sources.
func TestAgentFollowupIdentityBoundary(t *testing.T) {
	for _, mode := range []string{"unchanged", "reused_before_get", "reused_after_get", "changed_after_get"} {
		t.Run(mode, func(t *testing.T) {
			s, a := newAgentFixture(t)
			a.name = "m" + strings.Repeat("a", 30)
			p := AgentProgress{Name: a.name, WorkspaceID: a.workspace, PaneID: a.pane, TerminalID: a.terminal, Phase: "prompt_submitted", PromptSubmitted: true}
			incarnation, deliveredTo := "original", ""
			if mode == "reused_before_get" {
				incarnation = "replacement"
			}
			transport := s.call
			s.call = func(ctx context.Context, owner, connection, tool string, args map[string]any) (json.RawMessage, error) {
				var parts []json.RawMessage
				code := args["code"].(string)
				if err := json.Unmarshal([]byte("["+strings.TrimSuffix(strings.TrimPrefix(code, "return await call("), ");")+"]"), &parts); err != nil || len(parts) != 3 {
					t.Fatal("unexpected relay call")
				}
				var method string
				var params map[string]any
				if err := json.Unmarshal(parts[1], &method); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(parts[2], &params); err != nil {
					t.Fatal(err)
				}
				if params["session"] != "default" || params["name"] != p.Name {
					t.Fatal("did not reuse the stored native reference")
				}
				if method == "agent.prompt" {
					// These are all the published prompt fields. Neither a ledger
					// revision nor a wrapper nonce can fence native identity here.
					if len(params) != 3 || params["text"] != "synthetic follow-up" {
						t.Fatal("invented prompt argument")
					}
					if mode == "reused_after_get" || mode == "changed_after_get" {
						incarnation = "replacement"
					}
					if mode == "changed_after_get" {
						a.terminal = "replacement-terminal"
					}
					deliveredTo = incarnation // The native effect precedes its receipt.
				}
				return transport(ctx, owner, connection, tool, args)
			}
			c, target, err := s.Client(t.Context(), request().Scope)
			if err != nil {
				t.Fatal(err)
			}
			info, starting, err := c.agentInfo(t.Context(), request().Scope, target, p)
			if err != nil || starting || !info.InteractiveReady {
				t.Fatalf("precheck: starting=%v err=%v", starting, err)
			}
			r, err := c.relay(t.Context(), request().Scope, target, "agent.prompt", map[string]any{"session": target.Session, "name": p.Name, "text": "synthetic follow-up"})
			if err != nil {
				t.Fatal(err)
			}
			err = validatePromptReceipt(r, target.Session, p)
			if mode == "changed_after_get" {
				if !errors.Is(err, ErrUnknown) || deliveredTo != "replacement" {
					t.Fatal("a changed receipt must be unknown, but cannot undo input already sent")
				}
			} else if err != nil {
				t.Fatalf("matching references should pass existing receipt validation: %v", err)
			}
			want := "replacement"
			if mode == "unchanged" {
				want = "original"
			}
			if deliveredTo != want || !slices.Equal(a.calls, []string{"agent.get", "agent.prompt"}) {
				t.Fatalf("delivered=%s calls=%v", deliveredTo, a.calls)
			}
			t.Logf("get matched stored references; prompt delivered to %s; receipt validation error=%v", deliveredTo, err)
		})
	}
}
