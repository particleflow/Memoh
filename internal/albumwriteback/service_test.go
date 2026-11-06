package albumwriteback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
)

const (
	testUser = "11111111-1111-1111-1111-111111111111"
	testBot  = "22222222-2222-2222-2222-222222222222"
)

func testHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

type memoryStore struct {
	mu     sync.Mutex
	rows   []dbstore.PhoneAlbumWritebackRecord
	nextAt time.Time
	order  int
}

func (s *memoryStore) EnqueuePhoneAlbumWriteback(_ context.Context, record dbstore.PhoneAlbumWritebackRecord) (dbstore.PhoneAlbumWritebackRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		row := &s.rows[i]
		if row.UserID == record.UserID && row.PairKey == record.PairKey && row.ContentHash == record.ContentHash {
			if row.State == StateWritten {
				s.order++
				row.CreatedAt = s.nextAt.Add(time.Duration(s.order) * time.Second)
				row.WrittenAt = time.Time{}
			}
			row.State = StatePending
			row.BotID = record.BotID
			row.Mime = record.Mime
			if row.Name == "" {
				row.Name = record.Name
			}
			row.SizeBytes = record.SizeBytes
			return *row, nil
		}
	}
	s.order++
	record.ID = record.ContentHash
	record.State = StatePending
	record.CreatedAt = s.nextAt.Add(time.Duration(s.order) * time.Second)
	s.rows = append(s.rows, record)
	return record, nil
}

func (s *memoryStore) ListPendingPhoneAlbumWritebacks(_ context.Context, userID string) ([]dbstore.PhoneAlbumWritebackRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dbstore.PhoneAlbumWritebackRecord, 0, len(s.rows))
	for _, row := range s.rows {
		if row.UserID == userID && row.State == StatePending {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *memoryStore) MarkPhoneAlbumWritebackWritten(_ context.Context, userID, pairKey, contentHash string) (dbstore.PhoneAlbumWritebackRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		row := &s.rows[i]
		if row.UserID == userID && row.PairKey == pairKey && row.ContentHash == contentHash {
			if row.State != StateWritten {
				row.State = StateWritten
				row.WrittenAt = s.nextAt.Add(time.Hour)
			}
			return *row, nil
		}
	}
	return dbstore.PhoneAlbumWritebackRecord{}, db.ErrNotFound
}

type fakeAssets struct {
	known map[string]media.Asset
}

func (f fakeAssets) Stat(_ context.Context, botID, contentHash string) (media.Asset, error) {
	asset, ok := f.known[contentHash]
	if !ok || asset.BotID != botID {
		return media.Asset{}, media.ErrAssetNotFound
	}
	return asset, nil
}

func newTestService(t *testing.T, hashes ...string) (*Service, *memoryStore) {
	t.Helper()
	known := map[string]media.Asset{}
	for _, hash := range hashes {
		known[hash] = media.Asset{ContentHash: hash, BotID: testBot, Mime: "image/jpeg", SizeBytes: 1024, StorageKey: hash[:2] + "/" + hash + ".jpg"}
	}
	store := &memoryStore{nextAt: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)}
	return NewService(nil, store, fakeAssets{known: known}), store
}

