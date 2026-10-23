package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
)

const delegationHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type delegationHTTPFixture struct {
	mu                        sync.Mutex
	row                       botdelegation.Record
	allowed                   bool
	creates, runs, deliveries atomic.Int32
}

func (*delegationHTTPFixture) ValidateSession(context.Context, string) error { return nil }
func (*delegationHTTPFixture) BotTeam(_ context.Context, bot string) (string, error) {
	if bot == taskBot || bot == taskTarget {
		return "team", nil
	}
	return "", errors.New("SECRET unknown Bot")
}

func (f *delegationHTTPFixture) Allowed(_ context.Context, user, bot, permission string) (bool, error) {
	return f.allowed && user == taskUser && (bot == taskBot || bot == taskTarget) && permission == bots.PermissionChat, nil
}

func (*delegationHTTPFixture) GetSession(_ context.Context, id string) (thread.Thread, error) {
	s := thread.Thread{ID: id, TeamID: "team", CreatedByUserID: taskUser, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}}
	switch id {
	case taskRoot:
		s.BotID = taskBot
	case taskOp:
		s.BotID = taskTarget
	default:
		return thread.Thread{}, errors.New("SECRET session")
	}
	return s, nil
}

type delegationHTTPSessions struct{ f *delegationHTTPFixture }

func (s delegationHTTPSessions) Get(ctx context.Context, id string) (thread.Thread, error) {
	return s.f.GetSession(ctx, id)
}

func (*delegationHTTPFixture) SessionHasAsset(_ context.Context, bot, session, hash string) (bool, error) {
	return bot == taskBot && session == taskRoot && hash == delegationHash, nil
}

func (*delegationHTTPFixture) ResultHasAsset(_ context.Context, bot, user, root, hash string) (bool, error) {
	return bot == taskTarget && user == taskUser && root == taskOp && hash == delegationHash, nil
}

func (*delegationHTTPFixture) Stat(_ context.Context, bot, hash string) (media.Asset, error) {
	if bot != taskBot || hash != delegationHash {
		return media.Asset{}, errors.New("SECRET wrong original owner")
	}
	return media.Asset{BotID: bot, ContentHash: hash, Mime: "text/plain", SizeBytes: 18}, nil
}

func (f *delegationHTTPFixture) Open(ctx context.Context, bot, hash string) (io.ReadCloser, media.Asset, error) {
	a, e := f.Stat(ctx, bot, hash)
	return io.NopCloser(strings.NewReader("synthetic original")), a, e
}

type delegationTestSeekReader struct{ *strings.Reader }

func (delegationTestSeekReader) Close() error { return nil }

func (f *delegationHTTPFixture) OpenSeekable(ctx context.Context, bot, hash string) (media.ReadSeekCloser, media.Asset, error) {
	a, err := f.Stat(ctx, bot, hash)
	return delegationTestSeekReader{strings.NewReader("synthetic original")}, a, err
}

func (f *delegationHTTPFixture) Claim(_ context.Context, r botdelegation.Record) (botdelegation.Record, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.row.ID != "" {
		if f.row.Fingerprint != r.Fingerprint {
			return botdelegation.Record{}, false, botdelegation.ErrConflict
		}
		return f.row, false, nil
	}
	r.CreatedAt = time.Now().UTC()
	r.UpdatedAt = r.CreatedAt
	f.row = r
	return r, true, nil
}

func (f *delegationHTTPFixture) Get(_ context.Context, bot, user, session, id string) (botdelegation.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.row
	if r.ID == "" || r.ID != id || r.Identity.SourceBotID != bot || r.Identity.UserID != user || r.Identity.SourceSessionID != session {
		return botdelegation.Record{}, pgx.ErrNoRows
	}
	return r, nil
}

func (f *delegationHTTPFixture) ForTarget(_ context.Context, bot, session string) (botdelegation.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.row.ID != "" && f.row.Identity.TargetBotID == bot && f.row.Identity.TargetSessionID == session {
		return f.row, nil
	}
	return botdelegation.Record{}, pgx.ErrNoRows
}

func (f *delegationHTTPFixture) Save(_ context.Context, r botdelegation.Record) (botdelegation.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Revision != f.row.Revision {
		return botdelegation.Record{}, botdelegation.ErrConflict
	}
	r.Revision++
	r.UpdatedAt = time.Now().UTC()
	f.row = r
	return r, nil
}

func (f *delegationHTTPFixture) List(_ context.Context, filter botdelegation.Filter) ([]botdelegation.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.row.ID != "" && filter.BeforeID == "" {
		return []botdelegation.Record{f.row}, nil
	}
	return nil, nil
}

func (*delegationHTTPFixture) Plan(context.Context, botdelegation.Identity, string) ([]botdelegation.DevicePair, string, error) {
	return nil, "native", nil
}

func (f *delegationHTTPFixture) CreateSession(context.Context, botdelegation.Record) (string, error) {
	f.creates.Add(1)
	return taskOp, nil
}

func (f *delegationHTTPFixture) Run(_ context.Context, _ botdelegation.Record, started func(string) error) (botdelegation.Result, string, error) {
	f.runs.Add(1)
	if e := started(uuid.NewString()); e != nil {
		return botdelegation.Result{}, "unknown", e
	}
	return botdelegation.Result{Summary: &botdelegation.Summary{Text: "Synthetic specialist final", Source: "assistant_final"}, Outputs: []string{delegationHash}}, "completed", nil
}

func (f *delegationHTTPFixture) Deliver(context.Context, botdelegation.Record) error {
	f.deliveries.Add(1)
	return nil
}

