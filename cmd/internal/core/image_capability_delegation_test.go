package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/agent/application"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/imagecapability"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type delegatedImageAuthority struct {
	active   bool
	hash     string
	sessions map[string]thread.Thread
}

func (f *delegatedImageAuthority) ValidateSession(context.Context, string) error {
	if !f.active {
		return botdelegation.ErrDenied
	}
	return nil
}

func (*delegatedImageAuthority) BotTeam(context.Context, string) (string, error) {
	return "synthetic-team", nil
}

func (f *delegatedImageAuthority) Allowed(_ context.Context, user, bot, _ string) (bool, error) {
	return f.active && user == imageSpawnUser && (bot == imageSpawnBot || bot == imageSpawnOtherBot), nil
}

func (f *delegatedImageAuthority) HasBotPermission(ctx context.Context, bot, user, permission string) (bool, error) {
	return f.Allowed(ctx, user, bot, permission)
}

func (f *delegatedImageAuthority) Get(_ context.Context, id string) (thread.Thread, error) {
	row, ok := f.sessions[id]
	if !ok {
		return thread.Thread{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *delegatedImageAuthority) SessionHasAsset(_ context.Context, bot, session, hash string) (bool, error) {
	return bot == imageSpawnOtherBot && session == "synthetic-source" && hash == f.hash, nil
}

func (*delegatedImageAuthority) ResultHasAsset(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

type delegatedImageLedger struct {
	botdelegation.Ledger
	record botdelegation.Record
}

func (f delegatedImageLedger) ForTarget(_ context.Context, bot, session string) (botdelegation.Record, error) {
	if bot != f.record.Identity.TargetBotID || session != f.record.Identity.TargetSessionID {
		return botdelegation.Record{}, pgx.ErrNoRows
	}
	return f.record, nil
}

type delegatedImageOriginals struct {
	*media.Service
	onOpen       func()
	readComplete atomic.Bool
}

func (s *delegatedImageOriginals) Open(ctx context.Context, bot, hash string) (io.ReadCloser, media.Asset, error) {
	reader, asset, err := s.Service.Open(ctx, bot, hash)
	if err != nil {
		return nil, asset, err
	}
	if s.onOpen != nil {
		s.onOpen()
	}
	return &delegatedImageReader{ReadCloser: reader, complete: &s.readComplete}, asset, nil
}

type delegatedImageReader struct {
	io.ReadCloser
	complete *atomic.Bool
}

func (r *delegatedImageReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err == io.EOF {
		r.complete.Store(true)
	}
	return n, err
}

func TestDelegatedImageReferenceUsesScopedOriginal(t *testing.T) {
	for _, mode := range []string{"success", "unknown", "undeclared", "revoked", "missing-scope", "source-unlinked", "revoked-on-open", "subagent-scope"} {
		t.Run(mode, func(t *testing.T) {
			var input bytes.Buffer
			if err := png.Encode(&input, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
				t.Fatal(err)
			}
			original := input.Bytes()
			originalHash := sha256.Sum256(original)
			hash := hex.EncodeToString(originalHash[:])
			store := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
			store.SetRequirePersistence(true)
			if _, err := store.Ingest(t.Context(), media.IngestInput{BotID: imageSpawnOtherBot, Mime: "image/png", Reader: bytes.NewReader(original)}); err != nil {
				t.Fatal(err)
			}
			f := &delegatedImageAuthority{active: true, hash: hash, sessions: map[string]thread.Thread{
				"synthetic-source": {ID: "synthetic-source", BotID: imageSpawnOtherBot, TeamID: "synthetic-team", CreatedByUserID: imageSpawnUser, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
				imageSpawnParent:   {ID: imageSpawnParent, BotID: imageSpawnBot, TeamID: "synthetic-team", CreatedByUserID: imageSpawnUser, Type: thread.TypeChat, Metadata: map[string]any{"workspace_target_id": "native"}},
			}}
			r := botdelegation.Record{ID: "synthetic-delegation", Identity: botdelegation.Identity{TeamID: "synthetic-team", UserID: imageSpawnUser, SourceBotID: imageSpawnOtherBot, SourceSessionID: "synthetic-source", SourceRootID: "synthetic-source", TargetBotID: imageSpawnBot, TargetSessionID: imageSpawnParent}, SourceSelection: "native", Payload: botdelegation.Payload{Inputs: []string{hash}, TargetSelection: "native"}}
			if mode == "undeclared" {
				r.Payload.Inputs = nil
			}
			authority := &botdelegation.Authority{Permissions: f, Sessions: f, Assets: f}
			originals := &delegatedImageOriginals{Service: store}
			if mode == "revoked-on-open" {
				originals.onOpen = func() { f.active = false }
			}
			delegations := botdelegation.NewService(authority, delegatedImageLedger{record: r}, originals, nil, 0)
			t.Cleanup(delegations.Close)
			host := &delegationHost{service: delegations}
			delegations.Host = host
			sessionID := imageSpawnParent
			if mode == "subagent-scope" {
				sessionID = "synthetic-specialist-child"
				f.sessions[sessionID] = thread.Thread{ID: sessionID, BotID: imageSpawnBot, TeamID: "synthetic-team", CreatedByUserID: imageSpawnUser, Type: thread.TypeSubagent, ParentThreadID: imageSpawnParent}
			}
			ctx, _, err := host.prepare(t.Context(), application.ChatRequest{BotID: imageSpawnBot, UserID: imageSpawnUser, ThreadID: sessionID})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "revoked" {
				f.active = false
			}
			if mode == "source-unlinked" {
				f.hash = "not-linked"
			}
			if mode == "missing-scope" {
				ctx = t.Context()
			}
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				posts.Add(1)
				if !originals.readComplete.Load() {
					t.Error("provider dispatched before full authorized input read")
				}
				if _, err := store.Stat(req.Context(), imageSpawnBot, hash); !errors.Is(err, media.ErrAssetNotFound) {
					t.Error("delegated input was copied or aliased into target Bot before output")
				}
				if req.Method != http.MethodPost || req.URL.Path != "/v1/images/edits" {
					t.Error("unexpected image dispatch")
				}
				var wire struct {
					Images []struct {
						URL string `json:"image_url"`
					} `json:"images"`
				}
				if json.NewDecoder(req.Body).Decode(&wire) != nil || len(wire.Images) != 1 || wire.Images[0].URL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(original) {
					t.Error("delegated original bytes changed")
				}
				if mode == "unknown" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(original)}}})
			}))
			t.Cleanup(server.Close)
			cfg, _ := json.Marshal(map[string]any{"base_url": server.URL + "/v1", "images_api": map[string]any{"protocol": "openai-images", "edits": true}})
			q := &imageSpawnQueries{model: sqlc.Model{ID: imageSpawnUUID(imageSpawnModel), ProviderID: imageSpawnUUID(imageSpawnProvider), ModelID: "synthetic-model", Enable: true, Config: []byte(`{"compatibilities":["image-output"]}`)}, provider: sqlc.Provider{ID: imageSpawnUUID(imageSpawnProvider), ClientType: "openai-completions", Enable: true, Config: cfg}}
			log := slog.New(slog.DiscardHandler)
			provider := imagecapability.New(nil, settings.NewService(log, q, nil, nil), models.NewService(log, q), q, store, imageCapabilityAuthorizer(f, f.Get))
			bindImageCapabilityReferences(provider, store, delegations)
			session := tools.SessionContext{BotID: imageSpawnBot, UserID: imageSpawnUser, SessionID: sessionID}
			definitions, err := provider.Tools(ctx, session)
			if err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal(map[string]any{"prompt": "synthetic reference transformation", "images": []string{hash}})
			result, err := toolexec.ExecuteTools(ctx, []sdk.ToolCall{{ToolCallID: "synthetic-delegated-image", ToolName: "edit_image", Input: sdk.ParseToolArguments(string(args))}}, toolexec.ToolExecOptions{Tools: definitions})
			if err != nil || len(result.Results) != 1 {
				t.Fatalf("tool execution: %v", err)
			}
			value := result.Results[0]
			if mode == "success" || mode == "unknown" || mode == "subagent-scope" {
				if value.IsError || posts.Load() != 1 {
					t.Fatalf("mode=%s posts=%d result=%s", mode, posts.Load(), value.Result.String())
				}
				if mode == "unknown" {
					if !strings.Contains(value.Result.String(), `"state":"unknown"`) || !strings.Contains(value.Result.String(), `"retryable":false`) {
						t.Fatal("unknown became retryable")
					}
				} else {
					if !strings.Contains(value.Result.String(), `"state":"completed"`) {
						t.Fatal("no completed persisted result")
					}
					reader, _, err := store.Open(ctx, imageSpawnBot, hash)
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = reader.Close() }()
					got, err := io.ReadAll(reader)
					if err != nil || !bytes.Equal(got, original) {
						t.Fatal("persisted result differs")
					}
				}
			} else if !value.IsError || posts.Load() != 0 {
				t.Fatalf("scope failure dispatched: posts=%d result=%s", posts.Load(), value.Result.String())
			}
			if mode != "success" && mode != "subagent-scope" {
				if _, err := store.Stat(t.Context(), imageSpawnBot, hash); !errors.Is(err, media.ErrAssetNotFound) {
					t.Fatalf("input acquired target alias: %v", err)
				}
			}
		})
	}
}
