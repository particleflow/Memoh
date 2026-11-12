package albumpairs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

const (
	testUser = "11111111-1111-1111-1111-111111111111"
	testBot  = "22222222-2222-2222-2222-222222222222"
	otherBot = "33333333-3333-3333-3333-333333333333"
)

// memoryStore keeps the same contract as the PostgreSQL store: activating a
// label retires the owner's other active target for the same bot, and a revoked
// row keeps its identity.
type memoryStore struct {
	mu    sync.Mutex
	rows  []dbstore.AlbumPairRecord
	clock time.Time
}

func (s *memoryStore) tick() time.Time {
	s.clock = s.clock.Add(time.Second)
	return s.clock
}

func (s *memoryStore) ListAlbumPairs(_ context.Context, userID string) ([]dbstore.AlbumPairRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []dbstore.AlbumPairRecord
	for _, row := range s.rows {
		if row.UserID == userID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *memoryStore) UpsertAlbumPair(_ context.Context, record dbstore.AlbumPairRecord) (dbstore.AlbumPairRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		row := &s.rows[i]
		if row.UserID == record.UserID && row.BotID == record.BotID && row.State == StateActive && row.PairKey != record.PairKey {
			row.State = StateRevoked
			row.UpdatedAt = s.tick()
		}
	}
	for i := range s.rows {
		row := &s.rows[i]
		if row.UserID == record.UserID && row.PairKey == record.PairKey {
			if row.BotID != record.BotID || row.State != StateActive {
				row.BotID = record.BotID
				row.State = StateActive
				row.UpdatedAt = s.tick()
			}
			return *row, nil
		}
	}
	now := s.tick()
	record.ID = record.PairKey
	record.State = StateActive
	record.CreatedAt, record.UpdatedAt = now, now
	s.rows = append(s.rows, record)
	return record, nil
}

func (s *memoryStore) RevokeAlbumPair(_ context.Context, userID, pairKey string) (dbstore.AlbumPairRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		row := &s.rows[i]
		if row.UserID == userID && row.PairKey == pairKey {
			if row.State != StateRevoked {
				row.State = StateRevoked
				row.UpdatedAt = s.tick()
			}
			return *row, nil
		}
	}
	return dbstore.AlbumPairRecord{}, db.ErrNotFound
}

func (s *memoryStore) ResolveActiveAlbumPair(_ context.Context, userID, botID string) (dbstore.AlbumPairRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.rows {
		if row.UserID == userID && row.BotID == botID && row.State == StateActive {
			return row, nil
		}
	}
	return dbstore.AlbumPairRecord{}, db.ErrNotFound
}

func newTestService() (*Service, *memoryStore) {
	store := &memoryStore{clock: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)}
	return NewService(nil, store), store
}

func TestUpsertActivatesAndReusesTheSameLabel(t *testing.T) {
	service, store := newTestService()

	first, err := service.Upsert(context.Background(), testUser, "red-jade", testBot)
	if err != nil || first.State != StateActive || first.PairKey != "red-jade" || first.BotID != testBot {
		t.Fatalf("Upsert = %+v, %v", first, err)
	}
	// Repeating the same activation keeps the row's creation time.
	repeat, err := service.Upsert(context.Background(), testUser, "red-jade", testBot)
	if err != nil {
		t.Fatalf("repeat Upsert: %v", err)
	}
	if repeat.CreatedAt != first.CreatedAt || repeat.UpdatedAt != first.UpdatedAt {
		t.Fatalf("repeat changed timestamps: %+v -> %+v", first, repeat)
	}
	if len(store.rows) != 1 {
		t.Fatalf("repeat minted %d rows", len(store.rows))
	}
}