func TestBotDelegationJWTSubmitObserveOriginalAndRevocation(t *testing.T) {
	f := &delegationHTTPFixture{allowed: true}
	a := &botdelegation.Authority{Permissions: f, Sessions: delegationHTTPSessions{f}, Assets: f}
	service := botdelegation.NewService(a, f, f, slog.New(slog.DiscardHandler))
	service.SetCursorSecret("synthetic")
	service.Host = f
	t.Cleanup(service.Close)
	outer := &taskHTTPFixture{chat: true}
	h := NewBotDelegationHandler(service, bots.NewService(nil, outer), newTestAdminAccountService("user"))
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	e.Use(auth.JWTMiddleware("synthetic", func(echo.Context) bool { return false }))
	h.Register(e)
	e.GET("/adjacent", func(c echo.Context) error { return c.NoContent(200) })
	base := "/bots/" + taskBot + "/sessions/" + taskRoot + "/delegations"
	call := func(method, path, user, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if user != "" {
			token, _, err := auth.GenerateToken(user, "synthetic", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "SECRET") {
			t.Fatal("private diagnostic exposed")
		}
		if path != "/adjacent" && rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("missing no-store on %d", rec.Code)
		}
		return rec
	}
	call("GET", base, "", "", 401)
	call("GET", base+"/"+taskOp, "", "", 401)
	call("GET", base+"/"+taskOp+"/attachments/"+delegationHash, "", "", 401)
	call("GET", base, taskUser, "", 200)
	call("GET", base+"?cursor=bad", taskUser, "", 400)
	call("GET", base+"?limit=0", taskUser, "", 400)
	call("GET", base+"/"+taskOp, taskUser, "", 404)
	call("DELETE", base, taskUser, "", 405)
	call("GET", base+"/"+taskOp+"/missing", taskUser, "", 404)
	input := botdelegation.Input{RequestID: uuid.NewString(), TargetBotID: taskTarget, Message: "Synthetic delegated task", Attachments: []botdelegation.AttachmentInput{{ContentHash: delegationHash}}}
	raw, _ := json.Marshal(input)
	accepted := call("POST", base, taskUser, string(raw), 202)
	var view botdelegation.View
	if err := json.Unmarshal(accepted.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		r, err := service.Get(t.Context(), taskBot, taskUser, taskRoot, view.ID)
		if err != nil {
			t.Fatal(err)
		}
		if r.DeliveryState == "accepted" {
			view = r
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handoff did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	detail := base + "/" + view.ID
	call("POST", base, taskUser, string(raw), 202)
	if len(view.Attachments) != 1 || view.Attachments[0].URL == "" {
		t.Fatal("actual scoped reference missing")
	}
	if body := call("GET", view.Attachments[0].URL, taskUser, "", 200).Body.String(); body != "synthetic original" {
		t.Fatal("returned different original")
	}
	callRange := func(method, user, value, ifRange string, status int, contentRange, body string) {
		t.Helper()
		req := httptest.NewRequest(method, view.Attachments[0].URL, nil)
		req.Header.Set("Range", value)
		req.Header.Set("If-Range", ifRange)
		if user != "" {
			token, _, err := auth.GenerateToken(user, "synthetic", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != status || rec.Header().Get("Content-Range") != contentRange || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("scoped range: status=%d range=%q cache=%q", rec.Code, rec.Header().Get("Content-Range"), rec.Header().Get("Cache-Control"))
		}
		if status < 400 && rec.Body.String() != body {
			t.Fatalf("scoped range bytes=%q want=%q", rec.Body.String(), body)
		}
	}
	callRange("GET", taskUser, "bytes=2-6", "", 206, "bytes 2-6/18", "nthet")
	callRange("GET", taskUser, "bytes=-4", "", 206, "bytes 14-17/18", "inal")
	callRange("GET", taskUser, "bytes=2-6", `"`+delegationHash+`"`, 206, "bytes 2-6/18", "nthet")
	callRange("GET", taskUser, "bytes=2-6", `"different"`, 200, "", "synthetic original")
	callRange("GET", taskUser, "bytes=18-", "", 416, "bytes */18", "")
	callRange("HEAD", taskUser, "", "", 200, "", "")
	callRange("GET", "", "bytes=2-6", `"`+delegationHash+`"`, 401, "", "")
	before := f.row.Revision
	for range 3 {
		call("GET", base, taskUser, "", 200)
		call("GET", detail, taskUser, "", 200)
	}
	if f.row.Revision != before || f.runs.Load() != 1 || f.creates.Load() != 1 || f.deliveries.Load() != 1 {
		t.Fatal("read/replay dispatched or mutated")
	}
	call("GET", detail, taskOwner, "", 404)
	call("GET", strings.Replace(detail, taskRoot, taskChild, 1), taskUser, "", 404)
	call("GET", detail+"/attachments/"+strings.Repeat("b", 64), taskUser, "", 404)
	f.allowed = false
	callRange("GET", taskUser, "bytes=2-6", `"`+delegationHash+`"`, 404, "", "")
	call("GET", detail, taskUser, "", 404)
	call("GET", view.Attachments[0].URL, taskUser, "", 404)
	call("POST", detail+"/delivery", taskUser, "", 404)
	call("POST", base, taskUser, string(raw), 404)
	if body := call("GET", base, taskUser, "", 404).Body.String(); strings.Contains(body, "Synthetic specialist") {
		t.Fatal("old detail retained")
	}
	outer.chat = false
	call("GET", base, taskUser, "", 403)
	if c := call("GET", "/adjacent", taskUser, "", 200).Header().Get("Cache-Control"); c != "" {
		t.Fatal("changed adjacent policy")
	}
}
