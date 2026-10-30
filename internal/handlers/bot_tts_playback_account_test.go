package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/felinics/twilight/sdk"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/audio/input"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/settings"
)

type playbackBoundaryAccounts struct {
	dbstore.AccountStore
	active bool
	reads  int
}

func (s *playbackBoundaryAccounts) GetByUserID(_ context.Context, user string) (dbstore.AccountRecord, error) {
	s.reads++
	return dbstore.AccountRecord{ID: user, Role: "member", IsActive: s.active}, nil
}

type playbackBoundaryQueries struct {
	*sessionInvocationQueries
	reads int
}

func (q *playbackBoundaryQueries) GetBotByID(ctx context.Context, id pgtype.UUID) (sqlc.GetBotByIDRow, error) {
	q.reads++
	return q.sessionInvocationQueries.GetBotByID(ctx, id)
}

type playbackBoundarySettings struct{ calls int }

func (s *playbackBoundarySettings) GetBot(context.Context, string) (settings.Settings, error) {
	s.calls++
	return settings.Settings{TtsModelID: "existing-configured-model"}, nil
}

func TestBotPlaybackAccountBoundary(t *testing.T) {
	testBotPlaybackAccountBoundary(t, false)
}

type playbackIdentityAudio struct {
	*playbackAudio
	identityCalls int
}

func (a *playbackIdentityAudio) PlaybackIdentity(context.Context, string) (string, error) {
	a.identityCalls++
	return "opaque-test-identity", nil
}

func (a *playbackIdentityAudio) StreamPlaybackWithIdentity(ctx context.Context, model, text string) (*sdk.SpeechStreamResult, string, error) {
	stream, err := a.StreamPlayback(ctx, model, text)
	return stream, "opaque-test-identity", err
}

func TestBotPlaybackConfigAccountBoundary(t *testing.T) {
	testBotPlaybackAccountBoundary(t, true)
}

func TestBotPlaybackInputStartAccountBoundary(t *testing.T) {
	testBotPlaybackAccountBoundary(t, false, "start")
}

func TestBotPlaybackInputAppendAccountBoundary(t *testing.T) {
	testBotPlaybackAccountBoundary(t, false, "append")
}

type boundaryInputSession struct{ audio *sdk.SpeechStreamResult }

func (s *boundaryInputSession) Audio() *sdk.SpeechStreamResult          { return s.audio }
func (*boundaryInputSession) Append(context.Context, input.Frame) error { return nil }
func (*boundaryInputSession) Cancel()                                   {}

type boundaryInputAudio struct{ *playbackIdentityAudio }

func (a *boundaryInputAudio) PlaybackInputInfo(context.Context, string) (string, string, error) {
	a.identityCalls++
	return "opaque-test-identity", "audio/wav", nil
}

func (a *boundaryInputAudio) OpenPlaybackInput(ctx context.Context, model string) (input.Session, string, error) {
	stream, err := a.StreamPlayback(ctx, model, "synthetic playback text")
	return &boundaryInputSession{stream}, "opaque-test-identity", err
}

