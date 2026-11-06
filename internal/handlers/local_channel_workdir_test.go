package handlers

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/channel"
	"github.com/felinics/memoh/internal/channel/route"
	sessionpkg "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

// A first Web send resolves the committed contact, including its fixed workdir.
// Omitting the workdir cannot clear it; naming a different one cannot rebind it
// or create a parallel session, even when the client supplies no session ID.
func TestLocalChannelWSFirstSendKeepsContactWorkdir(t *testing.T) {
	t.Parallel()
	const (
		botID           = "11111111-1111-1111-1111-111111111111"
		sessionID       = "22222222-2222-2222-2222-222222222222"
		currentUser     = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
		activeWorkdir   = "33333333-3333-3333-3333-333333333333"
		archivedWorkdir = "44444444-4444-4444-4444-444444444444"
		foreignWorkdir  = "55555555-5555-5555-5555-555555555555"
	)
	for _, tc := range []struct {
		name, requested, bound string
		conflict               bool
	}{
		{name: "same bound directory", requested: activeWorkdir, bound: activeWorkdir},
		{name: "omitted directory retains binding", bound: activeWorkdir},
		{name: "unbound contact stays unbound"},
		{name: "archived alternative cannot rebind", requested: archivedWorkdir, bound: activeWorkdir, conflict: true},
		{name: "foreign alternative cannot rebind", requested: foreignWorkdir, bound: activeWorkdir, conflict: true},
		{name: "directory cannot bind an existing unbound contact", requested: activeWorkdir, conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var creates atomic.Int32
			row := sqlc.BotSession{ID: testUUID(sessionID), BotID: testUUID(botID), CreatedByUserID: testUUID(currentUser), Type: sessionpkg.TypeChat, SessionMode: sessionpkg.TypeChat, RuntimeType: sessionpkg.RuntimeModel, Metadata: []byte(`{}`)}
			if tc.bound != "" {
				row.WorkdirID = testUUID(tc.bound)
			}
			queries := localChannelSessionAuthQueries{
				bot: testBotRow(botID, map[string]any{}), session: row,
				grants: []sqlc.ListBotUserGrantsForUserRow{{ID: testUUID("dddddddd-dddd-dddd-dddd-dddddddddddd"), BotID: testUUID(botID), SubjectType: bots.GrantSubjectUser, UserID: testUUID(currentUser), Permissions: []byte(`["chat","workspace_exec"]`)}},
				createSession: func(context.Context, sqlc.CreateSessionParams) (sqlc.BotSession, error) {
					creates.Add(1)
					return row, nil
				},
			}
			contact := &fixedContactStub{entry: route.PrivateChatEntry{Session: &sessionpkg.Thread{ID: sessionID, BotID: botID, CreatedByUserID: currentUser, WorkdirID: tc.bound}}}
			handler := &LocalChannelHandler{channelType: channel.ChannelTypeLocal, botService: bots.NewService(nil, queries), accountService: accounts.NewService(nil, testAdminAccountStore{role: "user"}), sessionService: sessionpkg.NewService(nil, queries, nil), agentService: &application.Service{}, privateChats: contact, logger: slog.Default()}
			client := openLocalChannelTestWS(t, handler, botID, currentUser)
			message := map[string]any{"type": "message", "invocation_id": "invocation-1", "text": "hello"}
			if tc.requested != "" {
				message["workdir_id"] = tc.requested
			}
			if err := client.WriteJSON(message); err != nil {
				t.Fatal(err)
			}
			var event map[string]any
			if err := client.ReadJSON(&event); err != nil {
				t.Fatal(err)
			}
			if tc.conflict {
				if event["type"] != "error" || event["code"] != "http.conflict" || event["invocation_id"] != "invocation-1" {
					t.Fatalf("expected conflict without a new session: %#v", event)
				}
			} else {
				if event["type"] != "session_created" || event["session_id"] != sessionID {
					t.Fatalf("first-send event must resolve the existing contact: %#v", event)
				}
				if tc.bound == "" {
					if _, ok := event["workdir_id"]; ok {
						t.Fatalf("unbound contact gained a directory: %#v", event)
					}
				} else if event["workdir_id"] != tc.bound {
					t.Fatalf("bound directory lost: %#v", event)
				}
			}
			if creates.Load() != 0 || contact.creates != 0 || contact.binds != 0 || contact.entry.Session.WorkdirID != tc.bound {
				t.Fatal("send created or rebound the committed contact")
			}
		})
	}
}
