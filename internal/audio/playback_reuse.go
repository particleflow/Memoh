package audio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

const (
	// playbackReuseTTL bounds how long generated audio stays replayable.
	// Synthesis is content-addressed, so an expired row only means one
	// re-synthesis: the window trades a small, predictable amount of provider
	// spend against the disk the bytes occupy. Thirty days covers the interval
	// in which a user revisits a reply in the same conversation.
	playbackReuseTTL = 30 * 24 * time.Hour

	// playbackReuseDir is the data-root subdirectory that holds reusable audio.
	playbackReuseDir = "speech-playback"

	// playbackReuseCandidates bounds how many rows one opening frame may
	// resolve to. Two distinct replies sharing an identical opening sentence
	// are already unusual; a backlog of them is not worth a wider scan.
	playbackReuseCandidates = 8
)

// PlaybackReuseKey is the content-addressed reuse identity for one utterance.
// The prefix identifies what the client posted first; ContentHash identifies
// the whole utterance that produced the audio.
type PlaybackReuseKey struct {
	BotID       string
	ModelID     string
	VoiceID     string
	PrefixHash  string
	ContentHash string
	ContentLen  int
}

// PlaybackReusePrefixKey derives the lookup identity from the opening frame.
// It is all the server knows before any audio is expected, so it is the only
// thing a cache lookup can be keyed on at POST time. The digest covers Bot,
// model row, resolved voice identity and the normalized opening text, so a
// change in any of them is a different lookup rather than a collision.
func PlaybackReusePrefixKey(botID, modelID, voiceIdentity, text string) PlaybackReuseKey {
	normalized := NormalizePlaybackText(text)
	return PlaybackReuseKey{
		BotID:      botID,
		ModelID:    modelID,
		VoiceID:    voiceIdentity,
		PrefixHash: hashPlaybackText(botID + "\x00" + modelID + "\x00" + voiceIdentity + "\x00" + normalized),
		ContentLen: len([]rune(normalized)),
	}
}

// WithUtterance returns the same identity retargeted at the complete spoken
// text, which is what a hit has to match before cached audio is served.
func (k PlaybackReuseKey) WithUtterance(text string) PlaybackReuseKey {
	normalized := NormalizePlaybackText(text)
	k.ContentHash = hashPlaybackText(normalized)
	k.ContentLen = len([]rune(normalized))
	return k
}

// Match returns the candidate whose bytes were produced by exactly this text.
func (k PlaybackReuseKey) Match(text string, candidates []CachedPlayback) (CachedPlayback, bool) {
	want := hashPlaybackText(NormalizePlaybackText(text))
	for _, candidate := range candidates {
		if candidate.ContentHash == want && len(candidate.Audio) > 0 {
			return candidate, true
		}
	}
	return CachedPlayback{}, false
}

// NormalizePlaybackText collapses whitespace runs and strips zero-width
// characters. It matches the web client's spoken-text projection: the client
// rejoins blocks and nodes with single spaces, so hashing raw markdown would
// miss replies whose rendered projection differs only in whitespace.
func NormalizePlaybackText(text string) string {
	text = strings.ReplaceAll(text, "\u200b", "")
	return strings.Join(strings.Fields(text), " ")
}

