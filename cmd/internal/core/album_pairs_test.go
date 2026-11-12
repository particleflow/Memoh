package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/felinics/memoh/internal/albumpairs"
	"github.com/felinics/memoh/internal/albumwriteback"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

const (
	sinkUser = "11111111-1111-1111-1111-111111111111"
	sinkBot  = "22222222-2222-2222-2222-222222222222"
)

func sinkHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

type sinkPairStore struct {
	mu   sync.Mutex
	rows []dbstore.AlbumPairRecord
}

func (s *sinkPairStore) ListAlbumPairs(context.Context, string) ([]dbstore.AlbumPairRecord, error) {
	return nil, nil
}

func (s *sinkPairStore) UpsertAlbumPair(_ context.Context, record dbstore.AlbumPairRecord) (dbstore.AlbumPairRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, record)
	return record, nil
}

func (s *sinkPairStore) RevokeAlbumPair(context.Context, string, string) (dbstore.AlbumPairRecord, error) {
	return dbstore.AlbumPairRecord{}, db.ErrNotFound
}

func (s *sinkPairStore) ResolveActiveAlbumPair(_ context.Context, userID, botID string) (dbstore.AlbumPairRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.rows {
		if row.UserID == userID && row.BotID == botID && row.State == albumpairs.StateActive {
			return row, nil
		}
	}
	return dbstore.AlbumPairRecord{}, db.ErrNotFound
}

type sinkWritebackStore struct {
	mu   sync.Mutex
	rows []dbstore.PhoneAlbumWritebackRecord
}

func (s *sinkWritebackStore) EnqueuePhoneAlbumWriteback(_ context.Context, record dbstore.PhoneAlbumWritebackRecord) (dbstore.PhoneAlbumWritebackRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		if s.rows[i].UserID == record.UserID && s.rows[i].PairKey == record.PairKey && s.rows[i].ContentHash == record.ContentHash {
			s.rows[i] = record
			return record, nil
		}
	}
	s.rows = append(s.rows, record)
	return record, nil
}

func (s *sinkWritebackStore) ListPendingPhoneAlbumWritebacks(context.Context, string) ([]dbstore.PhoneAlbumWritebackRecord, error) {
	return nil, nil
}

func (s *sinkWritebackStore) MarkPhoneAlbumWritebackWritten(context.Context, string, string, string) (dbstore.PhoneAlbumWritebackRecord, error) {
	return dbstore.PhoneAlbumWritebackRecord{}, db.ErrNotFound
}

func sinkIdentity() computertask.DeliveryIdentity {
	return computertask.DeliveryIdentity{
		Binding:     toolcontext.ComputerBinding{BotID: sinkBot, UserID: sinkUser},
		OperationID: "operation",
	}
}

func newAlbumSink(t *testing.T) (computertask.DeliverySink, *sinkPairStore, *sinkWritebackStore) {
	t.Helper()
	pairs := &sinkPairStore{}
	writebacks := &sinkWritebackStore{}
	sink := AlbumDeliveryCommitSink(slog.New(slog.DiscardHandler), albumpairs.NewService(nil, pairs), albumwriteback.NewService(nil, writebacks, nil))
	if sink == nil {
		t.Fatal("sink is nil")
	}
	return sink, pairs, writebacks
}

func TestAlbumSinkQueuesTheActivePairing(t *testing.T) {
	sink, pairs, writebacks := newAlbumSink(t)
	pairs.rows = []dbstore.AlbumPairRecord{{UserID: sinkUser, BotID: sinkBot, PairKey: "red-jade", State: albumpairs.StateActive}}

	hash := sinkHash("product")
	sink(context.Background(), sinkIdentity(), computertask.DeliveryRequest{
		Version:     1,
		Direction:   "upload",
		ContentHash: hash,
		SizeBytes:   2048,
		Mime:        "image/jpeg",
	})

	if len(writebacks.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(writebacks.rows))
	}
	row := writebacks.rows[0]
	if row.PairKey != "red-jade" || row.ContentHash != hash || row.BotID != sinkBot || row.UserID != sinkUser {
		t.Fatalf("queued row = %+v", row)
	}
	if row.Mime != "image/jpeg" || row.SizeBytes == nil || *row.SizeBytes != 2048 {
		t.Fatalf("queued metadata = %+v", row)
	}
	if row.Name != "" {
		t.Fatalf("sink invented a filename: %q", row.Name)
	}
}

