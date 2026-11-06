package handlers

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// Failure metadata remains independent of the fixed contact's denied history
// mutations. Both resources share the actual authenticated HTTP registration.
func TestUnifiedFixedContactUploadFailureBoundary(t *testing.T) {
	f := newUploadHTTPFixture(t)
	e, ok := f.server.Config.Handler.(*echo.Echo)
	if !ok {
		t.Fatal("expected the existing authenticated Echo fixture")
	}
	(&SessionHandler{}).Register(e)
	bot := "/bots/" + invocationTestBotID
	path := bot + "/media/upload-failures/" + strings.Repeat("b", 32)
	body := []byte(`{"version":1,"phase":"source-open","code":"VOICE_UNAVAILABLE","sdk_code":13900012,"at":1}`)
	first := f.request(t, http.MethodPut, path, body, http.StatusOK)
	f.request(t, http.MethodPost, bot+"/sessions", []byte(`{}`), http.StatusForbidden)
	f.request(t, http.MethodDelete, bot+"/messages", nil, http.StatusForbidden)
	if got := f.request(t, http.MethodPut, path, body, http.StatusOK); !bytes.Equal(got, first) {
		t.Fatal("closed contact mutations changed the original metadata acknowledgement")
	}
	before := f.request(t, http.MethodGet, path, nil, http.StatusOK)
	f.queries.revoked.Store(true)
	f.request(t, http.MethodPut, path, body, http.StatusForbidden)
	f.request(t, http.MethodGet, path, nil, http.StatusForbidden)
	f.queries.revoked.Store(false)
	if got := f.request(t, http.MethodGet, path, nil, http.StatusOK); !bytes.Equal(got, before) {
		t.Fatal("revoked metadata requests changed the original record")
	}
}