func hashPlaybackText(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// CachedPlayback is a reusable entry whose bytes have been read and verified.
type CachedPlayback struct {
	Audio       []byte
	ContentType string
	ContentHash string
	AudioSHA256 string
}

// PlaybackReuseStore persists synthesized playback audio so repeating
// unchanged text does not open another provider synthesis. It is the durable,
// cross-process authority: the web client's in-page cache never survives a
// reload, and the provider's duplex task cannot be dereferenced after its
// socket closes, which is why the encoded bytes are stored.
type PlaybackReuseStore struct {
	queries dbstore.Queries
	root    string
}

// NewPlaybackReuseStore returns a store rooted under the durable data root. A
// missing root or query set disables reuse: callers then always synthesize.
func NewPlaybackReuseStore(queries dbstore.Queries, dataRoot string) *PlaybackReuseStore {
	root := strings.TrimSpace(dataRoot)
	if root == "" || queries == nil {
		return nil
	}
	return &PlaybackReuseStore{queries: queries, root: filepath.Join(root, playbackReuseDir)}
}

// DataRoot reports the directory holding the reusable audio. It exists so a
// deployment or test can point another store at the same files.
func (s *PlaybackReuseStore) DataRoot() string {
	if s == nil {
		return ""
	}
	return s.root
}

// Candidates lists reusable entries whose opening frame matches the key. The
// caller compares ContentHash against the utterance it actually needs, so
// returning several is correct; returning none is a miss.
func (s *PlaybackReuseStore) Candidates(ctx context.Context, key PlaybackReuseKey) []CachedPlayback {
	if s == nil || key.PrefixHash == "" {
		return nil
	}
	botID, modelID, err := key.uuids()
	if err != nil {
		return nil
	}
	rows, err := s.queries.ListSpeechPlaybackCacheByPrefix(ctx, dbsqlc.ListSpeechPlaybackCacheByPrefixParams{
		BotID:         botID,
		ModelID:       modelID,
		VoiceIdentity: key.VoiceID,
		PrefixHash:    key.PrefixHash,
		RowLimit:      playbackReuseCandidates,
	})
	if err != nil {
		// A query failure is a miss: reuse must never fail a playback request.
		return nil
	}
	cached := make([]CachedPlayback, 0, len(rows))
	for _, row := range rows {
		audio, err := os.ReadFile(filepath.Join(s.root, filepath.Base(row.StorageKey))) //nolint:gosec // Name is a content digest, never caller input.
		if err != nil || int64(len(audio)) != row.AudioSize {
			s.drop(ctx, key.BotID, row.CacheKey)
			continue
		}
		if hashBytes(audio) != row.AudioSha256 {
			s.drop(ctx, key.BotID, row.CacheKey)
			continue
		}
		cached = append(cached, CachedPlayback{Audio: audio, ContentType: row.ContentType, ContentHash: row.ContentSha256, AudioSHA256: row.AudioSha256})
	}
	return cached
}

// Store records synthesized audio under the whole-utterance key. The file is
// written before the row, so a crash between the two leaves an unreferenced
// file, never a row whose audio is missing.
func (s *PlaybackReuseStore) Store(ctx context.Context, key PlaybackReuseKey, audio []byte, contentType, providerTaskID string) error {
	if s == nil || len(audio) == 0 || contentType == "" || key.ContentHash == "" {
		return nil
	}
	botID, modelID, err := key.uuids()
	if err != nil {
		return err
	}
	if err := s.write(key, audio); err != nil {
		return err
	}
	_, err = s.queries.UpsertSpeechPlaybackCache(ctx, dbsqlc.UpsertSpeechPlaybackCacheParams{
		BotID:          botID,
		PrefixHash:     key.PrefixHash,
		CacheKey:       playbackCacheKey(key),
		ModelID:        modelID,
		VoiceIdentity:  key.VoiceID,
		ContentSha256:  key.ContentHash,
		ContentChars:   int32(key.ContentLen), //nolint:gosec // Rune count of the bounded spoken utterance.
		AudioSha256:    hashBytes(audio),
		AudioSize:      int64(len(audio)),
		ContentType:    contentType,
		StorageKey:     fileName(key),
		ProviderTaskID: providerTaskID,
		ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(playbackReuseTTL), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("record playback reuse entry: %w", err)
	}
	return nil
}

// Reap deletes expired rows and their audio. It is best effort: a deployment
// that never calls it still refuses expired hits, because reads filter on
// expires_at.
func (s *PlaybackReuseStore) Reap(ctx context.Context) error {
	if s == nil {
		return nil
	}
	names, err := s.queries.DeleteExpiredSpeechPlaybackCache(ctx)
	if err != nil {
		return fmt.Errorf("delete expired playback reuse entries: %w", err)
	}
	for _, name := range names {
		_ = os.Remove(filepath.Join(s.root, filepath.Base(name)))
	}
	return nil
}

// drop removes one unusable entry. Failures stay silent: the entry is already
// treated as a miss, and the next successful synthesis replaces it.
func (s *PlaybackReuseStore) drop(ctx context.Context, botID, cacheKey string) {
	id, err := parseReuseUUID(botID)
	if err != nil {
		return
	}
	_ = s.queries.DeleteSpeechPlaybackCache(ctx, dbsqlc.DeleteSpeechPlaybackCacheParams{BotID: id, CacheKey: cacheKey})
}

// write stores the bytes atomically: a complete temp file is renamed into
// place, so a reader never observes a partially written utterance.
func (s *PlaybackReuseStore) write(key PlaybackReuseKey, audio []byte) error {
	if err := os.MkdirAll(s.root, 0o750); err != nil {
		return fmt.Errorf("create playback reuse dir: %w", err)
	}
	final := filepath.Join(s.root, fileName(key))
	if info, err := os.Stat(final); err == nil && info.Size() == int64(len(audio)) {
		return nil
	}
	temp, err := os.CreateTemp(s.root, "*.partial")
	if err != nil {
		return fmt.Errorf("create playback reuse file: %w", err)
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := temp.Write(audio); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write playback reuse file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close playback reuse file: %w", err)
	}
	if err := os.Rename(name, final); err != nil {
		return fmt.Errorf("publish playback reuse file: %w", err)
	}
	return nil
}

func (k PlaybackReuseKey) uuids() (pgtype.UUID, pgtype.UUID, error) {
	botID, err := parseReuseUUID(k.BotID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	modelID, err := parseReuseUUID(k.ModelID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	return botID, modelID, nil
}

// playbackCacheKey is the row key: the whole utterance plus the settings that
// produced it, so a parameter change writes a new row instead of overwriting
// audio that was synthesized under different settings.
func playbackCacheKey(key PlaybackReuseKey) string {
	return hashPlaybackText(key.BotID + "\x00" + key.ModelID + "\x00" + key.VoiceID + "\x00" + key.ContentHash)
}

// fileName is the on-disk name. It carries no content, so the directory is
// meaningless without the database row that points at it.
func fileName(key PlaybackReuseKey) string { return playbackCacheKey(key) + ".audio" }

func hashBytes(audio []byte) string {
	sum := sha256.Sum256(audio)
	return hex.EncodeToString(sum[:])
}

func parseReuseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse playback reuse id: %w", err)
	}
	if !id.Valid {
		return pgtype.UUID{}, errors.New("playback reuse id is empty")
	}
	return id, nil
}
