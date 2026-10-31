package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type delegationSizeAuthority struct{ *delegatedImageAuthority }

func (f delegationSizeAuthority) ResultHasAsset(_ context.Context, bot, user, session, hash string) (bool, error) {
	return bot == imageSpawnBot && user == imageSpawnUser && session == "synthetic-target" && hash == f.hash, nil
}

type delegationSizeStorage struct {
	*localfs.Originals
	opens  int
	onOpen func()
}

func (s *delegationSizeStorage) OpenVerifiedRange(ctx context.Context, key string) (storage.VerifiedRangeReader, error) {
	s.opens++
	r, e := s.Originals.OpenVerifiedRange(ctx, key)
	if e == nil && s.onOpen != nil {
		s.onOpen()
	}
	return r, e
}

func delegationSizeFixture(t *testing.T, input bool) (*delegationHost, botdelegation.Record, *delegationSizeAuthority, *delegationSizeStorage, media.Asset, int64) {
	t.Helper()
	authority := &delegationSizeAuthority{&delegatedImageAuthority{active: true, sessions: map[string]thread.Thread{
		"synthetic-source": {ID: "synthetic-source", BotID: imageSpawnOtherBot, CreatedByUserID: imageSpawnUser, TeamID: "synthetic-team", Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
		"synthetic-target": {ID: "synthetic-target", BotID: imageSpawnBot, CreatedByUserID: imageSpawnUser, TeamID: "synthetic-team", Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
	}}}
	originals := &delegationSizeStorage{Originals: localfs.NewOriginals(t.TempDir())}
	assets := media.NewService(nil, originals)
	assets.SetRequirePersistence(true)
	owner := imageSpawnBot
	if input {
		owner = imageSpawnOtherBot
	}
	var payload bytes.Buffer
	if err := png.Encode(&payload, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	asset, e := assets.Ingest(t.Context(), media.IngestInput{BotID: owner, Mime: "image/png", Reader: bytes.NewReader(payload.Bytes())})
	if e != nil {
		t.Fatal(e)
	}
	authority.hash = asset.ContentHash
	a := &botdelegation.Authority{Permissions: authority, Sessions: authority, Assets: authority}
	b, _, e := a.Bind(t.Context(), imageSpawnUser, imageSpawnOtherBot, "synthetic-source", imageSpawnBot)
	if e != nil {
		t.Fatal(e)
	}
	b.TargetSessionID = "synthetic-target"
	record := botdelegation.Record{ID: "synthetic-size-handoff", Identity: b, State: "completed", SourceSelection: "native", Payload: botdelegation.Payload{TargetSelection: "native"}, Result: botdelegation.Result{Outputs: []string{asset.ContentHash}}}
	if input {
		record.Payload.Inputs = []string{asset.ContentHash}
	}
	service := botdelegation.NewService(a, delegatedImageLedger{record: record}, assets, nil, time.Minute)
	t.Cleanup(service.Close)
	return &delegationHost{service: service, media: assets}, record, authority, originals, asset, int64(payload.Len())
}

func TestDelegationAttachmentSizeFromStoredOriginal(t *testing.T) {
	for _, input := range []bool{false, true} {
		name := "new-output"
		if input {
			name = "returned-input"
		}
		t.Run(name, func(t *testing.T) {
			host, r, _, _, asset, size := delegationSizeFixture(t, input)
			located, e := host.media.Stat(t.Context(), asset.BotID, asset.ContentHash)
			if e != nil || located.SizeBytes != 0 {
				t.Fatal("fixture no longer represents size-less locator", located, e)
			}
			raw, _ := json.Marshal(map[string]any{"type": "attachment_delta", "attachments": []any{map[string]any{"content_hash": asset.ContentHash, "type": "image", "size": size + 99}}})
			refs, e := host.outboundRefs(t.Context(), r, raw)
			if e != nil || len(refs) != 1 || refs[0].SizeBytes != size {
				t.Errorf("stored-original size lost: refs=%+v expected=%d error=%v", refs, size, e)
			}
			projected, e := host.inputAttachments(t.Context(), r, true)
			if e != nil || len(projected) != 1 || projected[0].Size != size {
				t.Fatalf("return size lost: attachments=%+v expected=%d error=%v", projected, size, e)
			}
			// A delegated unchanged input keeps its original Bot owner; no media alias.
			if input {
				if _, e := host.media.Stat(t.Context(), imageSpawnBot, asset.ContentHash); !errors.Is(e, media.ErrAssetNotFound) {
					t.Fatal("input copied into target scope", e)
				}
			}
		})
	}
}

func TestDelegationAttachmentSizeStillRequiresCurrentAuthority(t *testing.T) {
	for _, mode := range []string{"revoked", "during-open", "wrong-team", "unlisted-reference"} {
		t.Run(mode, func(t *testing.T) {
			host, r, authority, originals, asset, _ := delegationSizeFixture(t, false)
			switch mode {
			case "revoked":
				authority.active = false
			case "during-open":
				originals.onOpen = func() { authority.active = false }
			case "wrong-team":
				r.Identity.TeamID = "other-team"
			case "unlisted-reference":
				r.Result.Outputs = []string{"undelegated"}
			}
			if _, e := host.inputAttachments(t.Context(), r, true); !errors.Is(e, botdelegation.ErrDenied) {
				t.Fatalf("unauthorized attachment accepted: %v", e)
			}
			if mode != "during-open" && originals.opens != 0 {
				t.Fatal("unauthorized original opened")
			}
			if asset.SizeBytes == 0 {
				t.Fatal("fixture ingest did not measure original")
			}
		})
	}
}

func TestDelegationAttachmentSizeCaptureRechecksAuthority(t *testing.T) {
	for _, mode := range []string{"revoked", "during-open", "wrong-session", "missing-original"} {
		t.Run(mode, func(t *testing.T) {
			host, r, authority, originals, asset, _ := delegationSizeFixture(t, false)
			hash := asset.ContentHash
			switch mode {
			case "revoked":
				authority.active = false
			case "during-open":
				originals.onOpen = func() { authority.active = false }
			case "wrong-session":
				r.Identity.SourceSessionID = "other-session"
			case "missing-original":
				hash = string(bytes.Repeat([]byte("a"), 64))
			}
			raw, _ := json.Marshal(map[string]any{"type": "attachment_delta", "attachments": []any{map[string]any{"content_hash": hash, "size": 999999}}})
			refs, err := host.outboundRefs(t.Context(), r, raw)
			if err == nil || len(refs) != 0 {
				t.Fatalf("unconfirmed attachment was linked: refs=%+v error=%v", refs, err)
			}
			if mode != "during-open" && originals.opens != 0 {
				t.Fatal("original opened outside authority")
			}
		})
	}
}
