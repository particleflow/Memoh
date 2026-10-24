//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/workdir"
)

// Reuse the real migrated fixture, its role without BYPASSRLS and ledger rows.
// No device/provider is contacted and no business task is dispatched.
func exerciseSessionDeliveryHTTP(ctx context.Context, t *testing.T, store *postgresstore.Store, id computertask.DeliveryIdentity, count int) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	queries := postgresstore.NewQueriesWithPool(store.Pool(), store.SQLC())
	botService := bots.NewService(log, queries)
	accountService := accounts.NewService(log, store)
	calls := 0
	targets := agenvo.NewService(store, sessionDeliveryPermissions{botService, accountService}, func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
		calls++
		return nil, errors.New("unexpected device call")
	})
	tasks := sessiontask.NewService(store, thread.NewService(log, queries, nil), workdir.NewService(store, nil), targets, "synthetic-account-secret")
	handler := handlers.NewSessionTaskHandler(tasks, botService, accountService, nil)
	app := server.NewServer(log, ":0", "synthetic-account-secret", handler) //nolint:contextcheck // Real server middleware handles request context.
	token, _, err := auth.GenerateToken(id.Binding.UserID, "synthetic-account-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	path := "/bots/" + id.Binding.BotID + "/sessions/" + id.Binding.SessionID + "/tasks/computer_operation/" + id.OperationID + "/deliveries"
	call := func(query, credential string, status int) computertask.DeliveryPage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path+query, nil).WithContext(ctx)
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		response := httptest.NewRecorder()
		app.ServeHTTP(response, req)
		if response.Code != status || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("HTTP status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
		}
		var page computertask.DeliveryPage
		if status == 200 {
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
		}
		return page
	}
	call("", "", 401)
	call("?after=bad", token, 400)
	cursor, seen, pages := "", 0, 0
	revision := ""
	for {
		page := call("?after="+cursor, token, 200)
		if len(page.Files) > 16 || (len(page.Files) > 0 && page.NextAfter == cursor) {
			t.Fatal("unbounded/nonadvancing HTTP page")
		}
		if revision == "" {
			revision = page.Revision
		}
		if page.Revision != revision {
			t.Fatal("GET changed operation revision")
		}
		for _, file := range page.Files {
			if strings.HasPrefix(file.DeliveryID, "metadata_") {
				seen++
			}
			if file.State == "committed" && file.Reference == nil {
				t.Fatal("committed reference missing")
			}
		}
		cursor = page.NextAfter
		pages++
		if !page.HasMore {
			break
		}
		if pages > count {
			t.Fatal("pagination did not terminate")
		}
	}
	if seen != count || calls != 0 {
		t.Fatalf("metadata=%d want=%d deviceCalls=%d", seen, count, calls)
	}
	t.Logf("real Server+JWT+PG/RLS HTTP delivery pages=%d metadata=%d; no device call; GET revision unchanged", pages, seen)
}

type sessionDeliveryPermissions struct {
	bots     *bots.Service
	accounts *accounts.Service
}

func (p sessionDeliveryPermissions) HasBotPermission(ctx context.Context, bot, user, permission string) (bool, error) {
	admin, err := p.accounts.IsAdmin(ctx, user)
	if err != nil {
		return false, err
	}
	permissions, err := p.bots.ResolveUserPermissions(ctx, bot, user, admin)
	return err == nil && bots.HasPermission(permissions, permission), err
}
