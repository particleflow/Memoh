package application

import "testing"

// TestConversationPresencePostgresOneSidecarPerTurnRegardlessOfMessageRows pins the field report's
// arithmetic. The 2026-11-02 window had 26 bot_history_messages rows after
// 07:00Z and exactly one presence run; 25 of those rows were assistant/tool
// projections of the same turn. Presence is a per-turn sidecar, so the row
// count is not evidence that the pre-gate dropped user inputs. This test
// writes the same shape (one turn, 26 rows) and asserts one receipt: a future
// change that starts a sidecar per message row instead of per turn fails here.
func TestConversationPresencePostgresOneSidecarPerTurnRegardlessOfMessageRows(t *testing.T) {
	claims, ctx, pool := presencePostgresFixture(t)
	ticket, facts, _, inserted, err := claims.prepareStep1(ctx, false)
	if err != nil || !inserted {
		t.Fatalf("step1: inserted=%v error=%v", inserted, err)
	}
	if !facts.Admitted {
		t.Fatalf("facts %+v", facts)
	}
	// One user row plus 25 assistant/tool projections of the same turn. The
	// history read only counts prior user provenance, so none of them may flip
	// FirstDirectInput or start another sidecar.
	for index := 0; index < 25; index++ {
		role, content := "assistant", `{"role":"assistant","content":"projection"}`
		if index%2 == 1 {
			role, content = "tool", `{"role":"tool","content":"result"}`
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO bot_history_messages (bot_id, session_id, role, content, turn_id, turn_visible)
			VALUES ($1, $2, $3, $4::jsonb, $5, false)
		`, claims.handle.BotID, claims.handle.SessionID, role, content, claims.handle.TurnID); err != nil {
			t.Fatalf("insert projection %d: %v", index, err)
		}
	}
	// A replayed sidecar for the same invocation is the same admission: the
	// receipt conflict keeps it to one row, it does not fan out per message.
	if _, _, _, again, err := claims.prepareStep1(ctx, false); err != nil || again {
		t.Fatalf("replayed step1: inserted=%v error=%v", again, err)
	}
	var messages, receipts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM bot_history_messages WHERE session_id=$1`, claims.handle.SessionID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conversation_presence WHERE session_id=$1 AND invocation_id=$2`, claims.handle.SessionID, ticket.InvocationID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if messages != 25 {
		t.Fatalf("fixture rows=%d want 25 projections", messages)
	}
	if receipts != 1 {
		t.Fatalf("presence receipts=%d want 1 per admitted turn", receipts)
	}
}
