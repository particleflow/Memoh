package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/auth"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/userruntime"
)

type nativeAccountProbeStore struct {
	dbstore.AgenvoRuntimeStore
	writes int
}

func (*nativeAccountProbeStore) OwnedNativeHerdrRuntime(context.Context, string, string) (dbstore.UserRuntimeRecord, error) {
	return dbstore.UserRuntimeRecord{ID: "11111111-1111-4111-8111-111111111111", UserID: "22222222-2222-4222-8222-222222222222", Provider: "memoh", ProviderConfig: []byte(`{}`)}, nil
}

func (s *nativeAccountProbeStore) ConfigureNativeHerdrRuntime(ctx context.Context, owner, id string, _, after dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	s.writes++
	row, err := s.OwnedNativeHerdrRuntime(ctx, owner, id)
	row.ProviderConfig = after
	return row, err
}

func TestNativeHerdrConfigurationRequiresAccountSession(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		chat, revoked, query, anonymous bool
		status                          int
	}{
		{name: "account", status: http.StatusOK},
		{name: "revoked_account", revoked: true, status: http.StatusUnauthorized},
		{name: "chat_route", chat: true, status: http.StatusForbidden},
		{name: "revoked_chat_account", chat: true, revoked: true, status: http.StatusForbidden},
		{name: "chat_query", chat: true, query: true, status: http.StatusForbidden},
		{name: "anonymous", anonymous: true, status: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &nativeAccountProbeStore{}
			svc := userruntime.NewService(nil, nil)
			svc.SetAgenvoService(agenvo.NewService(store, nil, nil))
			const owner = "22222222-2222-4222-8222-222222222222"
			const secret = "synthetic-native-account-boundary-signing-secret"
			validated := 0
			log := slog.New(slog.DiscardHandler)
			srv := server.NewServerWithSessionValidator(log, "", secret, func(context.Context, string) error {
				validated++
				if tc.revoked {
					return errors.New("synthetic revoked account")
				}
				return nil
			}, NewUserRuntimeHandler(log, svc))
			token, _, err := auth.GenerateToken(owner, secret, time.Minute)
			if tc.chat {
				token, _, err = auth.GenerateChatToken(auth.ChatToken{BotID: "synthetic-bot", ChatID: "synthetic-chat", RouteID: "synthetic-route", UserID: owner}, secret, time.Minute)
			}
			if err != nil {
				t.Fatal(err)
			}
			profile := agenvo.NativeHerdrProfile{InstanceID: "synthetic-instance", Session: "default", WorkDir: `C:\Synthetic\work`, BashPath: `C:\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\config\herdr`}
			body, err := json.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			path := "/users/me/runtimes/11111111-1111-4111-8111-111111111111/native-herdr"
			if tc.query {
				path += "?token=" + url.QueryEscape(token)
			}
			req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if !tc.query && !tc.anonymous {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			t.Logf("status=%d account_validations=%d configuration_writes=%d", rec.Code, validated, store.writes)
			wantWrites, wantValidation := 0, 1
			if tc.status == http.StatusOK {
				wantWrites = 1
			}
			if tc.chat || tc.anonymous {
				wantValidation = 0
			}
			if rec.Code != tc.status || store.writes != wantWrites || validated != wantValidation {
				t.Errorf("wanted status=%d writes=%d validations=%d", tc.status, wantWrites, wantValidation)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Error("missing no-store")
			}
		})
	}
}
