package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/agent/application"
	chatview "github.com/felinics/memoh/internal/agent/view"
	"github.com/felinics/memoh/internal/botdelegation"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

const (
	computerResultSession = "10000000-0000-0000-0000-000000000001"
	computerResultRun     = "10000000-0000-0000-0000-000000000002"
	computerResultTurn    = "10000000-0000-0000-0000-000000000003"
	computerResultMessage = "10000000-0000-0000-0000-000000000004"
	computerResultTeam    = "10000000-0000-0000-0000-000000000005"
)

type computerResultAnchorFixture struct {
	session sqlc.BotSession
	run     sqlc.SessionRun
	turn    sqlc.GetHistoryTurnByIDRow
	message sqlc.GetMessageByIDBySessionRow
	linked  map[string]map[string]messagepkg.AssetRef
	fail    bool
}

func (f *computerResultAnchorFixture) GetSessionByID(_ context.Context, id pgtype.UUID) (sqlc.BotSession, error) {
	if id != f.session.ID {
		return sqlc.BotSession{}, errors.New("missing")
	}
	return f.session, nil
}

func (f *computerResultAnchorFixture) GetSessionRun(_ context.Context, id pgtype.UUID) (sqlc.SessionRun, error) {
	if id.String() != computerResultRun {
		return sqlc.SessionRun{}, errors.New("missing")
	}
	return f.run, nil
}

func (f *computerResultAnchorFixture) GetHistoryTurnByID(_ context.Context, p sqlc.GetHistoryTurnByIDParams) (sqlc.GetHistoryTurnByIDRow, error) {
	if p.SessionID != f.session.ID || p.OldTurnID.String() != computerResultTurn {
		return sqlc.GetHistoryTurnByIDRow{}, errors.New("missing")
	}
	return f.turn, nil
}

func (f *computerResultAnchorFixture) GetMessageByIDBySession(_ context.Context, p sqlc.GetMessageByIDBySessionParams) (sqlc.GetMessageByIDBySessionRow, error) {
	if p.SessionID != f.session.ID || p.MessageID.String() != computerResultMessage {
		return sqlc.GetMessageByIDBySessionRow{}, errors.New("missing")
	}
	return f.message, nil
}

func (f *computerResultAnchorFixture) LinkAssets(_ context.Context, id string, refs []messagepkg.AssetRef) error {
	if f.fail {
		return errors.New("PRIVATE-database-error")
	}
	if f.linked[id] == nil {
		f.linked[id] = map[string]messagepkg.AssetRef{}
	}
	for _, ref := range refs {
		f.linked[id][ref.ContentHash] = ref
	}
	return nil
}

type computerResultAuthority struct {
	*delegatedImageAuthority
	links *computerResultAnchorFixture
}

func (*computerResultAuthority) BotTeam(context.Context, string) (string, error) {
	return computerResultTeam, nil
}

func (f *computerResultAuthority) SessionHasAsset(_ context.Context, bot, session, hash string) (bool, error) {
	_, ok := f.links.linked[computerResultMessage][hash]
	return ok && bot == imageSpawnOtherBot && session == computerResultSession, nil
}

