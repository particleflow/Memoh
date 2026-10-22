package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/memory/adapters"
	"github.com/felinics/memoh/internal/memory/storefs"
)

type lifecycleFiles struct {
	*fakeStore
	syncErr        error
	ingestReads    int
	removeAllCalls int
}

func (s *lifecycleFiles) RebuildFiles(ctx context.Context, bot string, items []storefs.MemoryItem, filters map[string]any) error {
	if s.syncErr != nil {
		return s.syncErr
	}
	return s.fakeStore.RebuildFiles(ctx, bot, items, filters)
}

func (s *lifecycleFiles) ReadAllMemoryFilesForIngest(ctx context.Context, bot string) ([]storefs.MemoryItem, error) {
	s.ingestReads++
	return s.fakeStore.ReadAllMemoryFilesForIngest(ctx, bot)
}

func (s *lifecycleFiles) RemoveAllMemories(ctx context.Context, bot string) error {
	s.removeAllCalls++
	return s.fakeStore.RemoveAllMemories(ctx, bot)
}

func TestDeleteReportsProjectionFailureAndRebuildDoesNotImportIt(t *testing.T) {
	ctx := t.Context()
	store := newFakeWikiStore()
	files := &lifecycleFiles{fakeStore: newFakeStore()}
	rt := NewGraphRuntime(nil, store, files)
	added, err := rt.Add(ctx, adapters.AddRequest{BotID: "bot-1", Message: "合成偏好桂花茶"})
	if err != nil {
		t.Fatal(err)
	}
	id := added.Results[0].ID
	// Warm the graph before the destructive write; cleanup must invalidate it
	// even when the derived projection cannot be rewritten.
	if _, err = rt.Search(ctx, adapters.SearchRequest{BotID: "bot-1", Query: "桂花茶"}); err != nil {
		t.Fatal(err)
	}
	files.syncErr = errForced
	response, err := rt.Delete(ctx, "bot-1", id)
	if !errors.Is(err, errForced) || response.Message != "" {
		t.Errorf("delete reported success after projection failure: %+v, %v", response, err)
	}
	if _, ok := store.nodes[id]; ok {
		t.Fatal("authoritative delete did not commit")
	}
	if _, ok := files.items[id]; !ok {
		t.Fatal("fault must leave the stale projection")
	}
	hits, err := rt.Search(ctx, adapters.SearchRequest{BotID: "bot-1", Query: "桂花茶"})
	if err != nil || len(hits.Results) != 0 {
		t.Fatalf("deleted node survived in cache: %+v, %v", hits, err)
	}
	files.syncErr = nil
	// A new runtime must recover solely from the database, without importing
	// the stale Markdown left by the acknowledged database mutation.
	rt = NewGraphRuntime(nil, store, files)
	if _, err = rt.Rebuild(ctx, "bot-1"); err != nil {
		t.Fatal(err)
	}
	if len(store.nodes) != 0 || len(files.items) != 0 || files.ingestReads != 0 {
		t.Fatalf("rebuild imported a deleted projection: nodes=%d files=%d reads=%d", len(store.nodes), len(files.items), files.ingestReads)
	}
	if _, err = rt.Delete(ctx, "bot-1", id); err != nil {
		t.Fatalf("retry of already committed delete: %v", err)
	}
}

func TestDeleteAllPreservesOriginalsAndReportsSyncFailure(t *testing.T) {
	ctx := t.Context()
	store := newFakeWikiStore()
	files := &lifecycleFiles{fakeStore: newFakeStore()}
	rt := NewGraphRuntime(nil, store, files)
	if _, err := rt.Add(ctx, adapters.AddRequest{BotID: "bot-1", Message: "合成记忆"}); err != nil {
		t.Fatal(err)
	}
	files.syncErr = errForced
	response, err := rt.DeleteAll(ctx, adapters.DeleteAllRequest{BotID: "bot-1"})
	if !errors.Is(err, errForced) || response.Message != "" {
		t.Errorf("delete all reported success: %+v, %v", response, err)
	}
	if files.removeAllCalls != 0 {
		t.Error("delete all must not remove original Markdown via recursive directory deletion")
	}
	if len(store.nodes) != 0 {
		t.Fatal("authoritative rows remain")
	}
}

func TestRebuildReportsProjectionFailureAndNeverImportsOriginals(t *testing.T) {
	files := &lifecycleFiles{fakeStore: newFakeStore(storefs.MemoryItem{ID: "authored", Memory: "原始文档"}), syncErr: errForced}
	store := newFakeWikiStore()
	rt := NewGraphRuntime(nil, store, files)
	if _, err := rt.Rebuild(t.Context(), "bot-1"); !errors.Is(err, errForced) {
		t.Fatalf("rebuild error=%v", err)
	}
	if files.ingestReads != 0 || len(store.nodes) != 0 {
		t.Fatal("rebuild silently imported originals")
	}
}

func TestDeletedProjectionIsNotReadableWhenAuthorityIsUnavailable(t *testing.T) {
	files := newFakeStore(storefs.MemoryItem{ID: "bot-1:deleted", Memory: "已删除的合成暗号"})
	rt := NewGraphRuntime(nil, &errWikiStore{}, files)
	if got, err := rt.Search(t.Context(), adapters.SearchRequest{BotID: "bot-1", Query: "合成暗号"}); err == nil || len(got.Results) != 0 {
		t.Errorf("unverified stale search result: %+v, %v", got, err)
	}
	if got, err := rt.GetAll(t.Context(), adapters.GetAllRequest{BotID: "bot-1"}); err == nil || len(got.Results) != 0 {
		t.Errorf("unverified stale listing: %+v, %v", got, err)
	}
}
