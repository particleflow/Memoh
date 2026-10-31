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
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/userruntime"
)

func TestRetiredEndpointHTTPHasNoConfigurationOrExecution(t *testing.T) {
	const secret = "synthetic-endpoint-http-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	owner, runtime := uuid.NewString(), uuid.NewString()
	target := agenvo.Target{ConnectionID: uuid.NewString(), DeviceID: "synthetic-device", InstanceID: "synthetic-herdr", Session: "default", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\bash.exe`}
	raw, _ := json.Marshal(target)
	store := &admissionHTTPStore{owner: owner, runtime: runtime, config: raw}
	service := userruntime.NewService(store, nil)
	calls := 0
	service.SetAgenvoService(agenvo.NewService(store, nil, func(context.Context, agenvo.Scope, agenvo.Target, string, map[string]any) (agenvo.Receipt, error) {
		calls++
		t.Fatal("owner control or retired route dispatched native operation")
		return agenvo.Receipt{}, agenvo.ErrUnknown
	}))
	capture := &privateDeviceHTTPHandlers{handlers: []server.Handler{NewUsersHandler(nil, newTestAdminAccountService("admin"), nil, nil, nil, nil, nil, nil), NewUserRuntimeHandler(nil, service)}}
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
	request(http.MethodGet, sign(uuid.NewString()), "", 404)
	token := sign(owner)
	request(http.MethodGet, token, "", 404)
	request(http.MethodPut, token, `{}`, 404)
	for _, retired := range []struct{ method, path string }{
		{http.MethodPost, "/users/me/runtimes/agenvo/discover"},
		{http.MethodPut, "/users/me/runtimes/" + runtime + "/agenvo-agent"},
	} {
		req := httptest.NewRequest(retired.method, retired.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		capture.echo.ServeHTTP(rec, req)
		if rec.Code != 404 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("retired route %s returned %d", retired.path, rec.Code)
		}
	}
	if store.reads != 0 || store.writes != 0 || calls != 0 {
		t.Fatal("retired route touched stored configuration or native transport")
	}
}
