//go:build integration

package db_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/team"
)

func TestPostgresPrivateChatNavigation(t *testing.T) {
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
	create := func(t *testing.T, f contactLifecycleFixture, input thread.CreateInput) thread.Thread {
		t.Helper()
		input.BotID = f.bot
		if input.CreatedByUserID == "" {
			input.CreatedByUserID = f.user
		}
		s, e := f.sessions.Create(t.Context(), input)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	t.Run("read_only_candidates_and_explicit_binding_preserve_history_and_hook_count", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		c := f.coordinator(f.q)
		a := create(t, f, thread.CreateInput{Title: "older"})
		b := create(t, f, thread.CreateInput{Title: "newer"})
		hookBefore, eventBefore := f.hook.calls.Load(), f.events.calls.Load()
		entry, e := c.InspectPrivateChat(t.Context(), f.bot, f.user, "")
		if e != nil {
			t.Fatal(e)
		}
		if len(entry.Candidates) != 2 || entry.Session != nil || f.count(t, "bot_channel_routes", "bot_id=$1", f.bot) != 0 {
			t.Fatalf("inspection mutated or guessed: %+v", entry)
		}
		chosen, e := c.BindPrivateChat(t.Context(), f.bot, f.user, a.ID)
		if e != nil || chosen.ID != a.ID {
			t.Fatalf("bind: %+v %v", chosen, e)
		}
		if !chosen.CreatedAt.Equal(a.CreatedAt) || !chosen.UpdatedAt.Equal(a.UpdatedAt) || chosen.Title != a.Title {
			t.Fatal("adoption rewrote existing session")
		}
		if f.hook.calls.Load() != hookBefore || f.events.calls.Load() != eventBefore {
			t.Fatal("adoption replayed creation")
		}
		if _, e = c.BindPrivateChat(t.Context(), f.bot, f.user, b.ID); !errors.Is(e, route.ErrContactConflict) {
			t.Fatalf("replaced existing binding: %v", e)
		}
		again, e := c.BindPrivateChat(t.Context(), f.bot, f.user, a.ID)
		if e != nil || again.ID != a.ID {
			t.Fatalf("idempotency: %v", e)
		}
		historical, e := f.sessions.Get(t.Context(), b.ID)
		if e != nil || historical.RouteID != "" {
			t.Fatalf("unselected history changed: %+v %v", historical, e)
		}
		resolved, e := c.EnsurePrivateChat(t.Context(), f.bot, f.user)
		if e != nil || resolved.ID != a.ID {
			t.Fatalf("contact: %v", e)
		}
	})
	t.Run("administrator_cannot_adopt_foreign_internal_external_or_child_chats", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		other := newContactLifecycleFixture(t, pool)
		foreign := create(t, f, thread.CreateInput{CreatedByUserID: other.user})
		internal := create(t, f, thread.CreateInput{Visibility: thread.VisibilityInternal})
		parent := create(t, f, thread.CreateInput{})
		child := create(t, f, thread.CreateInput{Type: thread.TypeSubagent, ParentThreadID: parent.ID})
		external := create(t, f, thread.CreateInput{})
		if _, e := pool.Exec(t.Context(), "UPDATE bot_sessions SET runtime_type='codex' WHERE id=$1", external.ID); e != nil {
			t.Fatal(e)
		}
		c := f.coordinator(f.q)
		for _, s := range []thread.Thread{foreign, internal, child, external} {
			if _, e := c.BindPrivateChat(t.Context(), f.bot, f.user, s.ID); !errors.Is(e, route.ErrContactConflict) {
				t.Fatalf("unsafe candidate %s: %v", s.ID, e)
			}
		}
		entry, e := c.InspectPrivateChat(t.Context(), f.bot, f.user, "")
		if e != nil || len(entry.Candidates) != 1 || entry.Candidates[0].ID != parent.ID {
			t.Fatalf("candidate isolation: %+v %v", entry, e)
		}
		if f.count(t, "bot_channel_routes", "bot_id=$1", f.bot) != 0 {
			t.Fatal("rejected adoption left route debris")
		}
	})
	t.Run("two_devices_choose_different_existing_chats_exactly_one_wins", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		c := f.coordinator(f.q)
		a, b := create(t, f, thread.CreateInput{}), create(t, f, thread.CreateInput{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		ids := []string{a.ID, b.ID}
		gate := make(chan struct{})
		for i := range ids {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-gate
				_, errs[i] = c.BindPrivateChat(t.Context(), f.bot, f.user, ids[i])
			}(i)
		}
		close(gate)
		wg.Wait()
		wins := 0
		for _, e := range errs {
			if e == nil {
				wins++
			} else if !errors.Is(e, route.ErrContactConflict) {
				t.Fatal(e)
			}
		}
		if wins != 1 {
			t.Fatalf("winners %d, errors %v", wins, errs)
		}
		entry, e := c.InspectPrivateChat(t.Context(), f.bot, f.user, "")
		if e != nil || entry.Session == nil || f.count(t, "bot_sessions", "bot_id=$1 AND route_id IS NOT NULL", f.bot) != 1 {
			t.Fatalf("split contact: %+v %v", entry, e)
		}
	})
	t.Run("unknown_commit_is_read_back_without_rebinding_or_hooks", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		s := create(t, f, thread.CreateInput{})
		before := f.hook.calls.Load()
		q := &contactFaultQueries{Queries: f.q, base: f.q, unknownCommit: true}
		if _, e := f.coordinator(q).BindPrivateChat(t.Context(), f.bot, f.user, s.ID); !errors.Is(e, db.ErrCommitOutcomeUnknown) {
			t.Fatalf("unknown: %v", e)
		}
		entry, e := f.coordinator(f.q).InspectPrivateChat(t.Context(), f.bot, f.user, "")
		if e != nil || entry.Session == nil || entry.Session.ID != s.ID {
			t.Fatalf("readback: %+v %v", entry, e)
		}
		if f.hook.calls.Load() != before {
			t.Fatal("replayed hook after unknown")
		}
	})
	t.Run("existing_empty_pointer_is_unknown_and_not_adoptable", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		f.resolve(t)
		s := create(t, f, thread.CreateInput{})
		if _, e := pool.Exec(t.Context(), "UPDATE bot_channel_routes SET active_session_id=NULL WHERE bot_id=$1", f.bot); e != nil {
			t.Fatal(e)
		}
		if _, e := f.coordinator(f.q).BindPrivateChat(t.Context(), f.bot, f.user, s.ID); !errors.Is(e, route.ErrContactUnavailable) {
			t.Fatalf("blindly repaired empty pointer: %v", e)
		}
	})

	t.Run("ordinary_role_cannot_list_or_adopt_another_teams_candidate", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		candidate := create(t, f, thread.CreateInput{})
		conn := rlsConn(t, pool, teamMigrationDSN(t))
		var role string
		if e := conn.QueryRow(t.Context(), "SELECT current_user").Scan(&role); e != nil {
			t.Fatal(e)
		}
		isolated := cfg.Copy()
		isolated.ConnConfig.RuntimeParams["role"] = role
		isolated.ConnConfig.RuntimeParams["memoh.team_id"] = uuid.NewString()
		restricted, e := pgxpool.NewWithConfig(t.Context(), isolated)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(restricted.Close)
		q := postgresstore.NewQueriesWithPool(restricted, sqlc.New(restricted))
		c := f.coordinator(q)
		entry, e := c.InspectPrivateChat(t.Context(), f.bot, f.user, "", candidate.ID)
		if e != nil || entry.Session != nil || len(entry.Candidates) != 0 {
			t.Fatalf("cross-team candidate leaked: %+v %v", entry, e)
		}
		if _, e = c.BindPrivateChat(t.Context(), f.bot, f.user, candidate.ID); e == nil {
			t.Fatal("cross-team candidate adopted")
		}
	})
	t.Run("old_delegated_chat_is_excluded_without_hiding_direct_specialist_chat_or_changing_unknown", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		source := newContactLifecycleFixture(t, pool)
		root := create(t, source, thread.CreateInput{CreatedByUserID: f.user})
		delegated := create(t, f, thread.CreateInput{})
		direct := create(t, f, thread.CreateInput{Title: "Human direct chat"})
		id := uuid.NewString()
		if _, e := pool.Exec(t.Context(), "INSERT INTO bot_delegations(id,user_id,source_bot_id,source_session_id,source_root_id,target_bot_id,target_session_id,request_id,run_id,fingerprint,source_selection,state,delivery_state) VALUES($1,$2,$3,$4,$4,$5,$6,$7,$8,'proof','proof','unknown','unknown')", id, f.user, source.bot, root.ID, f.bot, delegated.ID, uuid.NewString(), uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		var before, after string
		if e := pool.QueryRow(t.Context(), "SELECT row_to_json(d)::text FROM bot_delegations d WHERE id=$1", id).Scan(&before); e != nil {
			t.Fatal(e)
		}
		c := f.coordinator(f.q)
		entry, e := c.InspectPrivateChat(t.Context(), f.bot, f.user, "", delegated.ID)
		if e != nil || len(entry.Candidates) != 1 || entry.Candidates[0].ID != direct.ID {
			t.Fatalf("delegated/direct classification: %+v %v", entry, e)
		}
		if _, e = c.BindPrivateChat(t.Context(), f.bot, f.user, delegated.ID); !errors.Is(e, route.ErrContactConflict) {
			t.Fatalf("adopted old delegated chat: %v", e)
		}
		if _, e = c.BindPrivateChat(t.Context(), f.bot, f.user, direct.ID); e != nil {
			t.Fatal(e)
		}
		if e = pool.QueryRow(t.Context(), "SELECT row_to_json(d)::text FROM bot_delegations d WHERE id=$1", id).Scan(&after); e != nil || before != after {
			t.Fatal("navigation changed unknown delegation ledger")
		}
	})
	t.Run("pagination_preserves_all_candidates_without_a_selection", func(t *testing.T) {
		f := newContactLifecycleFixture(t, pool)
		for i := 0; i < 53; i++ {
			if _, e := pool.Exec(t.Context(), "INSERT INTO bot_sessions(id,bot_id,created_by_user_id,channel_type,type,session_mode,runtime_type,visibility,title) VALUES($1,$2,$3,'local','chat','chat','model','user','same title')", uuid.NewString(), f.bot, f.user); e != nil {
				t.Fatal(e)
			}
		}
		c := f.coordinator(f.q)
		first, e := c.InspectPrivateChat(t.Context(), f.bot, f.user, "")
		if e != nil || len(first.Candidates) != 50 || first.NextCursor == "" {
			t.Fatalf("first page: %d %v", len(first.Candidates), e)
		}
		next, e := c.InspectPrivateChat(t.Context(), f.bot, f.user, first.NextCursor)
		if e != nil || len(next.Candidates) != 3 || next.NextCursor != "" {
			t.Fatalf("next page: %d %v", len(next.Candidates), e)
		}
		selected := next.Candidates[len(next.Candidates)-1].ID
		preferred, e := c.InspectPrivateChat(t.Context(), f.bot, f.user, "", selected)
		if e != nil || len(preferred.Candidates) != 51 || preferred.Candidates[0].ID != selected || preferred.NextCursor != first.NextCursor {
			t.Fatalf("explicit selection outside page: %+v %v", preferred, e)
		}
		if f.count(t, "bot_channel_routes", "bot_id=$1", f.bot) != 0 {
			t.Fatal("pagination mutated bindings")
		}
	})
}
