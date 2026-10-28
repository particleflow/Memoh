package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestComputerInputDelegationUsesOriginalAndTrustedConversation(t *testing.T) {
	for _, mode := range []string{"specialist", "child", "revoked", "unlinked", "wrong-user", "wrong-bot", "wrong-session", "wrong-target", "wrong-run", "missing-binding", "missing-delegation-scope", "revoked-on-open"} {
		t.Run(mode, func(t *testing.T) {
			host, record, authority, originals, asset, size := delegationSizeFixture(t, true)
			loader := &delegationAssetLoader{host: host}
			b := toolcontext.ComputerBinding{BotID: record.Identity.TargetBotID, UserID: record.Identity.UserID, SessionID: record.Identity.TargetSessionID, TargetID: "synthetic-windows", RunID: "synthetic-run"}
			if mode == "child" {
				authority.sessions["synthetic-child"] = thread.Thread{ID: "synthetic-child", BotID: b.BotID, CreatedByUserID: b.UserID, TeamID: record.Identity.TeamID, Type: thread.TypeSubagent, ParentThreadID: b.SessionID}
				b.SessionID = "synthetic-child"
			}
			base := t.Context()
			if mode != "missing-delegation-scope" {
				base = host.service.BindScope(base, record)
			}
			ctx, err := toolcontext.WithComputerBinding(base, b)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "revoked":
				authority.active = false
			case "unlinked":
				authority.hash = "unlinked"
			case "wrong-user":
				b.UserID = "another-user"
			case "wrong-bot":
				b.BotID = record.Identity.SourceBotID
			case "wrong-session":
				b.SessionID = "another-session"
			case "wrong-target":
				b.TargetID = "another-computer"
			case "wrong-run":
				b.RunID = "another-run"
			case "missing-binding":
				ctx = t.Context()
			case "revoked-on-open":
				originals.onOpen = func() { authority.active = false }
			}
			reader, mime, err := loader.openComputerInput(ctx, b, asset.ContentHash)
			if mode != "specialist" && mode != "child" && mode != "revoked-on-open" {
				if reader != nil || err == nil || originals.opens != 0 {
					t.Fatalf("unauthorized reference reached original: reader=%v err=%v opens=%d", reader != nil, err, originals.opens)
				}
				return
			}
			if err != nil || reader == nil {
				t.Fatalf("authorized delegated input unavailable: %v", err)
			}
			defer func() { _ = reader.Close() }()
			if mode == "revoked-on-open" {
				if _, err := reader.Seek(0, io.SeekEnd); err == nil {
					t.Fatal("revocation during open allowed size verification")
				}
				if _, err := reader.Read(make([]byte, 1)); err == nil {
					t.Fatal("revocation during open allowed bytes")
				}
				return
			}
			length, err := reader.Seek(0, io.SeekEnd)
			if err != nil || length != size || mime != "image/png" {
				t.Fatalf("original metadata changed: size=%d mime=%s err=%v", length, mime, err)
			}
			if _, err := reader.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			sum := sha256.Sum256(data)
			if err != nil || int64(len(data)) != size || hex.EncodeToString(sum[:]) != asset.ContentHash {
				t.Fatal("original bytes/hash changed", err)
			}
			if _, err := host.media.Stat(ctx, b.BotID, asset.ContentHash); !errors.Is(err, media.ErrAssetNotFound) {
				t.Fatal("delegated input acquired a target Bot alias", err)
			}
			// Keep checking already-open streams, not just the first download.
			authority.active = false
			if _, err := reader.Seek(0, io.SeekStart); err == nil {
				t.Fatal("revoked seek succeeded")
			}
			if _, err := io.Copy(io.Discard, reader); err == nil {
				t.Fatal("revoked read succeeded")
			}
		})
	}
}

func TestComputerInputDelegationDoesNotInferGrantFromHash(t *testing.T) {
	host, record, _, originals, asset, _ := delegationSizeFixture(t, true)
	loader := &delegationAssetLoader{host: host}
	b := toolcontext.ComputerBinding{BotID: record.Identity.TargetBotID, UserID: record.Identity.UserID, SessionID: record.Identity.TargetSessionID, TargetID: "synthetic-windows"}
	ctx, err := toolcontext.WithComputerBinding(context.WithValue(host.service.BindScope(t.Context(), record), delegationMediaKey{}, delegationMediaIdentity{bot: b.BotID, user: b.UserID, session: b.SessionID}), b)
	if err != nil {
		t.Fatal(err)
	}
	undeclared, err := host.media.Ingest(ctx, media.IngestInput{BotID: asset.BotID, Mime: "image/png", Reader: bytes.NewReader([]byte("not declared in this handoff"))})
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := loader.openComputerInput(ctx, b, undeclared.ContentHash)
	if reader != nil || err != nil || originals.opens != 0 {
		t.Fatalf("undeclared source hash acquired delegation access: %v", err)
	}
}
