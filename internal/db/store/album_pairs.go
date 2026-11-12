package store

import (
	"context"
	"time"
)

// AlbumPairRecord is one user-designated album pairing: which processing line
// (bot) writes finished products into which pairing label, and whether
// automatic queueing is on. It carries no album uri, album name or device
// identity; the phone resolves the label to an album on that device.
type AlbumPairRecord struct {
	ID        string
	UserID    string
	BotID     string
	PairKey   string
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AlbumPairStore owns the pairing archive. Activating a target retires the
// owner's previous active target for the same bot in one transaction, so the
// single-target constraint never sees two active rows. Revoking is a soft state
// change: the row and its key stay, and naming the same album again reuses it.
type AlbumPairStore interface {
	ListAlbumPairs(context.Context, string) ([]AlbumPairRecord, error)
	// UpsertAlbumPair activates the pairing. A pairing the owner already holds
	// keeps its row identity and creation time.
	UpsertAlbumPair(context.Context, AlbumPairRecord) (AlbumPairRecord, error)
	RevokeAlbumPair(context.Context, string, string) (AlbumPairRecord, error)
	ResolveActiveAlbumPair(context.Context, string, string) (AlbumPairRecord, error)
}
