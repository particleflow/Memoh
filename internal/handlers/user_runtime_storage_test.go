package handlers

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

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/userruntime"
)

type storageAccountStore struct {
	dbstore.UserRuntimeStore
	reads, writes int
}

func (s *storageAccountStore) OwnedNativeHerdrRuntime(_ context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	s.reads++
	if owner != "22222222-2222-4222-8222-222222222222" {
		return dbstore.UserRuntimeRecord{}, db.ErrNotFound
	}
	return dbstore.UserRuntimeRecord{ID: id, UserID: owner, Provider: "memoh", ProviderConfig: []byte(`{}`)}, nil
}

func (s *storageAccountStore) ConfigureNativeHerdrRuntime(context.Context, string, string, dbstore.JSON, dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	s.writes++
	return dbstore.UserRuntimeRecord{}, errors.New("must not write")
}

func TestWorkspaceStorageAccountBoundaryAndOfflineNoWrite(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		for _, mode := range []string{"account", "chat", "revoked", "anonymous", "other_owner", "invalid_id"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				const owner = "22222222-2222-4222-8222-222222222222"
				const secret = "synthetic-workspace-storage-session-key"
				store := &storageAccountStore{}
				svc := userruntime.NewService(store, nil)
				svc.SetWorkspaceStorage(t.TempDir(), `\\synthetic-nas\existing-share`)
				srv := server.NewServerWithSessionValidator(slog.New(slog.DiscardHandler), "", secret, func(context.Context, string) error {
					if mode == "revoked" {
						return errors.New("synthetic revoked session")
					}
					return nil
				}, NewUserRuntimeHandler(nil, svc))
				user := owner
				if mode == "other_owner" {
					user = "33333333-3333-4333-8333-333333333333"
				}
				token, _, err := auth.GenerateToken(user, secret, time.Minute)
				if mode == "chat" {
					token, _, err = auth.GenerateChatToken(auth.ChatToken{BotID: "bot", ChatID: "chat", RouteID: "route", UserID: owner}, secret, time.Minute)
				}
				if err != nil {
					t.Fatal(err)
				}
				url := "/users/me/runtimes/11111111-1111-4111-8111-111111111111/workspace-storage"
				if mode == "invalid_id" {
					url = "/users/me/runtimes/not-a-uuid/workspace-storage"
				}
				if method == http.MethodPost {
					url += "/directories"
				}
				req := httptest.NewRequest(method, url, strings.NewReader(`{"path":"."}`))
				req.Header.Set("Content-Type", "application/json")
				if mode != "anonymous" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				want := http.StatusServiceUnavailable
				switch mode {
				case "chat":
					want = http.StatusForbidden
				case "revoked", "anonymous":
					want = http.StatusUnauthorized
				case "other_owner":
					want = http.StatusNotFound
				case "invalid_id":
					want = http.StatusBadRequest
				}
				if rec.Code != want || store.writes != 0 || rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status=%d want=%d writes=%d cache=%q", rec.Code, want, store.writes, rec.Header().Get("Cache-Control"))
				}
				if mode == "account" {
					var problem struct {
						Code string `json:"code"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil || problem.Code != "workspace.storage_unavailable" || strings.Contains(rec.Body.String(), "synthetic-nas") {
						t.Fatal("wrong public NAS error or private detail leaked")
					}
				}
				if mode != "account" && mode != "other_owner" && store.reads != 0 {
					t.Fatal("invalid scope reached owner registry")
				}
			})
		}
	}
}
