package albumwriteback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
)

// The write-back queue names media by the existing content hash. Nothing here
// derives a second fingerprint: an item is enqueued only after the owner Bot's
// original resolves, and the row carries the size, MIME type and name the
// phone needs to create the asset.
//
// State is a two-value lifecycle. A failed write-back is never reported here:
// the item stays pending, and the phone keeps the per-item failure in its own
// journal, so a retry is the next pull of the same work list.

const (
	StatePending = "pending"
	StateWritten = "written"

	// MaxPairKeyBytes bounds the pairing label a caller may name.
	MaxPairKeyBytes = 64
	// MaxNameBytes bounds the display name a caller may attach to one item.
	MaxNameBytes = 255
	// MaxItemsPerCall bounds one enqueue call so a single tool call cannot
	// enqueue an unbounded list.
	MaxItemsPerCall = 200
)

var (
	// ErrInvalid reports a malformed pairing label, hash or name.
	ErrInvalid = errors.New("invalid album write-back request")
	// ErrAssetUnavailable reports a hash the calling Bot cannot resolve.
	ErrAssetUnavailable = errors.New("media asset not available to this bot")
	// ErrNotFound reports a written report for an item the owner has no row for.
	ErrNotFound = errors.New("album write-back entry not found")
)

// AssetResolver resolves a hash against the existing content-addressed store.
// Production uses the media service, so a queued identity is exactly the one
// the upload path records.
type AssetResolver interface {
	Stat(ctx context.Context, botID, contentHash string) (media.Asset, error)
}

// Service owns enqueue and the phone-facing pull/mark pair.
type Service struct {
	store  dbstore.PhoneAlbumWritebackStore
	assets AssetResolver
	logger *slog.Logger
}

func NewService(log *slog.Logger, store dbstore.PhoneAlbumWritebackStore, assets AssetResolver) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, assets: assets, logger: log.With(slog.String("service", "album_writeback"))}
}

