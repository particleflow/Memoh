package handlers

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/labstack/echo/v4"
)

type fixedContactStub struct {
	mu                  sync.Mutex
	entry               route.PrivateChatEntry
	inspectErr, bindErr error
	creates, binds      int
}

func (s *fixedContactStub) InspectPrivateChat(context.Context, string, string, string, ...string) (route.PrivateChatEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entry, s.inspectErr
}
func (s *fixedContactStub) EnsurePrivateChat(context.Context, string, string) (thread.Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entry.Session == nil {
		s.creates++
		s.entry.Session = &thread.Thread{ID: "fixed", BotID: "bot"}
	}
	return *s.entry.Session, nil
}
func (s *fixedContactStub) BindPrivateChat(_ context.Context, _, _, id string) (thread.Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binds++
	if s.bindErr != nil {
		return thread.Thread{}, s.bindErr
	}
	if s.entry.Session != nil {
		return thread.Thread{}, route.ErrContactConflict
	}
	s.entry.Session = &thread.Thread{ID: id, BotID: "bot"}
	return *s.entry.Session, nil
}

func TestFixedContactPublicMutationRoutesClosed(t *testing.T) {
	e := echo.New()
	(&SessionHandler{}).Register(e)
	(&MessageHandler{}).Register(e)
	for _, v := range []struct{ method, path string }{{"POST", "/bots/b/sessions"}, {"POST", "/bots/b/sessions/contact/bind"}, {"POST", "/bots/b/sessions/s/fork"}, {"DELETE", "/bots/b/sessions/s"}, {"DELETE", "/bots/b/messages"}} {
		t.Run(v.method+v.path, func(t *testing.T) {
			c := e.NewContext(httptest.NewRequest(v.method, v.path, strings.NewReader(`{"runtime_type":"codex","type":"subagent"}`)), httptest.NewRecorder())
			e.Router().Find(v.method, v.path, c)
			err := c.Handler()(c)
			if apperror.CodeOf(err) != apperror.CodeHTTPForbidden {
				t.Fatalf("mutation not refused: %v", err)
			}
		})
	}
}
func TestFixedContactSystemFirstBinding(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		t.Run(string(rune('0'+n)), func(t *testing.T) {
			s := &fixedContactStub{}
			for i := 0; i < n; i++ {
				s.entry.Candidates = append(s.entry.Candidates, thread.Thread{ID: "old", BotID: "bot"})
			}
			got, err := resolveContinuousContact(t.Context(), s, "bot", "user")
			if n == 2 {
				if err == nil || s.creates != 0 || s.binds != 0 {
					t.Fatalf("guessed legacy root: %+v %v", s, err)
				}
				return
			}
			if err != nil || got.ID == "" {
				t.Fatal(got, err)
			}
			again, err := resolveContinuousContact(t.Context(), s, "bot", "user")
			if err != nil || again.ID != got.ID || s.creates+s.binds != 1 {
				t.Fatal("changed contact on reopen")
			}
		})
	}
}
func TestFixedContactFailureNeverCreatesReplacement(t *testing.T) {
	for _, cause := range []error{errors.New("read unavailable"), route.ErrContactUnavailable} {
		s := &fixedContactStub{inspectErr: cause}
		if _, err := resolveContinuousContact(t.Context(), s, "bot", "user"); err == nil || s.creates != 0 {
			t.Fatal("created after failed read")
		}
	}
	s := &fixedContactStub{entry: route.PrivateChatEntry{Candidates: []thread.Thread{{ID: "old"}}}, bindErr: db.ErrCommitOutcomeUnknown}
	if _, err := resolveContinuousContact(t.Context(), s, "bot", "user"); !errors.Is(err, db.ErrCommitOutcomeUnknown) || s.binds != 1 || s.creates != 0 {
		t.Fatal("replayed unknown", err)
	}
}
func TestFixedContactLegacyHistoryReadOnlyAndWorkdirUnchanged(t *testing.T) {
	s := &fixedContactStub{entry: route.PrivateChatEntry{Session: &thread.Thread{ID: "new-with-8", BotID: "bot", WorkdirID: "bound"}, Candidates: []thread.Thread{{ID: "old-75"}}}}
	h := &LocalChannelHandler{privateChats: s}
	for _, id := range []string{"", "new-with-8"} {
		got, err := h.resolveWebMessageContact(t.Context(), "bot", "user", id, "")
		if err != nil || got.ID != "new-with-8" {
			t.Fatal(got, err)
		}
	}
	if _, err := h.resolveWebMessageContact(t.Context(), "bot", "user", "old-75", ""); apperror.CodeOf(err) != apperror.CodeHTTPForbidden {
		t.Fatal("old history writable", err)
	}
	if _, err := h.resolveWebMessageContact(t.Context(), "bot", "user", "new-with-8", "different"); apperror.CodeOf(err) != apperror.CodeHTTPConflict {
		t.Fatal("workdir retargeted", err)
	}
	if s.creates != 0 || s.binds != 0 {
		t.Fatal("rebound existing contact")
	}
}
func TestFixedContactConcurrentOpensDoNotWaitForBackgroundTasks(t *testing.T) {
	s := &fixedContactStub{entry: route.PrivateChatEntry{Session: &thread.Thread{ID: "fixed", BotID: "bot"}}}
	// This is an identity/admission boundary test, not a model or delivery test.
	gate := make(chan struct{})
	done := make(chan struct{}, 2)
	for range 2 {
		go func() { <-gate; done <- struct{}{} }()
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := resolveContinuousContact(t.Context(), s, "bot", "user")
			if err != nil || got.ID != "fixed" {
				t.Error(got, err)
			}
		}()
	}
	wg.Wait()
	if s.creates != 0 || s.binds != 0 {
		t.Fatal("opened another contact")
	}
	close(gate)
	<-done
	<-done
}
func TestFixedContactQuickActionsDoNotAdvertiseOrRunNew(t *testing.T) {
	h := &LocalChannelHandler{}
	result, err := h.executeWebQuickAction(t.Context(), "bot", "help", false, webQuickActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Items {
		if resetsConversation(item.ID) {
			t.Fatal("advertised reset", item.ID)
		}
	}
	for _, name := range []string{"new", "/new discuss codex", "reset", "clear"} {
		if !resetsConversation(name) {
			t.Fatal(name)
		}
		_, err := h.executeWebQuickAction(t.Context(), "bot", name, false, webQuickActionContext{})
		if err == nil {
			t.Fatal("executed", name)
		}
	}
}
