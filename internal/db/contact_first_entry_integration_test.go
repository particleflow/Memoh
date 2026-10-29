//go:build integration

package db_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/team"
)

// Separate fresh accounts reproduce first binding without resetting any existing
// route. Workspace metadata is a snapshot, not the contact's account identity.
func TestPostgresContactFirstEntryHistory(t *testing.T) {
	_ = freshMigratedDB(t)
	cfg, err := pgxpool.ParseConfig(teamMigrationDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, directCreate := range []bool{true, false} {
		name := "inspect_then_bind"
		if directCreate {
			name = "direct_create_bypasses_old_history"
		}
		t.Run(name, func(t *testing.T) {
			f := newContactLifecycleFixture(t, pool)
			c := f.coordinator(f.q)
			metadata := map[string]any{"workspace_target_id": "old-target", "workspace_target": map[string]any{"target_id": "old-target", "kind": "remote", "name": "Old Computer"}}
			old, e := f.sessions.Create(t.Context(), thread.CreateInput{BotID: f.bot, CreatedByUserID: f.user, Title: "old main chat", Metadata: metadata})
			if e != nil {
				t.Fatal(e)
			}
			addMessages := func(id string, n int) {
				t.Helper()
				_, e := pool.Exec(t.Context(), `INSERT INTO bot_history_messages(bot_id,session_id,sender_account_user_id,role,content) SELECT $1,$2,$3,'user',jsonb_build_array(jsonb_build_object('type','text','text','retained-'||n)) FROM generate_series(1,$4::int) n`, f.bot, id, f.user, n)
				if e != nil {
					t.Fatal(e)
				}
			}
			digest := func() string {
				t.Helper()
				var value string
				if e := pool.QueryRow(t.Context(), `SELECT md5(string_agg(row_to_json(m)::text,'' ORDER BY id)) FROM bot_history_messages m WHERE session_id=$1`, old.ID).Scan(&value); e != nil {
					t.Fatal(e)
				}
				return value
			}
			addMessages(old.ID, 75)
			before := digest()
			entry, e := c.InspectPrivateChat(t.Context(), f.bot, f.user, "")
			if e != nil || len(entry.Candidates) != 1 || entry.Candidates[0].ID != old.ID {
				t.Fatalf("eligible old history: %+v %v", entry, e)
			}
			var chosen thread.Thread
			if directCreate {
				chosen, e = c.EnsurePrivateChat(t.Context(), f.bot, f.user)
				if e != nil || chosen.ID == old.ID || f.count(t, "bot_history_messages", "session_id=$1", chosen.ID) != 0 {
					t.Fatalf("direct create counterexample: %+v %v", chosen, e)
				}
				addMessages(chosen.ID, 8)
				_, e = f.sessions.UpdateMetadata(t.Context(), chosen.ID, map[string]any{"workspace_target_id": "new-target", "workspace_target": map[string]any{"target_id": "new-target", "kind": "native", "name": "New Computer"}})
				if e != nil {
					t.Fatal(e)
				}
				if _, e = c.BindPrivateChat(t.Context(), f.bot, f.user, old.ID); !errors.Is(e, route.ErrContactConflict) {
					t.Fatalf("replaced new-message contact: %v", e)
				}
			} else {
				chosen, e = c.BindPrivateChat(t.Context(), f.bot, f.user, old.ID)
				if e != nil || chosen.ID != old.ID || !reflect.DeepEqual(chosen.Metadata, metadata) {
					t.Fatalf("bind changed identity/metadata: %+v %v", chosen, e)
				}
			}
			for i := 0; i < 3; i++ {
				// Fresh coordinators simulate independent opens, not a full server restart.
				entry, e = f.coordinator(f.q).InspectPrivateChat(t.Context(), f.bot, f.user, "")
				if e != nil || entry.Session == nil || entry.Session.ID != chosen.ID || len(entry.Candidates) != 0 {
					t.Fatalf("bound contact lost: %+v %v", entry, e)
				}
			}
			if digest() != before || f.count(t, "bot_history_messages", "session_id=$1", old.ID) != 75 {
				t.Fatal("old message rows changed")
			}
			retained, e := f.sessions.Get(t.Context(), old.ID)
			if e != nil || !reflect.DeepEqual(retained.Metadata, metadata) || retained.WorkdirID != old.WorkdirID {
				t.Fatal("old execution metadata changed")
			}
			expectedSessions := 1
			if directCreate {
				expectedSessions = 2
				if f.count(t, "bot_history_messages", "session_id=$1", chosen.ID) != 8 {
					t.Fatal("new messages changed")
				}
			}
			if f.count(t, "bot_sessions", "bot_id=$1", f.bot) != expectedSessions {
				t.Fatal("repeat opens created sessions")
			}
			t.Logf("old messages=75 unchanged; direct_create=%v; sessions=%d; repeated opens=3 same bound identity", directCreate, expectedSessions)
		})
	}
}
