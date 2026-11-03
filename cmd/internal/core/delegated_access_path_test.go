package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/fallback"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

// accessPathWorkspace is an in-memory workspace whose access paths mirror the
// container media mount.
type accessPathWorkspace struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newAccessPathWorkspace() *accessPathWorkspace {
	return &accessPathWorkspace{values: map[string][]byte{}}
}

func (p *accessPathWorkspace) Put(_ context.Context, key string, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.values[key] = data
	return nil
}

func (p *accessPathWorkspace) Open(_ context.Context, key string) (io.ReadCloser, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, ok := p.values[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (*accessPathWorkspace) Delete(context.Context, string) error { return nil }

func (*accessPathWorkspace) AccessPath(_ context.Context, key string) string {
	return "/data/.memoh/media/" + key
}

// G1: a delegated reference becomes readable through the consuming Bot's own
// container path. The owner's verified bytes materialize as the reading Bot's
// access copy; the canonical store never gains a second Bot directory.
func TestDelegatedAccessPathForGatewayMaterializesConsumerCopy(t *testing.T) {
	var payload bytes.Buffer
	if err := png.Encode(&payload, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	original := payload.Bytes()
	sum := sha256.Sum256(original)
	hash := hex.EncodeToString(sum[:])
	root := t.TempDir()
	workspace := newAccessPathWorkspace()
	store := media.NewService(nil, fallback.NewDurable(workspace, localfs.NewOriginals(root), newAccessPathWorkspace()))
	store.SetRequirePersistence(true)
	if _, err := store.Ingest(t.Context(), media.IngestInput{BotID: imageSpawnOtherBot, Mime: "image/png", Reader: bytes.NewReader(original)}); err != nil {
		t.Fatal(err)
	}
	authority := &delegatedImageAuthority{active: true, hash: hash, sessions: map[string]thread.Thread{
		"synthetic-source": {ID: "synthetic-source", BotID: imageSpawnOtherBot, TeamID: "synthetic-team", CreatedByUserID: imageSpawnUser, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
		imageSpawnParent:   {ID: imageSpawnParent, BotID: imageSpawnBot, TeamID: "synthetic-team", CreatedByUserID: imageSpawnUser, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
	}}
	r := botdelegation.Record{ID: "synthetic-delegation", Identity: botdelegation.Identity{TeamID: "synthetic-team", UserID: imageSpawnUser, SourceBotID: imageSpawnOtherBot, SourceSessionID: "synthetic-source", SourceRootID: "synthetic-source", TargetBotID: imageSpawnBot, TargetSessionID: imageSpawnParent}, SourceSelection: "native", Payload: botdelegation.Payload{Inputs: []string{hash}, TargetSelection: "native"}}
	delegations := botdelegation.NewService(&botdelegation.Authority{Permissions: authority, Sessions: authority, Assets: authority}, delegatedImageLedger{record: r}, store, nil, 0)
	t.Cleanup(delegations.Close)
	host := &delegationHost{service: delegations, media: store}
	delegations.Host = host
	loader := &delegationAssetLoader{base: &gatewayAssetLoaderAdapter{media: store}, host: host}

	ctx, _, err := host.prepare(t.Context(), application.ChatRequest{BotID: imageSpawnBot, UserID: imageSpawnUser, ThreadID: imageSpawnParent})
	if err != nil {
		t.Fatal(err)
	}
	accessPath, err := loader.AccessPathForGateway(ctx, imageSpawnBot, hash)
	if err != nil {
		t.Fatalf("delegated reference has no readable path: %v", err)
	}
	if !strings.HasPrefix(accessPath, "/data/.memoh/media/"+imageSpawnBot+"/") || !strings.Contains(accessPath, hash) {
		t.Fatalf("access path = %q, want the reading Bot's container path", accessPath)
	}
	key := imageSpawnBot + "/" + hash[:2] + "/" + hash + ".png"
	if !bytes.Equal(workspace.values[key], original) {
		t.Fatal("consumer access copy does not hold the owner's bytes")
	}
	if _, err := os.Stat(filepath.Join(root, imageSpawnBot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("materialization wrote canonical bytes for the reading Bot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, imageSpawnOtherBot, filepath.FromSlash(hash[:2]+"/"+hash+".png"))); err != nil {
		t.Fatalf("owner original disturbed: %v", err)
	}

	// A revoked delegation stops answering the path, exactly as it stops reads.
	authority.active = false
	if _, err := loader.AccessPathForGateway(ctx, imageSpawnBot, hash); err == nil {
		t.Fatal("revoked delegation still materialized an access path")
	}
}
