package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func newClientNetReportEcho(t *testing.T) (*echo.Echo, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	e := echo.New()
	NewClientNetReportHandler(slog.New(slog.NewJSONHandler(out, nil))).Register(e)
	return e, out
}

func postClientNetReport(t *testing.T, e *echo.Echo, payload string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/client/net-report", strings.NewReader(payload))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, request)
	return recorder.Code
}

func validClientNetReport() string {
	report := map[string]any{
		"schema": 1, "at": 1730680020000, "version": "1.0.44", "build": 1000044,
		"reason": "client-failed",
		"binding": map[string]any{
			"version": 1, "started": true, "boundNetId": 218, "action": "bind",
			"reason": "cellular-ipv6",
			"nets": []map[string]any{
				{"netId": 100, "wifi": true, "cellular": false, "ipv6": false},
				{"netId": 218, "wifi": false, "cellular": true, "ipv6": true},
			},
		},
		"journal": []map[string]any{
			{"at": 1730680019000, "area": "NETWORK", "component": "app-net", "phase": "bound", "detail": "net=218"},
			{"at": 1730680019500, "area": "SYSTEM", "component": "web-network", "phase": "transport-error", "detail": "code=ERR_NAME_NOT_RESOLVED"},
		},
	}
	encoded, _ := json.Marshal(report)
	return string(encoded)
}

func TestClientNetReportAcceptsValidReport(t *testing.T) {
	e, out := newClientNetReportEcho(t)
	if code := postClientNetReport(t, e, validClientNetReport()); code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", code)
	}
	logged := out.String()
	if !strings.Contains(logged, "client_net_report") || !strings.Contains(logged, "cellular-ipv6") {
		t.Fatalf("report missing from log: %s", logged)
	}
}

func TestClientNetReportAcceptsReportWithoutBindingOrJournal(t *testing.T) {
	e, _ := newClientNetReportEcho(t)
	payload := `{"schema":1,"at":1730680020000,"version":"1.0.44","build":1000044,"reason":"manual"}`
	if code := postClientNetReport(t, e, payload); code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", code)
	}
}

func TestClientNetReportRejectsBadPayloads(t *testing.T) {
	e, _ := newClientNetReportEcho(t)
	cases := map[string]string{
		"empty":          ``,
		"not-json":       `{`,
		"schema":         `{"schema":2,"at":1,"version":"1","build":1,"reason":"manual"}`,
		"at":             `{"schema":1,"at":0,"version":"1","build":1,"reason":"manual"}`,
		"reason":         `{"schema":1,"at":1,"version":"1","build":1,"reason":"unexpected"}`,
		"version":        `{"schema":1,"at":1,"version":"","build":1,"reason":"manual"}`,
		"build":          `{"schema":1,"at":1,"version":"1","build":-1,"reason":"manual"}`,
		"unknown-field":  `{"schema":1,"at":1,"version":"1","build":1,"reason":"manual","extra":1}`,
		"trailing":       validClientNetReport() + ` {}`,
		"journal-area":   `{"schema":1,"at":1,"version":"1","build":1,"reason":"manual","journal":[{"at":1,"area":"OTHER","component":"a","phase":"b","detail":""}]}`,
		"journal-token":  `{"schema":1,"at":1,"version":"1","build":1,"reason":"manual","journal":[{"at":1,"area":"NETWORK","component":"a b","phase":"b","detail":""}]}`,
		"binding-action": `{"schema":1,"at":1,"version":"1","build":1,"reason":"manual","binding":{"version":1,"started":true,"boundNetId":1,"action":"bind!","reason":"r","nets":[]}}`,
	}
	for name, payload := range cases {
		if code := postClientNetReport(t, e, payload); code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", name, code)
		}
	}
}

func TestClientNetReportRejectsOversizedBody(t *testing.T) {
	e, _ := newClientNetReportEcho(t)
	detail := strings.Repeat("a", 600)
	events := make([]string, 200)
	for i := range events {
		events[i] = `{"at":1,"area":"NETWORK","component":"c","phase":"p","detail":"` + detail + `"}`
	}
	payload := `{"schema":1,"at":1,"version":"1","build":1,"reason":"manual","journal":[` + strings.Join(events, ",") + `]}`
	if code := postClientNetReport(t, e, payload); code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", code)
	}
}
