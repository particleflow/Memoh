package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/accounts"
	audiopkg "github.com/felinics/memoh/internal/audio"
	"github.com/felinics/memoh/internal/audio/input"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/settings"
)

// A reply whose first sentence ends before the rest arrives. Escapes keep the
// fixture byte-identical regardless of editor or shell encoding.
const (
	firstSentence  = "\u7b2c\u4e00\u53e5\u8bdd\u3002"
	secondSentence = "\u7b2c\u4e8c\u53e5\u8bdd\u3002"
	otherEnding    = "\u5b8c\u5168\u4e0d\u540c\u7684\u7ed3\u5c3e\u3002"
)

// reuseMemoryStore stands in for the durable store: entries survive handler and
// server instances, which is how a restart is expressed in these tests.
type reuseMemoryStore struct {
	mu      sync.Mutex
	entries map[string]audiopkg.CachedPlayback
	writes  int
}

func newReuseMemoryStore() *reuseMemoryStore {
	return &reuseMemoryStore{entries: make(map[string]audiopkg.CachedPlayback)}
}

func (s *reuseMemoryStore) Candidates(_ context.Context, key audiopkg.PlaybackReuseKey) []audiopkg.CachedPlayback {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audiopkg.CachedPlayback, 0, len(s.entries))
	for prefix, entry := range s.entries {
		if prefix == key.PrefixHash {
			out = append(out, entry)
		}
	}
	return out
}

func (s *reuseMemoryStore) Store(_ context.Context, key audiopkg.PlaybackReuseKey, audio []byte, contentType, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	s.entries[key.PrefixHash] = audiopkg.CachedPlayback{Audio: audio, ContentType: contentType, ContentHash: key.ContentHash}
	return nil
}

// expire drops every entry, which is what the retention window does to a row
// whose expires_at has passed.
func (s *reuseMemoryStore) expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]audiopkg.CachedPlayback)
}

// reuseInputAudio is a provider fixture: every open counts as a synthesis and
// yields deterministic bytes derived from the spoken text.
type reuseInputAudio struct {
	*playbackAudio
	identity string
	voice    string
	opens    int
	spoken   []string
}

func (a *reuseInputAudio) PlaybackIdentity(context.Context, string) (string, error) {
	return a.identity, nil
}

func (a *reuseInputAudio) PlaybackInputInfo(context.Context, string) (string, string, error) {
	return a.identity, "audio/mpeg", nil
}

func (a *reuseInputAudio) PlaybackReuseInfo(_ context.Context, botID, modelID, text string) (audiopkg.PlaybackReuseKey, string, string, error) {
	return audiopkg.PlaybackReusePrefixKey(botID, modelID, a.voice, text), a.identity, "audio/mpeg", nil
}

func (a *reuseInputAudio) OpenPlaybackInput(_ context.Context, _ string) (input.Session, string, error) {
	a.opens++
	a.spoken = append(a.spoken, "")
	return &reuseInputSession{audio: a, chunks: make(chan []byte, 1), errs: make(chan error, 1)}, a.identity, nil
}

type reuseInputSession struct {
	mu     sync.Mutex
	audio  *reuseInputAudio
	text   []byte
	chunks chan []byte
	errs   chan error
	once   sync.Once
}

func (s *reuseInputSession) Append(_ context.Context, frame input.Frame) error {
	s.mu.Lock()
	s.text = append(s.text, frame.Text...)
	text, final := string(s.text), frame.Final
	s.mu.Unlock()
	if final {
		// Deterministic per utterance: identical text yields identical bytes,
		// which is what makes "same audio" a meaningful assertion. A provider
		// stream stays open until input closes, exactly like the real adapter.
		s.chunks <- []byte("synthesized:" + text)
		s.once.Do(func() {
			s.audio.spoken[len(s.audio.spoken)-1] = text
			close(s.chunks)
			close(s.errs) // A nil-error close is a clean end, matching the SDK contract.
		})
	}
	return nil
}

