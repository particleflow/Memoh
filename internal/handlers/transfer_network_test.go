package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/media"
)

func TestMediaNetworkPolicyUsesExistingAuthorization(t *testing.T) {
	for _, role := range []string{"", "user", "admin"} {
		e, service := mediaTestHTTP(t, nil, 1024, role)
		if err := service.SetTransferNetworkPolicy(&media.TransferNetworkPolicy{Version: 1, LANAddresses: []string{"192.0.2.1", "2001:db8::1"}}); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bots/"+invocationTestBotID+"/media/limits", nil))
		if role == "" {
			if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "192.0.2.1") {
				t.Fatal("anonymous policy disclosure", rec.Code)
			}
			continue
		}
		if role == "user" {
			if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "192.0.2.1") {
				t.Fatal("unauthorized Bot policy disclosure", rec.Code)
			}
			continue
		}
		var reply MediaUploadLimitsResponse
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &reply) != nil || reply.NetworkPolicy == nil || reply.NetworkPolicy.LANAddresses[1] != "2001:db8::1" || reply.MaxUploadBytes != 1024 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
}

func TestCurrentUserAndFileLimitsShareNetworkPolicy(t *testing.T) {
	s := media.NewService(nil, nil)
	if err := s.SetTransferNetworkPolicy(&media.TransferNetworkPolicy{Version: 1, LANAddresses: []string{"192.0.2.1"}}); err != nil {
		t.Fatal(err)
	}
	h := &UsersHandler{service: newTestAdminAccountService("user"), mediaService: s}
	rec := httptest.NewRecorder()
	e := echo.New()
	c := testAuthContext(e, httptest.NewRequest(http.MethodGet, "/users/me", nil), rec, invocationTestOtherUser)
	if err := h.GetMe(c); err != nil {
		t.Fatal(err)
	}
	var reply CurrentUserResponse
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &reply) != nil || reply.Role != "user" || reply.NetworkPolicy == nil || reply.NetworkPolicy.LANAddresses[0] != "192.0.2.1" {
		t.Fatal(rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable profile policy")
	}
	anonymous := e.NewContext(httptest.NewRequest(http.MethodGet, "/users/me", nil), httptest.NewRecorder())
	if err := h.GetMe(anonymous); err == nil {
		t.Fatal("anonymous profile succeeded")
	}
}
