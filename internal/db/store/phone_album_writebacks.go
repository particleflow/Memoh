package store

import (
	"context"
	"time"
)

// PhoneAlbumWritebackRecord is one phone album write-back intent. The identity
// is the existing content hash; (user, pair key, content hash) is unique, so
// enqueueing the same item twice keeps one row.
type PhoneAlbumWritebackRecord struct {
	ID          string
	UserID      string
	PairKey     string
	BotID       string
	ContentHash string
	Mime        string
	Name        string
	State       string
	SizeBytes   *int64
	CreatedAt   time.Time
	// WrittenAt is zero until the phone reports the item written.
	WrittenAt time.Time
}

// PhoneAlbumWritebackStore owns the write-back queue. Enqueue is upsert: a hash
// already queued for the same pairing stays one row, and one that was written
// returns to pending. Mark is idempotent.
type PhoneAlbumWritebackStore interface {
	EnqueuePhoneAlbumWriteback(context.Context, PhoneAlbumWritebackRecord) (PhoneAlbumWritebackRecord, error)
	ListPendingPhoneAlbumWritebacks(context.Context, string) ([]PhoneAlbumWritebackRecord, error)
	MarkPhoneAlbumWritebackWritten(context.Context, string, string, string) (PhoneAlbumWritebackRecord, error)
}