func TestUpsertRetiresTheBotsPreviousTarget(t *testing.T) {
	service, store := newTestService()

	if _, err := service.Upsert(context.Background(), testUser, "first", testBot); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}
	if _, err := service.Upsert(context.Background(), testUser, "second", testBot); err != nil {
		t.Fatalf("Upsert second: %v", err)
	}
	if len(store.rows) != 2 {
		t.Fatalf("rows = %d, want both labels retained", len(store.rows))
	}
	resolved, err := service.Resolve(context.Background(), testUser, testBot)
	if err != nil || resolved.PairKey != "second" {
		t.Fatalf("active target = %+v, %v", resolved, err)
	}
	list, err := service.List(context.Background(), testUser)
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %+v, %v", list, err)
	}
	states := map[string]string{}
	for _, pair := range list {
		states[pair.PairKey] = pair.State
	}
	if states["first"] != StateRevoked || states["second"] != StateActive {
		t.Fatalf("states = %+v", states)
	}
}

func TestUpsertMovesALabelToAnotherBot(t *testing.T) {
	service, _ := newTestService()

	if _, err := service.Upsert(context.Background(), testUser, "shared", testBot); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	moved, err := service.Upsert(context.Background(), testUser, "shared", otherBot)
	if err != nil || moved.BotID != otherBot {
		t.Fatalf("moved = %+v, %v", moved, err)
	}
	if _, err := service.Resolve(context.Background(), testUser, testBot); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old bot still resolves: %v", err)
	}
	if pair, err := service.Resolve(context.Background(), testUser, otherBot); err != nil || pair.PairKey != "shared" {
		t.Fatalf("new bot resolve = %+v, %v", pair, err)
	}
}

func TestRevokeKeepsTheRowAndIsIdempotent(t *testing.T) {
	service, store := newTestService()

	active, err := service.Upsert(context.Background(), testUser, "red-jade", testBot)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	revoked, err := service.Revoke(context.Background(), testUser, "red-jade")
	if err != nil || revoked.State != StateRevoked {
		t.Fatalf("Revoke = %+v, %v", revoked, err)
	}
	if revoked.CreatedAt != active.CreatedAt {
		t.Fatalf("Revoke changed creation time: %q -> %q", active.CreatedAt, revoked.CreatedAt)
	}
	repeat, err := service.Revoke(context.Background(), testUser, "red-jade")
	if err != nil || repeat.UpdatedAt != revoked.UpdatedAt {
		t.Fatalf("repeat Revoke = %+v, %v", repeat, err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("Revoke deleted the row: %d rows", len(store.rows))
	}
	if _, err := service.Resolve(context.Background(), testUser, testBot); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked pair still resolves: %v", err)
	}
	// Naming the same album again reuses the same row and key.
	reactivated, err := service.Upsert(context.Background(), testUser, "red-jade", testBot)
	if err != nil || reactivated.State != StateActive || reactivated.CreatedAt != active.CreatedAt {
		t.Fatalf("reactivated = %+v, %v", reactivated, err)
	}
}

func TestServiceRejectsMalformedInput(t *testing.T) {
	service, _ := newTestService()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"empty label", func() error { _, err := service.Upsert(context.Background(), testUser, "", testBot); return err }},
		{"label with space", func() error { _, err := service.Upsert(context.Background(), testUser, "a b", testBot); return err }},
		{"label too long", func() error {
			_, err := service.Upsert(context.Background(), testUser, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", testBot)
			return err
		}},
		{"malformed bot", func() error { _, err := service.Upsert(context.Background(), testUser, "p", "not-a-uuid"); return err }},
		{"malformed owner", func() error { _, err := service.List(context.Background(), "not-a-uuid"); return err }},
		{"revoke unknown label", func() error { _, err := service.Revoke(context.Background(), testUser, "absent"); return err }},
	} {
		if err := tc.call(); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s error = %v, want ErrInvalid or ErrNotFound", tc.name, err)
		}
	}
}

func TestServiceReportsAnUnconfiguredStore(t *testing.T) {
	var service *Service
	if _, err := service.List(context.Background(), testUser); err == nil {
		t.Fatal("nil service listed pairs")
	}
}