func (s *reuseInputSession) Audio() *sdk.SpeechStreamResult {
	return sdk.NewSpeechStreamResult(s.chunks, "audio/mpeg", s.errs)
}

func (s *reuseInputSession) Cancel() {
	s.once.Do(func() {
		close(s.chunks)
		close(s.errs)
	})
}

type reuseSettings struct{ model string }

func (s *reuseSettings) GetBot(context.Context, string) (settings.Settings, error) {
	return settings.Settings{TtsModelID: s.model}, nil
}

// reuseServer builds a handler and HTTP server. A second call with the same
// store represents a restarted process: no in-memory state carries over.
func reuseServer(t *testing.T, audio *reuseInputAudio, store *reuseMemoryStore) (*httptest.Server, string) {
	t.Helper()
	q := newInvocationTestQueries()
	q.bot = testBotRow(invocationTestBotID, nil)
	q.bot.OwnerUserID = testUUID(invocationTestOwnerID)
	account := accounts.NewService(nil, &playbackBoundaryAccounts{active: true})
	h := &BotAudioHandler{
		audioService:    audio,
		settingsService: &reuseSettings{model: "configured-model"},
	}
	// A nil store must stay an untyped nil, so the handler sees reuse disabled.
	if store != nil {
		h.reuse = store
	}
	h.SetPlaybackAccess(bots.NewService(nil, q), account)
	const secret = "synthetic-playback-reuse-secret"
	token, _, err := auth.GenerateInternalToken(invocationTestOwnerID, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.NewServerWithSessionValidator(slog.New(slog.DiscardHandler), "", secret, func(ctx context.Context, user, _ string) error { return account.ValidateSession(ctx, user) }, h))
	t.Cleanup(srv.Close)
	return srv, token
}