func TestEnqueueRejectsInvalidPairingAndHashes(t *testing.T) {
	hash := testHash("a")
	service, store := newTestService(t, hash)

	for _, in := range []EnqueueInput{
		{PairKey: "", ContentHashes: []string{hash}},
		{PairKey: "has space", ContentHashes: []string{hash}},
		{PairKey: "ok", ContentHashes: nil},
		{PairKey: "ok", ContentHashes: []string{"short"}},
		{PairKey: "ok", ContentHashes: []string{strings.ToUpper(hash)}},
	} {
		if _, err := service.Enqueue(context.Background(), testUser, testBot, in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Enqueue(%+v) error = %v, want ErrInvalid", in, err)
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("rejected calls wrote %d rows", len(store.rows))
	}
}

func TestEnqueueRejectsUnknownAssetWithoutPartialWrite(t *testing.T) {
	known := testHash("known")
	unknown := testHash("unknown")
	service, store := newTestService(t, known)

	_, err := service.Enqueue(context.Background(), testUser, testBot, EnqueueInput{
		PairKey:       "processed-default",
		ContentHashes: []string{known, unknown},
	})
	if !errors.Is(err, ErrAssetUnavailable) {
		t.Fatalf("Enqueue error = %v, want ErrAssetUnavailable", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("partial enqueue wrote %d rows", len(store.rows))
	}
}

func TestEnqueueIsIdempotentAndDeduplicatesWithinCall(t *testing.T) {
	hash := testHash("dup")
	service, store := newTestService(t, hash)

	first, err := service.Enqueue(context.Background(), testUser, testBot, EnqueueInput{
		PairKey:       "processed-default",
		ContentHashes: []string{hash, hash},
		Name:          "结果.jpg",
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(first.Enqueued) != 1 || first.Enqueued[0] != hash {
		t.Fatalf("Enqueued = %v, want one %s", first.Enqueued, hash)
	}
	if len(store.rows) != 1 {
		t.Fatalf("rows after call with a duplicate hash = %d, want 1", len(store.rows))
	}
	if _, err := service.Enqueue(context.Background(), testUser, testBot, EnqueueInput{
		PairKey:       "processed-default",
		ContentHashes: []string{hash},
	}); err != nil {
		t.Fatalf("repeat Enqueue: %v", err)
	}
	pending, err := service.Pending(context.Background(), testUser)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending.Items) != 1 {
		t.Fatalf("pending items = %d, want 1", len(pending.Items))
	}
	if pending.Items[0].Mime != "image/jpeg" || pending.Items[0].SizeBytes == nil || *pending.Items[0].SizeBytes != 1024 {
		t.Fatalf("item metadata = %+v", pending.Items[0])
	}
	if pending.Items[0].Name != "结果.jpg" {
		t.Fatalf("caller-supplied name = %q", pending.Items[0].Name)
	}
}

func TestEnqueueWithoutNameUsesStorageBasename(t *testing.T) {
	hash := testHash("basename")
	service, _ := newTestService(t, hash)
	if _, err := service.Enqueue(context.Background(), testUser, testBot, EnqueueInput{PairKey: "p", ContentHashes: []string{hash}}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	pending, err := service.Pending(context.Background(), testUser)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending.Items) != 1 || pending.Items[0].Name != hash+".jpg" {
		t.Fatalf("derived name = %+v", pending.Items)
	}
}

func TestEnqueueAfterWrittenReturnsToPending(t *testing.T) {
	hash := testHash("rewrite")
	service, _ := newTestService(t, hash)

	if _, err := service.Enqueue(context.Background(), testUser, testBot, EnqueueInput{PairKey: "p", ContentHashes: []string{hash}}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := service.MarkWritten(context.Background(), testUser, "p", hash); err != nil {
		t.Fatalf("MarkWritten: %v", err)
	}
	pending, err := service.Pending(context.Background(), testUser)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending.Items) != 0 {
		t.Fatalf("written item still pending: %+v", pending.Items)
	}
	if _, err := service.Enqueue(context.Background(), testUser, testBot, EnqueueInput{PairKey: "p", ContentHashes: []string{hash}}); err != nil {
		t.Fatalf("re-Enqueue: %v", err)
	}
	pending, err = service.Pending(context.Background(), testUser)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending.Items) != 1 || pending.Items[0].ContentHash != hash {
		t.Fatalf("re-enqueued item missing: %+v", pending.Items)
	}
}

func TestMarkWrittenIsIdempotentAndOwnerScoped(t *testing.T) {
	hash := testHash("mark")
	service, _ := newTestService(t, hash)
	if _, err := service.Enqueue(context.Background(), testUser, testBot, EnqueueInput{PairKey: "p", ContentHashes: []string{hash}}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	first, err := service.MarkWritten(context.Background(), testUser, "p", hash)
	if err != nil {
		t.Fatalf("MarkWritten: %v", err)
	}
	second, err := service.MarkWritten(context.Background(), testUser, "p", hash)
	if err != nil {
		t.Fatalf("repeat MarkWritten: %v", err)
	}
	if first.CreatedAt != second.CreatedAt {
		t.Fatalf("repeat changed createdAt: %q -> %q", first.CreatedAt, second.CreatedAt)
	}
	other := "33333333-3333-3333-3333-333333333333"
	if _, err := service.MarkWritten(context.Background(), other, "p", hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner MarkWritten error = %v, want ErrNotFound", err)
	}
	if _, err := service.MarkWritten(context.Background(), "not-a-uuid", "p", hash); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed owner error = %v, want ErrInvalid", err)
	}
}

func TestPendingKeepsEnqueueOrderPerOwner(t *testing.T) {
	one, two := testHash("one"), testHash("two")
	service, _ := newTestService(t, one, two)
	if _, err := service.Enqueue(context.Background(), testUser, testBot, EnqueueInput{PairKey: "p", ContentHashes: []string{two}}); err != nil {
		t.Fatalf("Enqueue two: %v", err)
	}
	if _, err := service.Enqueue(context.Background(), testUser, testBot, EnqueueInput{PairKey: "p", ContentHashes: []string{one}}); err != nil {
		t.Fatalf("Enqueue one: %v", err)
	}
	pending, err := service.Pending(context.Background(), testUser)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending.Items) != 2 || pending.Items[0].ContentHash != two || pending.Items[1].ContentHash != one {
		t.Fatalf("pending order = %+v", pending.Items)
	}
	if pending.Items[0].PairKey != "p" || pending.Items[0].BotID != testBot {
		t.Fatalf("pending identity = %+v", pending.Items[0])
	}
}
