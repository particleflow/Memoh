package computertask

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

type taskInputDelegationAuthority struct {
	assets   *taskInputAssets
	identity botdelegation.Identity
	hash     string
}

func (a *taskInputDelegationAuthority) ValidateSession(context.Context, string) error {
	if a.assets.revoked {
		return botdelegation.ErrDenied
	}
	return nil
}

func (*taskInputDelegationAuthority) BotTeam(context.Context, string) (string, error) {
	return "synthetic-team", nil
}

func (a *taskInputDelegationAuthority) Allowed(_ context.Context, user, bot, _ string) (bool, error) {
	return !a.assets.revoked && user == a.identity.UserID && (bot == a.identity.SourceBotID || bot == a.identity.TargetBotID), nil
}

func (a *taskInputDelegationAuthority) Get(_ context.Context, id string) (thread.Thread, error) {
	bot := a.identity.SourceBotID
	if id == a.identity.TargetSessionID {
		bot = a.identity.TargetBotID
	} else if id != a.identity.SourceSessionID {
		return thread.Thread{}, pgx.ErrNoRows
	}
	return thread.Thread{ID: id, BotID: bot, TeamID: a.identity.TeamID, CreatedByUserID: a.identity.UserID, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}}, nil
}

func (a *taskInputDelegationAuthority) SessionHasAsset(_ context.Context, bot, session, hash string) (bool, error) {
	return bot == a.identity.SourceBotID && session == a.identity.SourceSessionID && hash == a.hash, nil
}