// readInput posts the opening frame and then PUTs the remainder exactly the way
// the web client does, returning the body the server streamed.
func readInput(t *testing.T, srv *httptest.Server, token, id, opening, rest string) string {
	t.Helper()
	response := nativeRequest(t, srv, token, http.MethodPost, id, opening, 0, 0, false)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("start %d: %s", response.StatusCode, body)
	}
	if rest != "" {
		appendResponse := nativeRequest(t, srv, token, http.MethodPut, id, rest, 1, uint64(len([]rune(opening))), false)
		_ = appendResponse.Body.Close()
		if appendResponse.StatusCode != http.StatusNoContent {
			t.Fatalf("append %d", appendResponse.StatusCode)
		}
	}
	final := nativeRequest(t, srv, token, http.MethodPut, id, "", 2, uint64(len([]rune(opening+rest))), true)
	_ = final.Body.Close()
	if final.StatusCode != http.StatusNoContent {
		t.Fatalf("finish %d", final.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestPlaybackReuseServesSameUtteranceWithoutAnotherSynthesis(t *testing.T) {
	store := newReuseMemoryStore()
	audio := &reuseInputAudio{playbackAudio: &playbackAudio{}, identity: "voice-config-one", voice: "voice-one"}
	srv, token := reuseServer(t, audio, store)

	first := readInput(t, srv, token, "11111111-1111-4111-8111-111111111111", firstSentence, secondSentence)
	if audio.opens != 1 || first == "" {
		t.Fatalf("first read must synthesize once (opens=%d body=%q)", audio.opens, first)
	}
	second := readInput(t, srv, token, "22222222-2222-4222-8222-222222222222", firstSentence, secondSentence)
	if audio.opens != 1 {
		t.Fatalf("repeating the same text opened another synthesis: opens=%d", audio.opens)
	}
	if first != second {
		t.Fatalf("reuse served different audio: %q != %q", first, second)
	}
	if store.writes != 1 {
		t.Fatalf("the utterance was registered %d times", store.writes)
	}
}

func TestPlaybackReuseMissesWhenVoiceChanges(t *testing.T) {
	store := newReuseMemoryStore()
	audio := &reuseInputAudio{playbackAudio: &playbackAudio{}, identity: "voice-config-one", voice: "voice-one"}
	srv, token := reuseServer(t, audio, store)

	readInput(t, srv, token, "33333333-3333-4333-8333-333333333333", firstSentence, secondSentence)
	// Same text, different resolved voice: replaying the old bytes would speak
	// the reply in the previous voice, so this must synthesize again.
	audio.voice = "voice-two"
	audio.identity = "voice-config-two"
	readInput(t, srv, token, "44444444-4444-4444-8444-444444444444", firstSentence, secondSentence)
	if audio.opens != 2 {
		t.Fatalf("a voice change reused the old audio: opens=%d", audio.opens)
	}
}

func TestPlaybackReuseMissesWhenTheUtteranceDiffers(t *testing.T) {
	store := newReuseMemoryStore()
	audio := &reuseInputAudio{playbackAudio: &playbackAudio{}, identity: "voice-config-one", voice: "voice-one"}
	srv, token := reuseServer(t, audio, store)

	readInput(t, srv, token, "55555555-5555-4555-8555-555555555555", firstSentence, secondSentence)
	// The same opening frame with different remaining text is a different
	// utterance and must not be served the previous reply's audio.
	body := readInput(t, srv, token, "66666666-6666-4666-8666-666666666666", firstSentence, otherEnding)
	if audio.opens != 2 {
		t.Fatalf("a shared opening frame reused the wrong utterance: opens=%d", audio.opens)
	}
	if body == "" {
		t.Fatal("the second utterance produced no audio")
	}
}

func TestPlaybackReuseRegeneratesAfterRetentionExpiry(t *testing.T) {
	store := newReuseMemoryStore()
	audio := &reuseInputAudio{playbackAudio: &playbackAudio{}, identity: "voice-config-one", voice: "voice-one"}
	srv, token := reuseServer(t, audio, store)

	readInput(t, srv, token, "77777777-7777-4777-8777-777777777777", firstSentence, secondSentence)
	store.expire()
	readInput(t, srv, token, "88888888-8888-4888-8888-888888888888", firstSentence, secondSentence)
	if audio.opens != 2 {
		t.Fatalf("an expired entry was still served: opens=%d", audio.opens)
	}
}

func TestPlaybackReuseSurvivesANewServerProcess(t *testing.T) {
	store := newReuseMemoryStore()
	first := &reuseInputAudio{playbackAudio: &playbackAudio{}, identity: "voice-config-one", voice: "voice-one"}
	srv, token := reuseServer(t, first, store)
	readInput(t, srv, token, "99999999-9999-4999-8999-999999999999", firstSentence, secondSentence)
	if first.opens != 1 {
		t.Fatalf("first process did not synthesize: opens=%d", first.opens)
	}

	// A new server process has no in-memory state at all. Only the durable
	// store carries the entry, which is the cross-restart guarantee.
	second := &reuseInputAudio{playbackAudio: &playbackAudio{}, identity: "voice-config-one", voice: "voice-one"}
	restarted, restartedToken := reuseServer(t, second, store)
	readInput(t, restarted, restartedToken, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", firstSentence, secondSentence)
	if second.opens != 0 {
		t.Fatalf("a restarted process re-synthesized unchanged content: opens=%d", second.opens)
	}
}

func TestPlaybackReuseStaysOffWithoutAStore(t *testing.T) {
	audio := &reuseInputAudio{playbackAudio: &playbackAudio{}, identity: "voice-config-one", voice: "voice-one"}
	srv, token := reuseServer(t, audio, nil)
	readInput(t, srv, token, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", firstSentence, secondSentence)
	readInput(t, srv, token, "cccccccc-cccc-4ccc-8ccc-cccccccccccc", firstSentence, secondSentence)
	if audio.opens != 2 {
		t.Fatalf("reuse without a store changed the synthesis count: opens=%d", audio.opens)
	}
}