func testBotPlaybackAccountBoundary(t *testing.T, metadata bool, native ...string) {
	mode := ""
	if len(native) > 0 {
		mode = native[0]
	}
	const inputID = "66666666-6666-4666-8666-666666666666"
	t.Helper()
	const secret = "synthetic-playback-account-boundary-secret"
	const otherBot = "44444444-4444-4444-8444-444444444444"
	for _, tc := range []struct {
		name, kind                        string
		inactive, crossBot, grant, denied bool
		status                            int
	}{
		{name: "active owner same Bot", status: http.StatusOK},
		{name: "active chat grantee same Bot", grant: true, status: http.StatusOK},
		{name: "active account without chat permission", denied: true, status: http.StatusForbidden},
		{name: "disabled owner account", inactive: true, status: http.StatusUnauthorized},
		{name: "chat route same Bot active owner", kind: "chat", status: http.StatusForbidden},
		{name: "chat route wrong Bot active owner", kind: "chat", crossBot: true, status: http.StatusForbidden},
		{name: "chat route wrong Bot chat grantee", kind: "chat", crossBot: true, grant: true, status: http.StatusForbidden},
		{name: "chat route same Bot disabled owner", kind: "chat", inactive: true, status: http.StatusForbidden},
		{name: "chat route wrong Bot disabled owner", kind: "chat", inactive: true, crossBot: true, status: http.StatusForbidden},
		{name: "anonymous", kind: "anonymous", status: http.StatusUnauthorized},
		{name: "expired account", kind: "expired", status: http.StatusUnauthorized},
	} {
		for _, query := range []bool{false, true} {
			transport := "header"
			if query {
				transport = "query"
			}
			t.Run(tc.name+"/"+transport, func(t *testing.T) {
				target := invocationTestBotID
				if tc.crossBot {
					target = otherBot
				}
				q := &playbackBoundaryQueries{sessionInvocationQueries: newInvocationTestQueries()}
				q.bot = testBotRow(target, nil)
				q.bot.OwnerUserID = testUUID(invocationTestOwnerID)
				q.permissions = []byte(`[]`)
				if tc.grant || tc.denied {
					q.bot.OwnerUserID = testUUID(invocationTestOtherUser)
					if tc.grant {
						q.permissions = []byte(`["chat"]`)
					}
				}
				accountStore := &playbackBoundaryAccounts{active: !tc.inactive}
				accountService := accounts.NewService(nil, accountStore)
				audio := &playbackAudio{}
				identityAudio := &playbackIdentityAudio{playbackAudio: audio}
				var service botSpeechService = audio
				if metadata {
					service = identityAudio
				}
				if mode != "" {
					service = &boundaryInputAudio{identityAudio}
				}
				config := &playbackBoundarySettings{}
				h := &BotAudioHandler{audioService: service, settingsService: config}
				if mode == "append" {
					h.inputSessions.entries = map[string]*playbackInputSession{invocationTestOwnerID + ":" + target + ":" + inputID: {session: &boundaryInputSession{}, identity: "opaque-test-identity", modelID: "existing-configured-model", cancel: func() {}}}
				}
				h.SetPlaybackAccess(bots.NewService(nil, q), accountService)
				srv := server.NewServerWithSessionValidator(slog.New(slog.DiscardHandler), "", secret, accountService.ValidateSession, h)
				token, _, err := auth.GenerateToken(invocationTestOwnerID, secret, time.Minute)
				switch tc.kind {
				case "chat":
					token, _, err = auth.GenerateChatToken(auth.ChatToken{BotID: invocationTestBotID, ChatID: invocationTestSessionA, RouteID: "synthetic-route", UserID: invocationTestOwnerID}, secret, time.Minute)
				case "expired":
					token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": invocationTestOwnerID, "user_id": invocationTestOwnerID, "exp": time.Now().Add(-time.Minute).Unix()}).SignedString([]byte(secret))
				case "anonymous":
					token = ""
				}
				if err != nil {
					t.Fatal(err)
				}
				path := "/bots/" + target + "/tts/playback"
				method := http.MethodPost
				if metadata {
					path += "/config"
					method = http.MethodGet
				}
				if mode != "" {
					path += "/input"
					if mode == "append" {
						method = http.MethodPut
					}
				}
				if query && token != "" {
					path += "?token=" + url.QueryEscape(token)
				}
				body := `{"text":"synthetic playback text"}`
				if mode != "" {
					body = `{"id":"` + inputID + `","sequence":0,"offset":0,"text":"synthetic playback text","final":true}`
				}
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", "https://phone-fixture.invalid")
				if !query && token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				if !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "X-Speech-Playback-Identity") {
					t.Fatal("cross-origin shared client cannot read the cache identity")
				}
				t.Logf("status=%d account_reads=%d bot_reads=%d settings_reads=%d synthesis_calls=%d", rec.Code, accountStore.reads, q.reads, config.calls, audio.calls)
				expected := tc.status
				if mode == "append" && expected == http.StatusOK {
					expected = http.StatusNoContent
				}
				if rec.Code != expected {
					t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
				}
				if tc.status == http.StatusOK {
					switch {
					case mode == "append":
						if accountStore.reads != 2 || q.reads < 1 || config.calls != 1 || audio.calls != 0 || identityAudio.identityCalls != 1 {
							t.Fatal("input append skipped account/Bot/config gate or opened another synthesis")
						}
					case metadata:
						if accountStore.reads != 2 || q.reads < 1 || config.calls != 1 || audio.calls != 0 || identityAudio.identityCalls != 1 || !strings.Contains(rec.Body.String(), "opaque-test-identity") {
							t.Fatal("cached replay validation generated audio or skipped account/Bot checks")
						}
					default:
						if accountStore.reads != 2 || q.reads < 1 || config.calls != 1 || audio.calls != 1 || audio.model != "existing-configured-model" || audio.text != "synthetic playback text" || rec.Body.String() != "synthetic-audio" {
							t.Fatal("validated account playback path changed")
						}
					}
				} else {
					if config.calls != 0 || audio.calls != 0 || identityAudio.identityCalls != 0 {
						t.Fatal("rejected request reached settings or synthesis")
					}
					if tc.kind == "chat" && (accountStore.reads != 0 || q.reads != 0) {
						t.Fatal("chat-route credential reached account/Bot authorization")
					}
					if tc.inactive && tc.kind == "" && (accountStore.reads != 1 || q.reads != 0) {
						t.Fatal("inactive owner reached Bot owner authorization")
					}
				}
			})
		}
	}
}
