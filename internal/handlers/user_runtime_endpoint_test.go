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
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/userruntime"
)

func (s *admissionHTTPStore) ConfigureAgenvoRuntime(_ context.Context, owner, id string, before, after dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	if owner != s.owner || id != s.runtime || string(before) != string(s.config) {
		return dbstore.UserRuntimeRecord{}, db.ErrNotFound
	}
	if s.row.Paused {
		return dbstore.UserRuntimeRecord{}, db.ErrComputerAdmissionPaused
	}
	s.writes++
	s.config = after
	return dbstore.UserRuntimeRecord{ID: id, UserID: owner, ProviderConfig: after}, nil
}

func TestOwnerEndpointHTTPAuthenticationConfigurationAndNoExecution(t *testing.T) {
	const secret = "synthetic-endpoint-http-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	owner, runtime := uuid.NewString(), uuid.NewString()
	target := agenvo.Target{ConnectionID: uuid.NewString(), DeviceID: "synthetic-device", InstanceID: "synthetic-herdr", Session: "default", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\bash.exe`}
	raw, _ := json.Marshal(target)
	store := &admissionHTTPStore{owner: owner, runtime: runtime, config: raw}
	service := userruntime.NewService(store, nil)
	calls := 0
	service.SetAgenvoService(agenvo.NewService(store, nil, func(_ context.Context, user, connection, tool string, args map[string]any) (json.RawMessage, error) {
		calls++
		if user != owner || connection != target.ConnectionID || tool != "execute" || !strings.Contains(args["code"].(string), `"workspace.list"`) {
			t.Fatal("endpoint configuration attempted an unexpected native call")
		}
		text := `{"execution":"accepted","result":{"session":"default","result":{"workspaces":[]}}}`
		out, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": text}}})
		return out, nil
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
		req := httptest.NewRequest(method, "/users/me/runtimes/"+runtime+"/agenvo-endpoint", strings.NewReader(body))
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
		t.Fatal("anonymous reached store")
	}
	request(http.MethodGet, sign(uuid.NewString()), "", 403)
	token := sign(owner)
	var initial agenvo.EndpointConfiguration
	if err := json.Unmarshal(request(http.MethodGet, token, "", 200), &initial); err != nil || initial.Locator != nil || calls != 0 {
		t.Fatal("GET inferred a location or contacted native", err)
	}
	for _, body := range []string{`null`, `{}`, `{"locator":{}}`, `{"locator":{},"owner_id":"forged"}`, `{"locator":{"program_path":"C:\\Host\\entry","host_root":"C:\\Host","command":"stop"}}`} {
		request(http.MethodPut, token, body, 400)
	}
	if calls != 0 || store.writes != 0 {
		t.Fatal("invalid request caused a side effect")
	}
	put, _ := json.Marshal(agenvo.ChangeEndpointConfiguration{ExpectedConfigurationFingerprint: initial.ConfigurationFingerprint, Locator: &agenvo.EndpointLocator{ProgramPath: `C:\Endpoint\endpoint.mjs`, HostRoot: `D:\Host data`}})
	var configured agenvo.EndpointConfiguration
	if err := json.Unmarshal(request(http.MethodPut, token, string(put), 200), &configured); err != nil || configured.TargetFingerprint != initial.TargetFingerprint || configured.Locator == nil {
		t.Fatal("configuration lost original task identity", err)
	}
	request(http.MethodPut, token, string(put), 409)
	if calls != 1 || store.writes != 1 {
		t.Fatal("stale update caused side effect")
	}
	store.row.Paused = true
	put, _ = json.Marshal(agenvo.ChangeEndpointConfiguration{ExpectedConfigurationFingerprint: configured.ConfigurationFingerprint, Locator: &agenvo.EndpointLocator{}})
	result := request(http.MethodPut, token, string(put), 409)
	if !strings.Contains(string(result), `"code":"computer.admission_paused"`) || store.writes != 1 {
		t.Fatal("live pause allowed reconfiguration")
	}
}
