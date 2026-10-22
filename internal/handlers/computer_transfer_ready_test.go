package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/transferclient"
)

// Advance only the synthetic receipt's clock, then run the real GC. Originals
// and operation evidence are never modified by this fixture helper.
func expireReadyFixture(t *testing.T, root, id string, uploads *media.UploadStore) {
	t.Helper()
	path := filepath.Join(root, ".uploads", id+".json")
	raw, err := os.ReadFile(path) //nolint:gosec // Isolated test receipt.
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || string(fields["state"]) != `"ready"` {
		t.Fatal("not a ready fixture")
	}
	fields["expires_at"], _ = json.Marshal(time.Now().Add(-time.Hour))
	raw, _ = json.Marshal(fields)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := uploads.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("receipt did not expire", err)
	}
}

func TestTaskTransferReadyProofAfterStagingGC(t *testing.T) {
	for _, captured := range []bool{true, false} {
		t.Run(map[bool]string{true: "durable-proof", false: "cross-store-gap"}[captured], func(t *testing.T) {
			root := t.TempDir()
			var assets *media.Service
			f, verifier, _, client := taskTransferHTTPFixture(t, false, taskTransferFixtureOptions{root: root, assets: func(a *media.Service) computertask.DeliveryAssets { assets = a; return a }})
			spec := transferclient.Spec{SizeBytes: f.ref.SizeBytes, SHA256: f.ref.ContentHash, Mime: f.ref.Mime}
			upload, err := client.Create(t.Context(), f.id.Binding.BotID, spec)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Patch(t.Context(), f.id.Binding.BotID, upload.ID, 0, []byte("fixture")); err != nil {
				t.Fatal(err)
			}
			if captured {
				if _, err := client.Complete(t.Context(), f.id.Binding.BotID, upload.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				// Model process loss after the Media transaction but before the
				// operation proof CAS. No successful task response exists here.
				if _, err := assets.Uploads().Complete(t.Context(), f.id.Binding.UserID, f.id.Binding.BotID, upload.ID); err != nil {
					t.Fatal(err)
				}
			}
			expireReadyFixture(t, root, upload.ID, assets.Uploads())
			got, err := client.Get(t.Context(), f.id.Binding.BotID, upload.ID)
			var status *transferclient.HTTPError
			if !captured {
				if !errors.As(err, &status) || status.Status != http.StatusNotFound {
					t.Fatal("same-hash original fabricated proof", err)
				}
			} else {
				if err != nil || got.ID != upload.ID || got.State != "ready" || got.ContentHash != spec.SHA256 {
					t.Fatal("proof recovery", got, err)
				}
				if replay, err := client.Complete(t.Context(), f.id.Binding.BotID, upload.ID); err != nil || replay != got {
					t.Fatal("idempotent complete lost proof", err)
				}
				if replay, err := client.Create(t.Context(), f.id.Binding.BotID, spec); err != nil || replay != got {
					t.Fatal("create replaced original receipt", err)
				}
				verifier.revoked.Store(true)
				if _, err := client.Get(t.Context(), f.id.Binding.BotID, upload.ID); !errors.As(err, &status) || status.Status != http.StatusForbidden {
					t.Fatal("proof bypassed revoke", err)
				}
			}
			reader, _, err := assets.OpenSeekable(t.Context(), f.id.Binding.BotID, spec.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || string(data) != "fixture" {
				t.Fatal("original changed", err)
			}
		})
	}
}