// Item is one queued write-back as the phone reads it.
type Item struct {
	PairKey     string `json:"pair_key"`
	BotID       string `json:"bot_id"`
	ContentHash string `json:"content_hash"`
	SizeBytes   *int64 `json:"size_bytes,omitempty"`
	Mime        string `json:"mime,omitempty"`
	Name        string `json:"name,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// PendingResponse is the work list the phone drains when it comes forward.
type PendingResponse struct {
	Items []Item `json:"items"`
}

// EnqueueInput is one caller's write-back request.
type EnqueueInput struct {
	PairKey       string
	ContentHashes []string
	// Name is an optional display name applied to every hash in this call.
	Name string
}

// EnqueueResult reports what the queue now holds for this pairing.
type EnqueueResult struct {
	PairKey  string   `json:"pair_key"`
	Enqueued []string `json:"enqueued"`
}

// Enqueue validates every hash against this Bot's originals and upserts the
// rows. A hash already written for this pairing returns to pending, which is
// how a re-processed product gets written again.
func (s *Service) Enqueue(ctx context.Context, userID, botID string, in EnqueueInput) (EnqueueResult, error) {
	if s == nil || s.store == nil || s.assets == nil {
		return EnqueueResult{}, errors.New("album write-back service not configured")
	}
	pairKey := strings.TrimSpace(in.PairKey)
	if !ValidPairKey(pairKey) || len(in.ContentHashes) == 0 || len(in.ContentHashes) > MaxItemsPerCall {
		return EnqueueResult{}, ErrInvalid
	}
	name := strings.TrimSpace(in.Name)
	if len(name) > MaxNameBytes || !utf8.ValidString(name) {
		return EnqueueResult{}, ErrInvalid
	}
	seen := make(map[string]bool, len(in.ContentHashes))
	hashes := make([]string, 0, len(in.ContentHashes))
	for _, raw := range in.ContentHashes {
		hash := strings.TrimSpace(raw)
		if !validContentHash(hash) {
			return EnqueueResult{}, ErrInvalid
		}
		if seen[hash] {
			continue
		}
		seen[hash] = true
		hashes = append(hashes, hash)
	}
	// Resolve every asset before the first write: one unresolvable hash rejects
	// the call instead of leaving a half-enqueued list.
	assets := make([]media.Asset, 0, len(hashes))
	for _, hash := range hashes {
		asset, err := s.assets.Stat(ctx, botID, hash)
		if err != nil || asset.ContentHash != hash || asset.BotID != botID {
			return EnqueueResult{}, ErrAssetUnavailable
		}
		assets = append(assets, asset)
	}
	enqueued := make([]string, 0, len(assets))
	for _, asset := range assets {
		size := asset.SizeBytes
		record := dbstore.PhoneAlbumWritebackRecord{
			UserID:      userID,
			PairKey:     pairKey,
			BotID:       botID,
			ContentHash: asset.ContentHash,
			Mime:        asset.Mime,
			Name:        itemName(name, asset.StorageKey),
			SizeBytes:   &size,
		}
		if _, err := s.store.EnqueuePhoneAlbumWriteback(ctx, record); err != nil {
			return EnqueueResult{}, err
		}
		enqueued = append(enqueued, asset.ContentHash)
	}
	return EnqueueResult{PairKey: pairKey, Enqueued: enqueued}, nil
}

// VerifiedItem is one already-verified delivery reference: its hash went through
// the endpoint's identity check and a full sha256 scan, so the queue writes it
// without a second Stat against the Bot's originals.
type VerifiedItem struct {
	ContentHash string
	SizeBytes   int64
	Mime        string
}

// EnqueueVerified queues one product the endpoint already verified against its
// delivered bytes. It skips the Stat call Enqueue makes to reject untrusted
// caller-supplied hashes, and it carries no filename: the queue knows only the
// hash, the size and the MIME type, and the phone derives a display name from
// the MIME type. A hash already written for this pairing returns to pending, so
// re-processing the same product queues it again.
func (s *Service) EnqueueVerified(ctx context.Context, userID, botID, pairKey string, item VerifiedItem) (string, error) {
	if s == nil || s.store == nil {
		return "", errors.New("album write-back service not configured")
	}
	pairKey = strings.TrimSpace(pairKey)
	contentHash := strings.TrimSpace(item.ContentHash)
	if !ValidPairKey(pairKey) || !validContentHash(contentHash) {
		return "", ErrInvalid
	}
	if _, err := db.ParseUUID(userID); err != nil {
		return "", ErrInvalid
	}
	if _, err := db.ParseUUID(botID); err != nil {
		return "", ErrInvalid
	}
	size := item.SizeBytes
	record := dbstore.PhoneAlbumWritebackRecord{
		UserID:      userID,
		PairKey:     pairKey,
		BotID:       botID,
		ContentHash: contentHash,
		Mime:        item.Mime,
		SizeBytes:   &size,
	}
	if _, err := s.store.EnqueuePhoneAlbumWriteback(ctx, record); err != nil {
		return "", err
	}
	return contentHash, nil
}

// Pending returns the owner's pending set in enqueue order. Every pending row is
// its own terminal unit, so a phone that stops mid-list resumes from the same
// list next time.
func (s *Service) Pending(ctx context.Context, userID string) (PendingResponse, error) {
	if s == nil || s.store == nil {
		return PendingResponse{}, errors.New("album write-back service not configured")
	}
	if _, err := db.ParseUUID(userID); err != nil {
		return PendingResponse{}, ErrInvalid
	}
	records, err := s.store.ListPendingPhoneAlbumWritebacks(ctx, userID)
	if err != nil {
		return PendingResponse{}, err
	}
	items := make([]Item, 0, len(records))
	for _, record := range records {
		items = append(items, itemFromRecord(record))
	}
	return PendingResponse{Items: items}, nil
}

// MarkWritten records one item as written. It is idempotent and scoped to the
// caller: a pairing/hash the owner has no row for is not found.
func (s *Service) MarkWritten(ctx context.Context, userID, pairKey, contentHash string) (Item, error) {
	if s == nil || s.store == nil {
		return Item{}, errors.New("album write-back service not configured")
	}
	pairKey = strings.TrimSpace(pairKey)
	contentHash = strings.TrimSpace(contentHash)
	if !ValidPairKey(pairKey) || !validContentHash(contentHash) {
		return Item{}, ErrInvalid
	}
	if _, err := db.ParseUUID(userID); err != nil {
		return Item{}, ErrInvalid
	}
	record, err := s.store.MarkPhoneAlbumWritebackWritten(ctx, userID, pairKey, contentHash)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return Item{}, ErrNotFound
		}
		return Item{}, err
	}
	return itemFromRecord(record), nil
}

func itemFromRecord(record dbstore.PhoneAlbumWritebackRecord) Item {
	return Item{
		PairKey:     record.PairKey,
		BotID:       record.BotID,
		ContentHash: record.ContentHash,
		SizeBytes:   record.SizeBytes,
		Mime:        record.Mime,
		Name:        record.Name,
		CreatedAt:   record.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// itemName prefers the caller's name; otherwise the original's storage basename
// gives the phone a usable file name derived from the content hash.
func itemName(name, storageKey string) string {
	if name != "" {
		return name
	}
	if storageKey == "" {
		return ""
	}
	return path.Base(storageKey)
}

// ValidPairKey accepts the pairing labels the phone and the bot agree on. It is
// exported so the pairing archive validates a label with the same rule the queue
// applies: one label vocabulary, not two.
func ValidPairKey(value string) bool {
	if value == "" || len(value) > MaxPairKeyBytes {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		allowed := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
		if !allowed {
			return false
		}
	}
	return true
}

// validContentHash accepts exactly a lowercase SHA-256 hex digest, so the queue
// can never hold an identity the upload path would not produce.
func validContentHash(value string) bool {
	if value != strings.ToLower(value) || len(value) != hex.EncodedLen(sha256.Size) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
