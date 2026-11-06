package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

func TestIndependentEscapedInvocationScope(t *testing.T) {
	for _, tc := range []struct {
		name, invocation, storedSession string
		found                           bool
	}{
		{"literal_percent_slash_is_not_decoded_twice", "literal%2F..%2Fother", invocationTestSessionA, true},
		{"encoded_slash_does_not_escape_authorized_session", "follow-up:item/../other", invocationTestSessionB, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := sessionledger.New()
			admitted := admitInvocationTestRun(t, store, "run-owned-scope", tc.storedSession, tc.invocation)
			lookup := &recordingInvocationLookup{store: store}
			h := newInvocationTestHandler(newInvocationTestQueries(invocationTestSession(invocationTestSessionA, invocationTestOwnerID), invocationTestSession(invocationTestSessionB, invocationTestOwnerID)), lookup)
			e := echo.New()
			e.GET("/bots/:bot_id/sessions/:session_id/invocations/:invocation_id", func(c echo.Context) error {
				c.Set("user", &jwt.Token{Valid: true, Claims: jwt.MapClaims{"sub": invocationTestOwnerID, "user_id": invocationTestOwnerID}})
				return h.GetSessionInvocation(c)
			})
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bots/"+invocationTestBotID+"/sessions/"+invocationTestSessionA+"/invocations/"+url.PathEscape(tc.invocation), nil))
			body := decodeInvocationBody(t, rec)
			if rec.Code != http.StatusOK || body["found"] != tc.found || body["invocation_id"] != tc.invocation || body["session_id"] != invocationTestSessionA {
				t.Fatalf("scope/identity changed: status=%d body=%#v", rec.Code, body)
			}
			if tc.found && body["run_id"] != admitted.RunID {
				t.Fatal("different operation was returned")
			}
			if !tc.found && body["run_id"] != nil {
				t.Fatal("foreign operation leaked")
			}
			if len(lookup.calls) != 1 || lookup.calls[0] != invocationTestSessionA+"/"+tc.invocation {
				t.Fatalf("unscoped or double-decoded lookup: %#v", lookup.calls)
			}
		})
	}
}
