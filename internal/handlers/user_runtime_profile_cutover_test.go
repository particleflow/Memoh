package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/auth"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/userruntime"
)

type profileCutoverHTTPStore struct {
	dbstore.AgenvoRuntimeStore
	row    dbstore.UserRuntimeRecord
	gate   dbstore.ComputerAdmission
	target string
	writes int
}

func (s *profileCutoverHTTPStore) OwnedNativeHerdrRuntime(_ context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	if owner != s.row.UserID || id != s.row.ID {
		return dbstore.UserRuntimeRecord{}, errors.New("not owner")
	}
	return s.row, nil
}

func (s *profileCutoverHTTPStore) ReadComputerAdmission(context.Context, string, string, string, string) (dbstore.ComputerAdmission, error) {
	return s.gate, nil
}

func (*profileCutoverHTTPStore) ChangeComputerAdmission(context.Context, string, string, dbstore.JSON, string, int64, bool) (dbstore.ComputerAdmission, error) {
	return dbstore.ComputerAdmission{}, errors.New("not used")
}

func (s *profileCutoverHTTPStore) OwnedNativeProfileTarget(_ context.Context, _, _, target string) (bool, error) {
	return target == s.target, nil
}

func (s *profileCutoverHTTPStore) CutoverPausedNativeProfile(_ context.Context, _, _, _ string, _, _ dbstore.JSON, _, _ string, _ int64) (dbstore.ComputerAdmission, error) {
	s.writes++
	s.gate.Revision++
	return s.gate, nil
}

func TestNativeProfileCutoverHTTPAccountBoundary(t *testing.T) {
	const owner = "22222222-2222-4222-8222-222222222222"
	const runtime = "11111111-1111-4111-8111-111111111111"
	const mount = "33333333-3333-4333-8333-333333333333"
	const pause = "44444444-4444-4444-8444-444444444444"
	const secret = "synthetic-cutover-http-secret"
	profile := agenvo.NativeHerdrProfile{InstanceID: "synthetic", Session: "default", WorkDir: `C:\Synthetic\work`, BashPath: `C:\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "fixture", Model: "fixture", ConfigDir: `C:\Synthetic\old`}}
	raw, _ := json.Marshal(map[string]any{"herdr": profile})
	target, err := agenvo.DecodeNativeTarget(runtime, raw)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agenvo.NativeProfileConfigDigest(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"account", "anonymous", "chat_route", "revoked", "foreign_owner", "stale_revision", "unknown_field"} {
		t.Run(scenario, func(t *testing.T) {
			store := &profileCutoverHTTPStore{row: dbstore.UserRuntimeRecord{ID: runtime, UserID: owner, Provider: "memoh", ProviderConfig: raw, ActivatedAt: time.Now()}, target: mount, gate: dbstore.ComputerAdmission{ConnectionID: runtime, DeviceID: runtime, InstanceID: profile.InstanceID, RequestID: pause, Revision: 1, Paused: true}}
			svc := userruntime.NewService(nil, nil)
			svc.SetAgenvoService(agenvo.NewService(store, nil, nil))
			log := slog.New(slog.DiscardHandler)
			srv := server.NewServerWithSessionValidator(log, "", secret, func(context.Context, string) error {
				if scenario == "revoked" {
					return errors.New("revoked")
				}
				return nil
			}, NewUserRuntimeHandler(log, svc))
			user := owner
			if scenario == "foreign_owner" {
				user = "55555555-5555-4555-8555-555555555555"
			}
			token, _, err := auth.GenerateToken(user, secret, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "chat_route" {
				token, _, err = auth.GenerateChatToken(auth.ChatToken{BotID: "bot", ChatID: "chat", UserID: owner}, secret, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
			}
			in := agenvo.NativeProfileCutoverRequest{RuntimeID: runtime, TargetID: mount, InstanceID: profile.InstanceID, ExpectedConfigSHA256: digest, ExpectedTargetFingerprint: agenvo.TargetFingerprint(target), PauseRequestID: pause, ExpectedPauseRevision: "1", AgentConfigDir: `C:\Synthetic\new`}
			if scenario == "stale_revision" {
				in.ExpectedPauseRevision = "2"
			}
			body, _ := json.Marshal(in)
			if scenario == "unknown_field" {
				body = append(body[:len(body)-1], []byte(`,"extra":"not allowed"}`)...)
			}
			req := httptest.NewRequest(http.MethodPut, "/users/me/runtimes/"+runtime+"/native-herdr/profile-cutover", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if scenario != "anonymous" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			want := map[string]int{"account": 200, "anonymous": 401, "chat_route": 403, "revoked": 401, "foreign_owner": 403, "stale_revision": 409, "unknown_field": 400}[scenario]
			if rec.Code != want || (store.writes == 1) != (scenario == "account") {
				t.Fatalf("status=%d want=%d writes=%d", rec.Code, want, store.writes)
			}
			if scenario == "account" {
				var receipt agenvo.Admission
				if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil || receipt.Revision != "2" || !receipt.Paused || receipt.RequestID != pause {
					t.Fatalf("cutover receipt: %+v %v", receipt, err)
				}
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}
		})
	}
}
