package computertask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/media"
)

type testAssets struct {
	text        string
	failOpen    bool
	afterIngest func()
}

func (a *testAssets) Ingest(_ context.Context, in media.IngestInput) (media.Asset, error) {
	raw, err := io.ReadAll(in.Reader)
	if err != nil {
		return media.Asset{}, err
	}
	a.text = string(raw)
	sum := sha256.Sum256(raw)
	if a.afterIngest != nil {
		a.afterIngest()
	}
	return media.Asset{BotID: in.BotID, ContentHash: hex.EncodeToString(sum[:]), Mime: in.Mime, SizeBytes: int64(len(raw))}, nil
}

func (a *testAssets) Open(_ context.Context, bot, hash string) (io.ReadCloser, media.Asset, error) {
	if a.failOpen {
		return nil, media.Asset{}, errors.New("not ready")
	}
	return io.NopCloser(strings.NewReader(a.text)), media.Asset{BotID: bot, ContentHash: hash}, nil
}

func TestStandardMCPArtifactRequiresReadyBytesAndCurrentGrant(t *testing.T) {
	for _, failure := range []string{"", "not-ready", "revoked"} {
		t.Run(failure, func(t *testing.T) {
			source, store, _ := setup(t)
			assets := &testAssets{failOpen: failure == "not-ready"}
			source.service.assets = assets
			c := standardClient(t, source, "pc-a", false)
			r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": "printf 42"}, false))
			if failure == "revoked" {
				assets.afterIngest = func() { store.revoked = true }
			}
			raw := invoke(t, c, "computer_task_artifact", map[string]any{"operation_id": r.ID}, failure != "")
			if failure == "" {
				got := parseResult(t, raw)
				sum := sha256.Sum256([]byte("42\n"))
				if got.ArtifactHash != hex.EncodeToString(sum[:]) {
					t.Fatal("artifact was not the stored terminal snapshot")
				}
			}
			if failure != "" && strings.Contains(string(raw), "artifact_hash") {
				t.Fatal("unready or revoked artifact reference disclosed")
			}
		})
	}
}
