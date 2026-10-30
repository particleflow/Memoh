package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agent/runtime/native"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/runtimefence"
)

type presenceLocalOnlyTransport struct {
	host    string
	base    http.RoundTripper
	blocked atomic.Int32
}

func (t *presenceLocalOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "http" || r.URL.Host != t.host || r.URL.User != nil {
		t.blocked.Add(1)
		return nil, errors.New("nonlocal model request blocked by fixture")
	}
	return t.base.RoundTrip(r)
}

func TestPresenceFixtureRejectsNonlocalRequestsBeforeNetwork(t *testing.T) {
	transport := &presenceLocalOnlyTransport{host: "127.0.0.1:12345"}
	for _, target := range []string{"https://127.0.0.1:12345/test", "http://outside.invalid/test", "http://user@127.0.0.1:12345/test"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if _, err := transport.RoundTrip(req); err == nil {
			t.Fatal("unapproved fixture request reached the network transport")
		}
	}
	if transport.blocked.Load() != 3 {
		t.Fatal("a rejected fixture request was not recorded")
	}
}

// Drives the actual StreamChatWS -> native Agent -> SDK -> local HTTP path,
// alongside durable admission/fence/claim/history. No real inference is sent.
func TestConversationPresenceNativeWSAndHistory(t *testing.T) {
	claims, _, pool := presencePostgresFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = runtimefence.WithContext(ctx, runtimefence.Fence{BotID: claims.handle.BotID, SessionID: claims.handle.SessionID, Token: claims.handle.FencingToken})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	bodies := make(chan map[string]any, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
			return
		}
		bodies <- body
		if body["stream"] == true {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			if _, err := fmt.Fprint(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Formal fixture answer.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"); err != nil {
				t.Error(err)
			}
		} else {
			w.Header().Set("Content-Type", "application/json")
			if _, err := fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","model":"direct-lifecycle-model","choices":[{"index":0,"message":{"role":"assistant","content":"{\"text\":\"收到，我来梳理。\",\"phase\":\"thinking\"}"},"finish_reason":"stop"}]}`); err != nil {
				t.Error(err)
			}
		}
	}))
	defer server.Close()
	fixture := newDirectLifecycleFixture(t, directLifecycleModelSuccess)
	service := fixture.service
	fake := service.queries.(*directLifecycleQueries)
	fake.provider.Config, _ = json.Marshal(map[string]any{"base_url": server.URL, "api_key": "fixture-only"})
	for id, model := range fake.models {
		model.Config = []byte(`{"context_window":128000,"thinking_mode":"toggle","reasoning_efforts":["low","high"],"reasoning_off_support":"rejected"}`)
		fake.models[id] = model
	}
	for _, statement := range []string{`INSERT INTO providers(id,name) VALUES($1,'presence-native-provider') ON CONFLICT DO NOTHING`, `INSERT INTO models(id,model_id,provider_id) VALUES($1,'direct-lifecycle-model',$2) ON CONFLICT DO NOTHING`} {
		var err error
		if strings.Contains(statement, "INSERT INTO providers") {
			_, err = pool.Exec(ctx, statement, directLifecycleProviderID)
		} else {
			_, err = pool.Exec(ctx, statement, directLifecycleModelID, directLifecycleProviderID)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	// The durable provider row, not the model fake, owns the resolved address.
	if _, err := pool.Exec(ctx, `UPDATE providers SET config=$2 WHERE id=$1`, directLifecycleProviderID, fake.provider.Config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE providers SET config='{}'::jsonb WHERE id=$1`, directLifecycleProviderID)
	})
	localURL, _ := url.Parse(server.URL)
	transport := &presenceLocalOnlyTransport{host: localURL.Host, base: server.Client().Transport}
	service.streamHTTPClient = &http.Client{Transport: transport}
	service.nonStreamingHTTPClient = &http.Client{Transport: transport}
	service.queries = claims.queries
	service.messageService = messagepkg.NewService(service.logger, claims.queries)
	service.botPermissions = presenceChatPermission{}
	service.SetConversationPresence(ConversationPresenceOptions{Policy: conversationpresence.Policy{Limits: conversationpresence.Limits{Deadline: 8 * time.Second, MaxInputRunes: 100, MaxOutputTokens: 100, MaxTextRunes: 100}}, MaxConcurrentPerProvider: 1})
	var position int64
	if err := pool.QueryRow(ctx, `SELECT turn_position FROM session_runs WHERE run_id=$1`, claims.handle.RunID).Scan(&position); err != nil {
		t.Fatal(err)
	}
	req := ChatRequest{BotID: claims.handle.BotID, ChatID: claims.handle.BotID, ThreadID: claims.handle.SessionID, RunID: claims.handle.RunID, TurnID: claims.handle.TurnID, TurnPosition: &position, RunHandle: claims.handle, UserID: claims.actor, Query: "Please explain", RawQuery: "Please explain", UserVisibleText: "Please explain", CurrentChannel: "local", ReasoningEffort: "high"}
	events := make(chan WSStreamEvent, 32)
	finished := make(chan error, 1)
	go func() {
		_, err := service.StreamChatWS(ctx, req, events, make(chan struct{}))
		finished <- err
		close(events)
	}()
	var sequence []string
	var allEvents []string
	var ackCount int
	for raw := range events {
		var e native.StreamEvent
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		allEvents = append(allEvents, string(e.Type)+":"+e.Code)
		if e.Type == native.EventProgress {
			var piece conversationpresence.Piece
			data, _ := json.Marshal(e.Progress)
			_ = json.Unmarshal(data, &piece)
			if piece.Kind == "conversation_ack" {
				ackCount++
				sequence = append(sequence, "ack")
				once.Do(func() { close(release) })
			}
		}
		if e.Type == native.EventTextDelta {
			sequence = append(sequence, "formal")
		}
	}
	if err := <-finished; err != nil {
		t.Fatalf("native WS: %v events=%v", err, sequence)
	}
	if ackCount != 1 || len(sequence) < 2 || sequence[0] != "ack" {
		t.Fatalf("ack/main sequence: %v all=%v requests=%d", sequence, allEvents, len(bodies))
	}
	if transport.blocked.Load() != 0 {
		t.Fatalf("nonlocal model request attempted: %d", transport.blocked.Load())
	}
	first, second := <-bodies, <-bodies
	if first["model"] != second["model"] || first["reasoning_effort"] != "high" || second["reasoning_effort"] != "low" {
		t.Fatalf("main/aux wire: %v %v", first, second)
	}
	var history string
	if err := pool.QueryRow(ctx, `SELECT coalesce(string_agg(content::text,' '),'') FROM bot_history_messages WHERE session_id=$1`, claims.handle.SessionID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(history, "收到") || !strings.Contains(history, "Formal fixture answer.") {
		t.Fatalf("formal history pollution/missing answer: %s", history)
	}
}
