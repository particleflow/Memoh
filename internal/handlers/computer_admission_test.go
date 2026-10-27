package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/userruntime"
)

type admissionHTTPStore struct {
	dbstore.UserRuntimeStore
	dbstore.AgenvoRuntimeStore
	owner, runtime string
	config         dbstore.JSON
	row            dbstore.ComputerAdmission
	reads, writes  int
}

func (s *admissionHTTPStore) OwnedNativeHerdrRuntime(_ context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	s.reads++
	if owner != s.owner || id != s.runtime {
		return dbstore.UserRuntimeRecord{}, db.ErrNotFound
	}
	return dbstore.UserRuntimeRecord{ID: id, UserID: owner, Provider: "memoh", ProviderConfig: s.config}, nil
}

func (s *admissionHTTPStore) ReadComputerAdmission(context.Context, string, string, string, string) (dbstore.ComputerAdmission, error) {
	return s.row, nil
}

func (s *admissionHTTPStore) ReadNativeAdmissionSnapshot(ctx context.Context, owner, id string) (dbstore.NativeAdmissionSnapshot, error) {
	row, err := s.OwnedNativeHerdrRuntime(ctx, owner, id)
	out := dbstore.NativeAdmissionSnapshot{RuntimeID: row.ID, Provider: row.Provider, ProviderConfig: row.ProviderConfig}
	if s.row.Revision > 0 {
		gate := s.row
		out.Gate = &gate
	}
	return out, err
}

func (s *admissionHTTPStore) ChangeComputerAdmission(_ context.Context, _, _ string, _ dbstore.JSON, request string, revision int64, paused bool) (dbstore.ComputerAdmission, error) {
	if revision != s.row.Revision {
		return dbstore.ComputerAdmission{}, db.ErrComputerAdmissionConflict
	}
	s.writes++
	s.row.RequestID = request
	s.row.Revision++
	s.row.Paused = paused
	return s.row, nil
}

func TestComputerAdmissionHTTPUsesOwnerSessionAndRejectsRouteInjection(t *testing.T) {
	const secret = "synthetic-admission-http-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	owner, runtime := uuid.NewString(), uuid.NewString()
	target := agenvo.Target{InstanceID: "synthetic-herdr", Session: "default", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\bash.exe`}
	raw, _ := json.Marshal(map[string]any{"private_fixture": "SYNTHETIC-PRIVATE-CONFIG", "herdr": agenvo.NativeHerdrProfile{InstanceID: target.InstanceID, Session: target.Session, WorkDir: target.WorkDir, BashPath: target.BashPath, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\Pi`}}})
	store := &admissionHTTPStore{owner: owner, runtime: runtime, config: raw, row: dbstore.ComputerAdmission{ConnectionID: runtime, DeviceID: runtime, InstanceID: target.InstanceID}}
	service := userruntime.NewService(store, nil)
	calls := 0
	service.SetAgenvoService(agenvo.NewService(store, nil, func(context.Context, agenvo.Scope, agenvo.Target, string, map[string]any) (agenvo.Receipt, error) {
		calls++
		t.Fatal("owner control or retired route dispatched native operation")
		return agenvo.Receipt{}, agenvo.ErrUnknown
	}))
	capture := &privateDeviceHTTPHandlers{handlers: []server.Handler{NewUserRuntimeHandler(nil, service)}}
	_ = server.NewServerWithSessionValidator(slog.New(slog.DiscardHandler), ":0", secret, func(context.Context, string) error { return nil }, capture)
	sign := func(user string) string {
		t.Helper()
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user, "user_id": user, "exp": time.Now().Add(time.Minute).Unix()}).SignedString([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	request := func(method, token, body string, status int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, "/users/me/runtimes/"+runtime+"/admission", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		capture.echo.ServeHTTP(rec, req)
		if rec.Code != status || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %d %s", method, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}
	request(http.MethodGet, "", "", 401)
	request(http.MethodPut, "", `{}`, 401)
	if store.reads != 0 {
		t.Fatal("anonymous reached owner store")
	}
	request(http.MethodGet, sign(uuid.NewString()), "", 403)
	token := sign(owner)
	responseGET := request(http.MethodGet, token, "", 200)
	var readback map[string]any
	if err := json.Unmarshal(responseGET, &readback); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"connection_id": true, "device_id": true, "instance_id": true, "request_id": true, "revision": true, "paused": true, "scope": true, "config_sha256": true, "target_fingerprint": true, "agent_config_dir": true}
	for key := range readback {
		if !allowed[key] {
			t.Fatalf("unexpected public field %s", key)
		}
	}
	if readback["agent_config_dir"] != `C:\Synthetic\Pi` {
		t.Fatal("missing profile directory")
	}
	if len(readback["config_sha256"].(string)) != 64 || len(readback["target_fingerprint"].(string)) != 64 || strings.Contains(string(responseGET), "SYNTHETIC-PRIVATE-CONFIG") {
		t.Fatal("invalid or leaking readback", string(responseGET))
	}
	chatToken, _, err := auth.GenerateChatToken(auth.ChatToken{BotID: "synthetic-bot", ChatID: "synthetic-chat", UserID: owner}, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request(http.MethodGet, chatToken, "", 403)
	if calls != 0 || store.writes != 0 {
		t.Fatal("GET modified native or gate")
	}
	request(http.MethodPut, token, `{"paused":true,"owner_id":"forged"}`, 400)
	request(http.MethodPut, token, `null`, 400)
	request(http.MethodPut, token, `{"paused":true,"request_id":"invalid","expected_revision":"0"}`, 400)
	if calls != 0 {
		t.Fatal("invalid input reached native")
	}
	body := `{"paused":true,"request_id":"` + uuid.NewString() + `","expected_revision":"0"}`
	request(http.MethodPut, token, body, 200)
	response := request(http.MethodPut, token, body, 409) // fixture CAS; DB separately tests idempotency.
	if !strings.Contains(string(response), `"code":"computer.admission_conflict"`) || store.writes != 1 || calls != 0 {
		t.Fatal("conflict contract", string(response), store.writes, calls)
	}
}
