//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsNativePublicationFiniteHerdrAction(t *testing.T) {
	root := nativeFixtureRoot(t, false)
	getenv := func(string) string { return root }
	data := []byte("finite native Herdr delivery")
	j := newJob(data)
	j.SnapshotID = j.DeliveryID
	client := publicationNetworkFixture(t, data)
	action := HerdrAction{Kind: "enqueue-download", DeliveryID: j.DeliveryID, Job: j}
	if _, err := InvokeHerdrNativeDelivery(t.Context(), getenv, "revision-1", nativeLimits(), action, nil, nil, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	action.Kind = "advance"
	if _, err := InvokeHerdrNativeDelivery(t.Context(), getenv, "revision-1", nativeLimits(), action, nil, client, nil, time.Now()); !errors.Is(err, ErrBinding) {
		t.Fatal("missing destination authorization accepted", err)
	}
	authorize := func(ctx context.Context, ref Reference) error {
		if ref.OperationID != j.OperationID || ref.BotID != j.BotID || ref.DeliveryID != j.DeliveryID || ref.ContentHash != j.Spec.SHA256 {
			return ErrAuthorization
		}
		return ctx.Err() // Synthetic verified task/destination, not production auth.
	}
	var status QueueStatus
	for range 2 {
		var err error
		status, err = InvokeHerdrNativeDelivery(t.Context(), getenv, "revision-1", nativeLimits(), action, nil, client, authorize, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	if !status.Associated || status.Job.Ready == nil {
		t.Fatalf("finite native handoff: %+v", status)
	}
	q := nativeQueue(t, root)
	p := nativePublication(t, q, *status.Job.Ready)
	reader, err := p.OpenPublished(t.Context(), *status.Job.Ready)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("native destination content", err)
	}
}

func TestWindowsNativePublicationRenameCollision(t *testing.T) {
	q := nativeQueue(t, nativeFixtureRoot(t, false))
	ref, source := publicationFixtureSource(t, q, []byte("verified source"))
	p := nativePublication(t, q, ref)
	path := filepath.Join(p.root.Name(), publicationKey(ref)+".object")
	p.hook = func(stage string) {
		if stage == "after_prepare" {
			if err := os.WriteFile(path, []byte("racing existing object"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := p.Publish(t.Context(), ref, source); err == nil {
		t.Fatal("native rename replaced a racing destination")
	}
	got, err := os.ReadFile(path) //nolint:gosec // Hash-derived fixture-owned target.
	if err != nil || string(got) != "racing existing object" {
		t.Fatal("collision bytes changed", err)
	}
	if _, err := p.Resolve(t.Context(), ref, true); !errors.Is(err, ErrPublicationConflict) {
		t.Fatal("unrelated object acknowledged", err)
	}
}

func TestWindowsNativePublicationRejectsLinksAndReplacement(t *testing.T) {
	for _, attack := range []string{"hardlink", "symlink", "same-content-replacement"} {
		t.Run(attack, func(t *testing.T) {
			q := nativeQueue(t, nativeFixtureRoot(t, false))
			data := []byte("private published bytes")
			ref, source := publicationFixtureSource(t, q, data)
			p := nativePublication(t, q, ref)
			if _, err := p.Publish(t.Context(), ref, source); err != nil {
				t.Fatal(err)
			}
			source.active = false
			_ = source.file.Close()
			path := filepath.Join(p.root.Name(), publicationKey(ref)+".object")
			outside := filepath.Join(q.root.Name(), "outside-publication")
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			if attack == "hardlink" {
				if err := os.Link(path, outside); err != nil {
					t.Fatal("native hardlink fixture", err)
				}
			} else {
				if err := os.Rename(path, outside); err != nil {
					t.Fatal(err)
				}
				if attack == "symlink" {
					// Required native coverage. Do not enable privileges or change ACLs
					// when a runner cannot create this isolated fixture.
					if err := os.Symlink(outside, path); err != nil {
						t.Fatal("native symlink fixture unavailable", err)
					}
				} else if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, cancel := range []bool{false, true} {
				if _, err := p.Resolve(t.Context(), ref, cancel); err == nil {
					t.Fatal("identity/link attack acknowledged", attack, cancel)
				}
			}
			if reader, err := p.OpenPublished(t.Context(), ref); err == nil {
				_ = reader.Close()
				t.Fatal("consumer escaped publication identity")
			}
			got, err := os.ReadFile(outside) //nolint:gosec // Fixed fixture-owned basename; asserts no escaped writes.
			if err != nil || !bytes.Equal(got, data) {
				t.Fatal("outside bytes changed", err)
			}
		})
	}
}

func TestWindowsNativePublicationDirectoryIdentity(t *testing.T) {
	q := nativeQueue(t, nativeFixtureRoot(t, false))
	ref, _ := publicationFixtureSource(t, q, []byte("directory binding"))
	p := nativePublication(t, q, ref)
	path := p.root.Name()
	other := filepath.Join(q.root.Name(), "previous-publications")
	if err := os.Rename(path, other); err == nil {
		t.Fatal("live publication directory moved")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, other); err != nil {
		t.Fatal(err)
	}
	if err := makeQueueDir(q.root, "publications-v1"); err != nil {
		t.Fatal(err)
	}
	locator, err := os.ReadFile(filepath.Join(other, "locator")) //nolint:gosec // Fixed names under the fixture-owned root.
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "locator"), locator, 0o600); err != nil {
		t.Fatal(err)
	}
	if replacement, err := q.OpenNativePublication(p.authorize); !errors.Is(err, ErrVersion) {
		if replacement != nil {
			_ = replacement.Close()
		}
		t.Fatal("copied locator silently accepted new directory identity", err)
	}
}

func TestWindowsNativePublicationFenceReopenAndRevocation(t *testing.T) {
	q := nativeQueue(t, nativeFixtureRoot(t, false))
	ref, source := publicationFixtureSource(t, q, []byte("revoke before rename"))
	p := nativePublication(t, q, ref)
	p.hook = func(stage string) {
		if stage == "after_prepare" {
			p.authorize = func(context.Context, Reference) error { return ErrAuthorization }
		}
	}
	if _, err := p.Publish(t.Context(), ref, source); !errors.Is(err, ErrAuthorization) {
		t.Fatal("revocation crossed rename", err)
	}
	source.active = false
	_ = source.file.Close()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p = nativePublication(t, q, ref)
	if out, err := p.Resolve(t.Context(), ref, true); err != nil || out != OutcomeCanceled {
		t.Fatal("durable cancel", out, err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p = nativePublication(t, q, ref)
	if out, err := p.Resolve(t.Context(), ref, false); err != nil || out != OutcomeCanceled {
		t.Fatal("reopened fence", out, err)
	}
	p.authorize = func(context.Context, Reference) error { return nil }
	conflict := ref
	conflict.OperationID = "different-operation"
	if _, err := p.Resolve(t.Context(), conflict, false); !errors.Is(err, ErrPublicationConflict) {
		t.Fatal("same ID rebound to another operation", err)
	}
	conflict.DeliveryID = "../outside"
	if _, err := p.Resolve(t.Context(), conflict, false); !errors.Is(err, ErrBinding) {
		t.Fatal("traversal ID accepted", err)
	}
}