// No pairing means no work at all: nothing queued, no placeholder, no text.
func TestAlbumSinkIsSilentWithoutAPairing(t *testing.T) {
	sink, _, writebacks := newAlbumSink(t)

	sink(context.Background(), sinkIdentity(), computertask.DeliveryRequest{
		Version:     1,
		Direction:   "upload",
		ContentHash: sinkHash("orphan"),
		SizeBytes:   1,
		Mime:        "image/png",
	})
	if len(writebacks.rows) != 0 {
		t.Fatalf("unpaired commit queued %d rows", len(writebacks.rows))
	}
}

// A download delivery writes onto the device, not onto the NAS.
func TestAlbumSinkSkipsDownloadDeliveries(t *testing.T) {
	sink, pairs, writebacks := newAlbumSink(t)
	pairs.rows = []dbstore.AlbumPairRecord{{UserID: sinkUser, BotID: sinkBot, PairKey: "red-jade", State: albumpairs.StateActive}}

	sink(context.Background(), sinkIdentity(), computertask.DeliveryRequest{
		Version:     1,
		Direction:   "download",
		ContentHash: sinkHash("download"),
		SizeBytes:   1,
		Mime:        "image/png",
	})
	if len(writebacks.rows) != 0 {
		t.Fatalf("download delivery queued %d rows", len(writebacks.rows))
	}
}

// A revoked pairing is the same as no pairing: the intent is off.
func TestAlbumSinkIgnoresRevokedPairings(t *testing.T) {
	sink, pairs, writebacks := newAlbumSink(t)
	pairs.rows = []dbstore.AlbumPairRecord{{UserID: sinkUser, BotID: sinkBot, PairKey: "red-jade", State: albumpairs.StateRevoked}}

	sink(context.Background(), sinkIdentity(), computertask.DeliveryRequest{
		Version:     1,
		Direction:   "upload",
		ContentHash: sinkHash("revoked"),
		SizeBytes:   1,
		Mime:        "image/png",
	})
	if len(writebacks.rows) != 0 {
		t.Fatalf("revoked pairing queued %d rows", len(writebacks.rows))
	}
}

// The sink is best effort: a failing lookup or write must not panic and must
// not change the commit, so it only logs.
func TestAlbumSinkSwallowsFailures(t *testing.T) {
	sink := AlbumDeliveryCommitSink(slog.New(slog.DiscardHandler), albumpairs.NewService(nil, failingPairStore{}), albumwriteback.NewService(nil, &sinkWritebackStore{}, nil))
	sink(context.Background(), sinkIdentity(), computertask.DeliveryRequest{
		Version:     1,
		Direction:   "upload",
		ContentHash: sinkHash("broken"),
		SizeBytes:   1,
		Mime:        "image/png",
	})
}

// The sink is registered only when both services exist.
func TestAlbumSinkRequiresBothServices(t *testing.T) {
	if AlbumDeliveryCommitSink(nil, nil, albumwriteback.NewService(nil, &sinkWritebackStore{}, nil)) != nil {
		t.Fatal("sink registered without the pairing archive")
	}
	if AlbumDeliveryCommitSink(nil, albumpairs.NewService(nil, &sinkPairStore{}), nil) != nil {
		t.Fatal("sink registered without the write-back queue")
	}
}

type failingPairStore struct{}

func (failingPairStore) ListAlbumPairs(context.Context, string) ([]dbstore.AlbumPairRecord, error) {
	return nil, errors.New("store offline")
}

func (failingPairStore) UpsertAlbumPair(context.Context, dbstore.AlbumPairRecord) (dbstore.AlbumPairRecord, error) {
	return dbstore.AlbumPairRecord{}, errors.New("store offline")
}

func (failingPairStore) RevokeAlbumPair(context.Context, string, string) (dbstore.AlbumPairRecord, error) {
	return dbstore.AlbumPairRecord{}, errors.New("store offline")
}

func (failingPairStore) ResolveActiveAlbumPair(context.Context, string, string) (dbstore.AlbumPairRecord, error) {
	return dbstore.AlbumPairRecord{}, errors.New("store offline")
}
