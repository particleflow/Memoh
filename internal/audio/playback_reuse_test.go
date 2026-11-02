package audio

import (
	"context"
	"testing"
)

// Fixtures use escapes so the expectation stays byte-identical regardless of
// the editor or shell encoding.
const (
	firstSentence  = "\u7b2c\u4e00\u53e5\u8bdd\u3002"
	secondSentence = "\u7b2c\u4e8c\u53e5\u8bdd\u3002"
	fullReply      = firstSentence + secondSentence
)

func TestPlaybackReuseTextNormalizationMatchesClientProjection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, a, b string }{
		{name: "zero width", a: "\u7b2c\u4e00\u53e5\u8bdd\u200b\u3002", b: firstSentence},
		{name: "newline equals space", a: firstSentence + "\n\n" + secondSentence, b: firstSentence + " " + secondSentence},
		{name: "surrounding space", a: "  " + firstSentence + "  ", b: firstSentence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if PlaybackReusePrefixKey("bot", "model", "voice", tc.a).ContentHash != "" {
				t.Fatal("a prefix key must not carry a whole-utterance hash")
			}
			if NormalizePlaybackText(tc.a) != NormalizePlaybackText(tc.b) {
				t.Fatalf("equivalent projections normalized differently: %q != %q", tc.a, tc.b)
			}
		})
	}
	// Different words must not collide: reuse may only replay identical content.
	if NormalizePlaybackText(firstSentence) == NormalizePlaybackText("\u7b2c\u4e00\u53e5\u8bdd\uff01") {
		t.Fatal("different punctuation normalized identically")
	}
}

func TestPlaybackReuseKeyCoversVoiceAndText(t *testing.T) {
	t.Parallel()
	base := PlaybackReusePrefixKey("bot", "model", "voice-one", firstSentence)
	if base.PrefixHash == "" || base.ContentLen == 0 {
		t.Fatalf("incomplete key %+v", base)
	}
	// Bot, model, voice and text are all part of the identity: changing any one
	// must miss rather than replay audio made under different settings.
	for _, changed := range []PlaybackReuseKey{
		PlaybackReusePrefixKey("other-bot", "model", "voice-one", firstSentence),
		PlaybackReusePrefixKey("bot", "other-model", "voice-one", firstSentence),
		PlaybackReusePrefixKey("bot", "model", "voice-two", firstSentence),
		PlaybackReusePrefixKey("bot", "model", "voice-one", secondSentence),
	} {
		if changed.PrefixHash == base.PrefixHash {
			t.Fatalf("key collision for %+v", changed)
		}
	}
}

func TestPlaybackReuseMatchRequiresTheWholeUtterance(t *testing.T) {
	t.Parallel()
	key := PlaybackReusePrefixKey("bot", "model", "voice", firstSentence)
	cached := []CachedPlayback{
		{ContentHash: key.WithUtterance(fullReply).ContentHash, ContentType: "audio/mpeg", Audio: []byte("long")},
		{ContentHash: key.WithUtterance(firstSentence).ContentHash, ContentType: "audio/mpeg", Audio: []byte("short")},
	}
	match, ok := key.Match(firstSentence, cached)
	if !ok || string(match.Audio) != "short" {
		t.Fatalf("exact utterance did not resolve: %v %q", ok, match.Audio)
	}
	// A reply that continues past the opening frame is not the cached one.
	if _, ok = key.Match(firstSentence + "\u522b\u7684\u5185\u5bb9\u3002", cached); ok {
		t.Fatal("a different utterance matched a shared opening frame")
	}
}

func TestPlaybackReuseStoreIsDisabledWithoutQueries(t *testing.T) {
	t.Parallel()
	store := NewPlaybackReuseStore(nil, t.TempDir())
	if store != nil {
		t.Fatal("a store without queries must be disabled")
	}
	// A disabled store is inert rather than a failure path.
	key := PlaybackReusePrefixKey("bot", "model", "voice", firstSentence)
	if candidates := store.Candidates(context.Background(), key); len(candidates) != 0 {
		t.Fatal("disabled store returned candidates")
	}
	if err := store.Store(context.Background(), key, []byte("a"), "audio/mpeg", ""); err != nil {
		t.Fatalf("disabled store failed a write: %v", err)
	}
	if err := store.Reap(context.Background()); err != nil {
		t.Fatalf("disabled store failed a reap: %v", err)
	}
}