func TestComputerResultAssociationOriginalSessionAndSpecialistBytes(t *testing.T) {
	for _, mode := range []string{"committed", "foreign-owner", "foreign-session", "foreign-bot", "foreign-run", "foreign-team", "foreign-run-user", "missing-run-user", "reset", "superseded", "wrong-anchor", "wrong-role", "unfenced", "foreign-fence", "link-failed"} {
		t.Run(mode, func(t *testing.T) {
			var data bytes.Buffer
			if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 3, 2))); err != nil {
				t.Fatal(err)
			}
			originals := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
			originals.SetRequirePersistence(true)
			original, err := originals.Ingest(t.Context(), media.IngestInput{BotID: imageSpawnOtherBot, Mime: "image/png", Reader: bytes.NewReader(data.Bytes())})
			if err != nil {
				t.Fatal(err)
			}
			input, _ := json.Marshal(map[string]any{"resume": map[string]any{"version": 1, "user_id": imageSpawnUser}})
			f := &computerResultAnchorFixture{
				session: sqlc.BotSession{ID: imageSpawnUUID(computerResultSession), BotID: imageSpawnUUID(imageSpawnOtherBot), TeamID: imageSpawnUUID(computerResultTeam), CreatedByUserID: imageSpawnUUID(imageSpawnUser)},
				run:     sqlc.SessionRun{RunID: imageSpawnUUID(computerResultRun), BotID: imageSpawnUUID(imageSpawnOtherBot), SessionID: imageSpawnUUID(computerResultSession), TeamID: imageSpawnUUID(computerResultTeam), TurnID: imageSpawnUUID(computerResultTurn), InputJson: input},
				turn:    sqlc.GetHistoryTurnByIDRow{ID: imageSpawnUUID(computerResultTurn), BotID: imageSpawnUUID(imageSpawnOtherBot), SessionID: imageSpawnUUID(computerResultSession), AssistantMessageID: imageSpawnUUID(computerResultMessage)},
				message: sqlc.GetMessageByIDBySessionRow{ID: imageSpawnUUID(computerResultMessage), BotID: imageSpawnUUID(imageSpawnOtherBot), SessionID: imageSpawnUUID(computerResultSession), Role: "assistant"},
				linked:  map[string]map[string]messagepkg.AssetRef{},
			}
			op := dbstore.ComputerOperationRecord{ID: "10000000-0000-0000-0000-000000000006", BotID: imageSpawnOtherBot, UserID: imageSpawnUser, SessionID: computerResultSession, RunID: computerResultRun}
			fence := runtimefence.Fence{BotID: op.BotID, SessionID: op.SessionID, Token: 7}
			switch mode {
			case "foreign-owner":
				f.session.CreatedByUserID = imageSpawnUUID(imageSpawnBot)
			case "foreign-session":
				op.SessionID = imageSpawnParent
				fence.SessionID = op.SessionID
			case "foreign-bot":
				op.BotID = imageSpawnBot
				fence.BotID = op.BotID
			case "foreign-run":
				f.run.SessionID = imageSpawnUUID(imageSpawnParent)
			case "foreign-team":
				f.run.TeamID = imageSpawnUUID(imageSpawnBot)
			case "foreign-run-user":
				f.run.InputJson = []byte(`{"resume":{"version":1,"user_id":"other-user"}}`)
			case "missing-run-user":
				f.run.InputJson = nil
			case "reset":
				f.run.ErrorCode = pgtype.Text{String: "history_reset", Valid: true}
			case "superseded":
				f.turn.SupersededByTurnID = imageSpawnUUID(imageSpawnBot)
			case "wrong-anchor":
				f.message.ID = imageSpawnUUID(imageSpawnParent)
			case "wrong-role":
				f.message.Role = "user"
			case "foreign-fence":
				fence.SessionID = imageSpawnParent
			case "link-failed":
				f.fail = true
			}
			ctx := runtimefence.WithContext(t.Context(), fence)
			if mode == "unfenced" {
				ctx = t.Context()
			}
			ref := computertask.DeliveryRequest{Version: 1, Direction: "upload", ContentHash: original.ContentHash, Mime: original.Mime, SizeBytes: original.SizeBytes}
			// Result reads can happen in later runs. They still upsert the one original
			// operation anchor, never a new message or the observer's latest assistant.
			for range 2 {
				err = associateComputerResult(ctx, f, f, op, []computertask.DeliveryRequest{ref, ref})
				if mode == "committed" && err != nil {
					t.Fatal(err)
				}
				if mode != "committed" && err == nil {
					t.Fatal("untrusted association accepted")
				}
			}
			if mode != "committed" {
				if len(f.linked) != 0 {
					t.Fatal("rejected result linked an attachment")
				}
				return
			}
			if len(f.linked) != 1 || len(f.linked[computerResultMessage]) != 1 {
				t.Fatal("duplicate result reads created duplicate attachment anchors")
			}
			got := f.linked[computerResultMessage][original.ContentHash]
			if got.Role != "attachment" || got.Mime != original.Mime || got.SizeBytes != int64(data.Len()) || got.Metadata["computer_operation_id"] != op.ID || got.Metadata["computer_run_id"] != op.RunID {
				t.Fatal("source association provenance changed")
			}
			projected := chatview.ConvertMessagesToUITurns([]messagepkg.Message{{ID: computerResultMessage, BotID: op.BotID, SessionID: op.SessionID, TurnID: computerResultTurn, Role: "assistant", Content: []byte(`{"role":"assistant","content":[]}`), Assets: []messagepkg.MessageAsset{{ContentHash: got.ContentHash, Role: got.Role, Mime: got.Mime, SizeBytes: got.SizeBytes, Metadata: got.Metadata}}}})
			if len(projected) != 1 || len(projected[0].Messages) != 1 || len(projected[0].Messages[0].Attachments) != 1 || projected[0].Messages[0].Attachments[0].ContentHash != original.ContentHash || projected[0].Messages[0].Attachments[0].BotID != op.BotID {
				t.Fatal("source history projection did not expose exactly one owning-Bot attachment")
			}
			authority := &computerResultAuthority{links: f, delegatedImageAuthority: &delegatedImageAuthority{active: true, hash: original.ContentHash, sessions: map[string]thread.Thread{
				computerResultSession: {ID: computerResultSession, BotID: imageSpawnOtherBot, TeamID: computerResultTeam, CreatedByUserID: imageSpawnUser, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
				imageSpawnParent:      {ID: imageSpawnParent, BotID: imageSpawnBot, TeamID: computerResultTeam, CreatedByUserID: imageSpawnUser, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
			}}}
			record := botdelegation.Record{ID: "synthetic-delegation", Identity: botdelegation.Identity{TeamID: computerResultTeam, UserID: imageSpawnUser, SourceBotID: imageSpawnOtherBot, SourceSessionID: computerResultSession, SourceRootID: computerResultSession, TargetBotID: imageSpawnBot, TargetSessionID: imageSpawnParent}, SourceSelection: "native", Payload: botdelegation.Payload{Inputs: []string{original.ContentHash}, TargetSelection: "native"}}
			service := botdelegation.NewService(&botdelegation.Authority{Permissions: authority, Sessions: authority, Assets: authority}, delegatedImageLedger{record: record}, originals, nil, 0)
			t.Cleanup(service.Close)
			host := &delegationHost{service: service}
			service.Host = host
			loader := &delegationAssetLoader{base: &gatewayAssetLoaderAdapter{media: originals}, host: host}
			expertCtx, _, err := host.prepare(t.Context(), application.ChatRequest{BotID: imageSpawnBot, UserID: imageSpawnUser, ThreadID: imageSpawnParent})
			if err != nil {
				t.Fatal(err)
			}
			// No destination-owned copy or Bot-wide alias is introduced.
			if _, err := originals.Stat(t.Context(), imageSpawnBot, original.ContentHash); !errors.Is(err, media.ErrAssetNotFound) {
				t.Fatal("source original leaked into specialist Bot")
			}
			reader, mime, err := loader.OpenForGateway(expertCtx, imageSpawnBot, original.ContentHash)
			if err != nil {
				t.Fatal(err)
			}
			received, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || mime != "image/png" || !bytes.Equal(received, data.Bytes()) {
				t.Fatal("specialist did not read exact source bytes")
			}
			// The link is not a grant: revocation and a missing declaration still deny.
			authority.active = false
			if r, _, err := loader.OpenForGateway(expertCtx, imageSpawnBot, original.ContentHash); err == nil {
				if r != nil {
					_ = r.Close()
				}
				t.Fatal("revoked specialist retained access")
			}
			authority.active = true
			if r, _, err := loader.OpenForGateway(t.Context(), imageSpawnBot, original.ContentHash); err == nil {
				if r != nil {
					_ = r.Close()
				}
				t.Fatal("unscoped specialist retained access")
			}
		})
	}
}
