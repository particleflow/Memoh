// Package albumpairs stores which album pairing label a user designated for a
// processing line, so finished products are queued without the model having to
// name the album again. It stores intent only: the album itself (uri, name) is
// resolved on the phone, and no display name is ever an identity here.
package albumpairs

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/felinics/memoh/internal/albumwriteback"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

const (
	// StateActive means new products for the pairing's Bot are queued
	// automatically. StateRevoked is an unbind: the row and its label stay.
	StateActive  = "active"
	StateRevoked = "revoked"
)

var (
	// ErrInvalid reports a malformed pairing label or bot identity.
	ErrInvalid = errors.New("invalid album pairing request")
	// ErrNotFound reports a pairing label the caller never registered.
	ErrNotFound = errors.New("album pairing not found")
	// ErrBotUnavailable reports a bot the current team has no such pairing
	// target for; the database rejects the reference.
	ErrBotUnavailable = errors.New("album pairing bot unavailable")
)

// Pair is one pairing row as the phone reads it. It carries no album uri, album
// name or device identity: those belong to the phone's local ledger.
type Pair struct {
	PairKey   string `json:"pair_key"`
	BotID     string `json:"bot_id"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Service owns the pairing archive. Every method is owner-scoped: a pairing
// belongs to the caller's identity, never to a model or another user.
type Service struct {
	store  dbstore.AlbumPairStore
	logger *slog.Logger
}

func NewService(log *slog.Logger, store dbstore.AlbumPairStore) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, logger: log.With(slog.String("service", "album_pairs"))}
}

// List returns every pairing the owner holds, active and revoked, oldest first.
func (s *Service) List(ctx context.Context, userID string) ([]Pair, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("album pairing service not configured")
	}
	if _, err := db.ParseUUID(userID); err != nil {
		return nil, ErrInvalid
	}
	records, err := s.store.ListAlbumPairs(ctx, userID)
	if err != nil {
		return nil, err
	}
	pairs := make([]Pair, 0, len(records))
	for _, record := range records {
		pairs = append(pairs, pairFromRecord(record))
	}
	return pairs, nil
}

// Upsert activates the pairing. The same label reused for a different bot, or
// after an unbind, updates the existing row instead of minting a second
// identity; the bot's previous active target is retired in the same write.
func (s *Service) Upsert(ctx context.Context, userID, pairKey, botID string) (Pair, error) {
	if s == nil || s.store == nil {
		return Pair{}, errors.New("album pairing service not configured")
	}
	userID, pairKey, botID = strings.TrimSpace(userID), strings.TrimSpace(pairKey), strings.TrimSpace(botID)
	if !albumwriteback.ValidPairKey(pairKey) {
		return Pair{}, ErrInvalid
	}
	if _, err := db.ParseUUID(userID); err != nil {
		return Pair{}, ErrInvalid
	}
	if _, err := db.ParseUUID(botID); err != nil {
		return Pair{}, ErrInvalid
	}
	record, err := s.store.UpsertAlbumPair(ctx, dbstore.AlbumPairRecord{UserID: userID, BotID: botID, PairKey: pairKey})
	if err != nil {
		if isForeignKeyViolation(err) {
			return Pair{}, ErrBotUnavailable
		}
		return Pair{}, err
	}
	return pairFromRecord(record), nil
}

// Revoke unbinds the label without deleting the row: the key stays occupied and
// queued write-backs stay queued. Repeating it keeps the first timestamp.
func (s *Service) Revoke(ctx context.Context, userID, pairKey string) (Pair, error) {
	if s == nil || s.store == nil {
		return Pair{}, errors.New("album pairing service not configured")
	}
	userID, pairKey = strings.TrimSpace(userID), strings.TrimSpace(pairKey)
	if !albumwriteback.ValidPairKey(pairKey) {
		return Pair{}, ErrInvalid
	}
	if _, err := db.ParseUUID(userID); err != nil {
		return Pair{}, ErrInvalid
	}
	record, err := s.store.RevokeAlbumPair(ctx, userID, pairKey)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return Pair{}, ErrNotFound
		}
		return Pair{}, err
	}
	return pairFromRecord(record), nil
}

// Resolve returns the owner's active pairing for one bot, or ErrNotFound when
// the owner designated none. It is the only lookup automatic queueing needs:
// the pipeline already knows the user and the bot at its terminal commit.
func (s *Service) Resolve(ctx context.Context, userID, botID string) (Pair, error) {
	if s == nil || s.store == nil {
		return Pair{}, errors.New("album pairing service not configured")
	}
	if _, err := db.ParseUUID(userID); err != nil {
		return Pair{}, ErrInvalid
	}
	if _, err := db.ParseUUID(botID); err != nil {
		return Pair{}, ErrInvalid
	}
	record, err := s.store.ResolveActiveAlbumPair(ctx, userID, botID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return Pair{}, ErrNotFound
		}
		return Pair{}, err
	}
	return pairFromRecord(record), nil
}

func pairFromRecord(record dbstore.AlbumPairRecord) Pair {
	return Pair{
		PairKey:   record.PairKey,
		BotID:     record.BotID,
		State:     record.State,
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// isForeignKeyViolation reports the SQLSTATE a write gets when the pairing
// names a bot this team does not have.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