func (*taskInputDelegationAuthority) ResultHasAsset(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

type taskInputDelegationLedger struct {
	botdelegation.Ledger
	record botdelegation.Record
}

func (l *taskInputDelegationLedger) ForTarget(_ context.Context, bot, session string) (botdelegation.Record, error) {
	if bot != l.record.Identity.TargetBotID || session != l.record.Identity.TargetSessionID {
		return botdelegation.Record{}, pgx.ErrNoRows
	}
	return l.record, nil
}

func delegatedTaskInput(t *testing.T) (taskInputFixture, *taskInputDelegationAuthority, *taskInputDelegationLedger, InputReferenceOpener) {
	t.Helper()
	f := newTaskInputFixture(t)
	// Synthetic bytes with the incident's original size, never the user's image.
	f.data = bytes.Repeat([]byte{0x61, 0x03}, 2183938/2)
	originals := &taskInputAssets{Service: f.assets.Service}
	identity := botdelegation.Identity{TeamID: "synthetic-team", UserID: f.base.binding.UserID, SourceBotID: "synthetic-nas-bot", SourceSessionID: "synthetic-nas-session", SourceRootID: "synthetic-nas-session", SourceSelection: "native", TargetBotID: f.base.binding.BotID, TargetSessionID: f.base.binding.SessionID}
	asset, err := originals.Ingest(f.ctx, media.IngestInput{BotID: identity.SourceBotID, Mime: "image/png", Reader: bytes.NewReader(f.data)})
	if err != nil {
		t.Fatal(err)
	}
	f.asset = asset
	a := &taskInputDelegationAuthority{assets: originals, identity: identity, hash: asset.ContentHash}
	l := &taskInputDelegationLedger{record: botdelegation.Record{ID: "synthetic-input-delegation", State: "running", Identity: identity, SourceSelection: "native", Payload: botdelegation.Payload{Inputs: []string{asset.ContentHash}, TargetSelection: f.base.binding.TargetID}}}
	delegations := botdelegation.NewService(&botdelegation.Authority{Permissions: a, Sessions: a, Assets: a}, l, originals, nil, 0)
	t.Cleanup(delegations.Close)
	open := func(ctx context.Context, b toolcontext.ComputerBinding, hash string) (media.ReadSeekCloser, string, error) {
		if b != f.base.binding {
			t.Fatal("input reference lost trusted computer identity")
		}
		r, err := delegations.ForSession(ctx, b.BotID, b.UserID, b.SessionID)
		if err != nil {
			return nil, "", err
		}
		if !slices.Contains(r.Payload.Inputs, hash) {
			return nil, "", nil
		}
		reader, attachment, err := delegations.Attachments(r).OpenSeekable(ctx, hash, false)
		return reader, attachment.Mime, err
	}
	return f, a, l, open
}

func TestTaskInputDelegationSubmitPreservesOriginalAndOperationIdentity(t *testing.T) {
	f, _, _, open := delegatedTaskInput(t)
	// Reproduce production: global media + specialist Bot cannot open NAS input.
	before, err := f.service.submit(f.ctx, f.base.binding, f.request())
	if !errors.Is(err, ErrDeliveryAsset) || before.ID != "" || len(f.base.store.records) != 0 || f.peer.writes.Load() != 0 || f.base.batch.runs != 0 {
		t.Fatal("fixture did not reproduce pre-operation input identity failure", err)
	}
	f.service.WithInputReferences(open)
	result, err := f.service.submit(f.ctx, f.base.binding, f.request())
	if err != nil || result.State != "submitted" || result.Inputs == nil || result.Inputs.State != "verified" || f.peer.writes.Load() != 1 || f.base.batch.runs != 1 {
		t.Fatalf("delegated input did not reach bounded task dispatch: %+v %v", result, err)
	}
	r, d, err := f.service.load(f.ctx, f.base.binding, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Batch.Progress.Inputs
	actual, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(p.Path(p.Files[0])))) // #nosec G304 -- Test-owned physical PC peer.
	if err != nil || !bytes.Equal(actual, f.data) || p.Files[0].ContentHash != f.asset.ContentHash || p.Files[0].SizeBytes != int64(len(f.data)) || r.BotID != f.base.binding.BotID || p.Task.BotID != r.BotID || p.Task.OperationID != result.ID || p.Task.UserID != r.UserID || p.Task.SessionID != r.SessionID || p.Task.TargetID != r.TargetID || p.Task.RunID != r.RunID {
		t.Fatal("source bytes or specialist operation identity changed", err)
	}
	if _, err := f.assets.Stat(f.ctx, f.base.binding.BotID, f.asset.ContentHash); !errors.Is(err, media.ErrAssetNotFound) {
		t.Fatal("input was copied into a specialist Bot alias", err)
	}
	nas, _, err := f.assets.Open(f.ctx, f.asset.BotID, f.asset.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := io.ReadAll(nas)
	_ = nas.Close()
	if err != nil || !bytes.Equal(kept, f.data) {
		t.Fatal("NAS original changed", err)
	}
	duplicate, err := f.service.submit(f.ctx, f.base.binding, f.request())
	if err != nil || duplicate.ID != result.ID || f.peer.writes.Load() != 1 || f.base.batch.runs != 1 {
		t.Fatal("duplicate replayed input or native business work", err)
	}
	t.Logf("synthetic delegated original: bytes=%d sha256=%s file_RPC_writes=1 native_submits=1 specialist_ledger=true source_alias=false", len(actual), f.asset.ContentHash)
}

func TestTaskInputDelegationRejectsRevokedUnlinkedAndCorruptOriginal(t *testing.T) {
	for _, mode := range []string{"revoked", "unlinked", "undeclared", "revoked-with-owned-alias", "revoked-during-stream", "corrupt-original", "target-changed"} {
		t.Run(mode, func(t *testing.T) {
			f, authority, ledger, open := delegatedTaskInput(t)
			f.service.WithInputReferences(open)
			switch mode {
			case "revoked", "revoked-with-owned-alias":
				authority.assets.revoked = true
				if mode == "revoked-with-owned-alias" {
					if _, err := f.assets.Ingest(f.ctx, media.IngestInput{BotID: f.base.binding.BotID, Mime: "image/png", Reader: bytes.NewReader(f.data)}); err != nil {
						t.Fatal(err)
					}
				}
			case "unlinked":
				authority.hash = "unlinked"
			case "undeclared":
				ledger.record.Payload.Inputs = nil
			case "revoked-during-stream":
				authority.assets.revokeOnRead = true
			case "corrupt-original":
				authority.assets.corrupt = true
			case "target-changed":
				prior := f.service.inputFiles
				f.service.inputFiles = func(ctx context.Context, b toolcontext.ComputerBinding, id string, target agenvo.Target) (InputFileSystem, error) {
					files, err := prior(ctx, b, id, target)
					f.base.denied = true
					return files, err
				}
			}
			result, err := f.service.submit(f.ctx, f.base.binding, f.request())
			if !errors.Is(err, ErrDeliveryAsset) || f.base.batch.runs != 0 {
				t.Fatal("unauthorized/corrupt delegated input reached native execution", err)
			}
			if mode != "revoked-during-stream" && mode != "corrupt-original" && f.peer.writes.Load() != 0 {
				t.Fatal("denied input reached PC write")
			}
			if result.Inputs != nil {
				for _, file := range result.Inputs.Files {
					if _, err := os.Stat(filepath.Join(f.root, filepath.FromSlash(file.Path))); !os.IsNotExist(err) {
						t.Fatal("rejected stream committed PC bytes", err)
					}
				}
			}
		})
	}
}

func TestTaskInputDelegationCachedBytesRecheckLiveAuthority(t *testing.T) {
	for _, mode := range []string{"resume", "revoked", "unlinked", "target-revoked", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			f, authority, _, open := delegatedTaskInput(t)
			f.service.WithInputReferences(open)
			f.peer.drop.Store(true)
			first, err := f.service.submit(f.ctx, f.base.binding, f.request())
			if !errors.Is(err, ErrDeliveryAsset) || first.ID == "" || f.peer.writes.Load() != 1 || f.base.batch.runs != 0 {
				t.Fatal("uncertain file response fixture did not preserve cached bytes", err)
			}
			f.service.store = &resumeOperationStore{nativeTaskStore: f.base.store}
			switch mode {
			case "revoked":
				authority.assets.revoked = true
			case "unlinked":
				authority.hash = "unlinked"
			case "target-revoked":
				f.base.denied = true
			case "canceled":
				r := f.base.store.records[first.ID]
				r.CancelRequested = true
				f.base.store.records[first.ID] = r
			}
			in := f.request()
			in.ResumeOperationID = first.ID
			resumed, err := f.service.submit(f.ctx, f.base.binding, in)
			if mode == "resume" {
				if err != nil || resumed.ID != first.ID || resumed.State != "submitted" || f.base.batch.runs != 1 {
					t.Fatal("authorized input reconciliation changed operation or failed", err)
				}
			} else if err == nil || f.base.batch.runs != 0 {
				t.Fatal("cached delegated bytes bypassed revocation/cancellation", err)
			}
			if f.peer.writes.Load() != 1 || len(f.base.store.records) != 1 {
				t.Fatal("resume replayed files or created a new operation")
			}
		})
	}
}
