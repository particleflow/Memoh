package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

type resumeHTTPBotQueries struct{ *imageSpawnQueries }

func (resumeHTTPBotQueries) GetContainerByBotID(context.Context, pgtype.UUID) (sqlc.Container, error) {
	return sqlc.Container{}, pgx.ErrNoRows
}

// Ordinary owned inputs have no delegation or returned-output references.
type (
	resumeHTTPNoOutputs struct{ sqlc.DBTX }
	resumeHTTPEmptyRows struct{ pgx.Rows }
)

func (resumeHTTPNoOutputs) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return resumeHTTPEmptyRows{}, nil
}
func (resumeHTTPEmptyRows) Next() bool { return false }
func (resumeHTTPEmptyRows) Close()     {}
func (resumeHTTPEmptyRows) Err() error { return nil }

// Only storage, filesystem and native transport are peers. JWT admission,
// session ownership, ResumeNative, delegation restoration and input checks are
// the production path. No model, Windows process or external service runs.
type resumeHTTPStore struct {
	dbstore.AgenvoRuntimeStore
	dbstore.SessionTaskReader
	op                   dbstore.ComputerOperationRecord
	mounts               map[string]dbstore.BotRemoteRuntimeBindingRecord
	claims, runs, writes int
	ready, lost, revoked bool
	files                map[string][]byte
	dirs                 map[string]bool
}

func (f *resumeHTTPStore) ClaimComputerOperation(_ context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, bool, error) {
	f.claims++
	r.Revision, r.CreatedAt, r.UpdatedAt = 1, time.Now(), time.Now()
	f.op = r
	return r, true, nil
}

func (f *resumeHTTPStore) ReadComputerOperation(_ context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	if r.ID != f.op.ID || r.BotID != f.op.BotID || r.UserID != f.op.UserID || r.SessionID != f.op.SessionID {
		return r, db.ErrNotFound
	}
	return f.op, nil
}

func (f *resumeHTTPStore) SaveComputerOperation(_ context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	if r.ID != f.op.ID || r.Revision != f.op.Revision {
		return r, db.ErrNotFound
	}
	r.Revision++
	r.UpdatedAt = time.Now()
	f.op = r
	return r, nil
}

func (f *resumeHTTPStore) SaveComputerOperationWithAdmission(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	return f.SaveComputerOperation(ctx, r)
}

func (*resumeHTTPStore) PreviousComputerPiOperation(context.Context, dbstore.ComputerOperationRecord, string) (dbstore.ComputerOperationRecord, error) {
	return dbstore.ComputerOperationRecord{}, db.ErrNotFound
}

func (f *resumeHTTPStore) ListSessionTaskCandidates(context.Context, dbstore.SessionTaskFilter) ([]dbstore.SessionTaskCandidate, error) {
	return []dbstore.SessionTaskCandidate{{Type: "computer_operation", ID: f.op.ID, SessionID: f.op.SessionID, CreatedAt: f.op.CreatedAt}}, nil
}

func (*resumeHTTPStore) GetPrimaryMount(context.Context, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	return dbstore.BotRemoteRuntimeBindingRecord{}, db.ErrNotFound
}

func (f *resumeHTTPStore) GetMount(_ context.Context, bot, target string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	m, ok := f.mounts[bot+"/"+target]
	if !ok {
		return m, db.ErrNotFound
	}
	return m, nil
}

func (f *resumeHTTPStore) ListMounts(_ context.Context, bot string) ([]dbstore.BotRemoteRuntimeBindingRecord, error) {
	var out []dbstore.BotRemoteRuntimeBindingRecord
	for _, m := range f.mounts {
		if m.BotID == bot {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *resumeHTTPStore) OwnedNativeHerdrRuntime(_ context.Context, user, id string) (dbstore.UserRuntimeRecord, error) {
	for _, m := range f.mounts {
		if m.RuntimeUserID == user && m.RuntimeID == id {
			return dbstore.UserRuntimeRecord{ID: id, UserID: user, Provider: m.RuntimeProvider, ProviderConfig: m.RuntimeProviderConfig, ActivatedAt: time.Now()}, nil
		}
	}
	return dbstore.UserRuntimeRecord{}, db.ErrNotFound
}

func (*resumeHTTPStore) OwnedHTTPMCPConnectionBot(context.Context, string, string) (string, error) {
	return "", db.ErrNotFound
}

func (f *resumeHTTPStore) HasBotPermission(_ context.Context, bot, user, _ string) (bool, error) {
	return !f.revoked && bot == imageSpawnBot && user == imageSpawnUser, nil
}

func (f *resumeHTTPStore) Stat(_ context.Context, path string) (*pb.FileEntry, error) {
	if f.dirs[path] {
		return &pb.FileEntry{Path: path, IsDir: true}, nil
	}
	if data, ok := f.files[path]; ok {
		return &pb.FileEntry{Path: path, Size: int64(len(data))}, nil
	}
	return nil, bridge.ErrNotFound
}

func (f *resumeHTTPStore) Mkdir(_ context.Context, path string) error {
	f.dirs[path] = true
	return nil
}

func (f *resumeHTTPStore) ReadRaw(_ context.Context, path string) (io.ReadCloser, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, bridge.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *resumeHTTPStore) WriteRaw(_ context.Context, path string, r io.Reader) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	f.files[path] = data
	f.writes++
	return int64(len(data)), nil
}

func (f *resumeHTTPStore) ListDir(ctx context.Context, parent string, _ bool, _, _, _ int32) (*bridge.ListDirResult, error) {
	out := &bridge.ListDirResult{}
	for path := range f.dirs {
		if i := strings.LastIndex(path, `\`); i >= 0 && strings.TrimRight(path[:i], `\`) == strings.TrimRight(parent, `\`) {
			e, _ := f.Stat(ctx, path)
			out.Entries = append(out.Entries, e)
		}
	}
	for path := range f.files {
		if i := strings.LastIndex(path, `\`); i >= 0 && path[:i] == parent {
			e, _ := f.Stat(ctx, path)
			out.Entries = append(out.Entries, e)
		}
	}
	return out, nil
}

func (f *resumeHTTPStore) native(_ context.Context, _ agenvo.Scope, target agenvo.Target, method string, args map[string]any) (agenvo.Receipt, error) {
	body := map[string]any{}
	switch method {
	case "workspace.create":
		body = map[string]any{"workspace": map[string]any{"workspace_id": "w-test", "label": args["label"]}, "root_pane": map[string]string{"pane_id": "control", "terminal_id": "control-terminal", "workspace_id": "w-test"}}
	case "tab.create":
		body = map[string]any{"root_pane": map[string]string{"pane_id": "job", "terminal_id": "job-terminal", "workspace_id": "w-test"}}
	case "pane.run":
		f.runs++
		if f.lost {
			return agenvo.Receipt{}, agenvo.ErrUnknown
		}
	default:
		return agenvo.Receipt{}, agenvo.ErrUnsupported
	}
	raw, _ := json.Marshal(map[string]any{"session": target.Session, "result": body})
	return agenvo.Receipt{Execution: "accepted", RequestID: uuid.NewString(), Result: raw}, nil
}

func TestComputerInputResumeAccountHTTPRestoresOriginalContext(t *testing.T) {
	for _, owner := range []string{"own", "delegated"} {
		for _, mode := range []string{"resume", "revoked", "canceled", "unknown"} {
			t.Run(owner+"/"+mode, func(t *testing.T) {
				host, record, authority, _, asset, _ := delegationSizeFixture(t, true)
				session := authority.sessions["synthetic-target"]
				delete(authority.sessions, "synthetic-target")
				session.ID = imageSpawnParent
				authority.sessions[session.ID] = session
				record.Identity.TargetSessionID = session.ID
				targetID, runtimeID := uuid.NewString(), uuid.NewString()
				profile := agenvo.NativeHerdrProfile{InstanceID: "synthetic-herdr", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{Provider: "synthetic", Model: "synthetic-model", ConfigDir: `C:\Synthetic\Pi`, ExecutionMode: "pi-json-v1"}}
				config, _ := json.Marshal(map[string]any{"herdr": profile})
				f := &resumeHTTPStore{mounts: map[string]dbstore.BotRemoteRuntimeBindingRecord{}, files: map[string][]byte{}, dirs: map[string]bool{profile.WorkDir: true}}
				sourceMount := uuid.NewString()
				for bot, mount := range map[string]string{imageSpawnBot: targetID, imageSpawnOtherBot: sourceMount} {
					f.mounts[bot+"/"+mount] = dbstore.BotRemoteRuntimeBindingRecord{ID: mount, BotID: bot, RuntimeID: runtimeID, RuntimeUserID: "synthetic-owner", BotOwnerUserID: "synthetic-owner", RuntimeProvider: "memoh", RuntimeProviderConfig: config}
				}
				target, err := agenvo.DecodeRuntimeTarget("memoh", runtimeID, config)
				if err != nil {
					t.Fatal(err)
				}
				record.Payload.TargetSelection = targetID
				record.Payload.Devices = []botdelegation.DevicePair{{SourceMountID: sourceMount, TargetMountID: targetID, RuntimeID: runtimeID, Fingerprint: agenvo.TargetFingerprint(target)}}
				host.service.Authority.Mounts = f
				host.service.Ledger = delegatedImageLedger{record: record}
				if owner == "own" {
					original, _, e := host.media.Open(t.Context(), asset.BotID, asset.ContentHash)
					if e != nil {
						t.Fatal(e)
					}
					asset, e = host.media.Ingest(t.Context(), media.IngestInput{BotID: imageSpawnBot, Mime: asset.Mime, Reader: original})
					_ = original.Close()
					if e != nil {
						t.Fatal(e)
					}
					record.Payload.Inputs = nil
					// This is an ordinary owned session, not a delegated target.
					record.Identity.TargetSessionID = "another-specialist-session"
					host.service.Ledger = delegatedImageLedger{record: record}
					host.store = postgresstore.NewWithQueries(sqlc.New(resumeHTTPNoOutputs{}))
				}
				loader := &delegationAssetLoader{host: host}
				relay := agenvo.NewService(f, f, f.native)
				service := computertask.NewService(f, f, relay, host.media, func(ctx context.Context, b toolcontext.ComputerBinding, id, _, _ string) error {
					return toolcontext.CheckDelegationTarget(ctx, b.BotID, id, bots.PermissionWorkspaceExec)
				}).WithInputReferences(loader.openComputerInput).WithInputFiles(func(context.Context, toolcontext.ComputerBinding, string, agenvo.Target) (computertask.InputFileSystem, error) {
					if !f.ready {
						return nil, computertask.ErrDeliveryAsset
					}
					return f, nil
				})
				source := computertask.NewSource(service)
				source.SetResumeContext(computerTaskResumeContext(host.service))
				binding := toolcontext.ComputerBinding{BotID: imageSpawnBot, UserID: imageSpawnUser, SessionID: session.ID, TargetID: targetID, RunID: "original-terminal-run"}
				base := t.Context()
				if owner == "delegated" {
					base = host.service.BindScope(base, record)
				}
				ctx, e := toolcontext.WithComputerBinding(base, binding)
				if e != nil {
					t.Fatal(e)
				}
				request := map[string]any{"kind": "herdr_pi_task", "task": "original synthetic image input", "work_dir": ".", "target_id": targetID, "inputs": []string{asset.ContentHash}}
				first, e := source.CallTool(ctx, mcp.ToolSessionContext{BotID: binding.BotID, UserID: binding.UserID, SessionID: binding.SessionID, RunID: binding.RunID, RunContext: ctx}, "computer_task_submit", request)
				if e != nil || first["isError"] != true || f.op.ID == "" || f.runs != 0 || f.writes != 0 {
					t.Fatalf("interrupted input fixture: err=%v operation=%s", e, f.op.ID)
				}
				originalID, originalRun := f.op.ID, f.op.RunID
				f.ready = true
				f.lost = mode == "unknown"
				if mode == "revoked" {
					authority.active = false
					if owner == "own" {
						f.revoked = true
					}
				}
				if mode == "canceled" {
					f.op.CancelRequested = true
				}
				queries := &imageSpawnQueries{}
				queries.allowed.Store(true)
				account := accounts.NewService(nil, imageSpawnAccounts{})
				h := handlers.NewSessionTaskHandler(sessiontask.NewService(f, authority, nil, relay, "synthetic-secret"), bots.NewService(nil, resumeHTTPBotQueries{queries}), account, source)
				echoServer := echo.New()
				echoServer.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.Default())
				echoServer.Use(auth.JWTMiddleware("synthetic-secret", func(echo.Context) bool { return false }, account.ValidateSession))
				h.Register(echoServer)
				token, _, e := auth.GenerateToken(imageSpawnUser, "synthetic-secret", time.Minute)
				if e != nil {
					t.Fatal(e)
				}
				body, _ := json.Marshal(request)
				for attempt := 0; attempt < 2; attempt++ {
					req := httptest.NewRequest(http.MethodPost, "/bots/"+binding.BotID+"/sessions/"+binding.SessionID+"/tasks/computer_operation/"+originalID+"/resume", bytes.NewReader(body))
					req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
					req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
					req.Header.Set("X-Memoh-Run-Id", "forged-run")
					rec := httptest.NewRecorder()
					echoServer.ServeHTTP(rec, req)
					if mode == "revoked" {
						if rec.Code != http.StatusNotFound {
							t.Fatalf("revoked HTTP status=%d body=%s", rec.Code, rec.Body.String())
						}
						continue
					}
					var out handlers.NativeTaskResumeResponse
					if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.OperationID != originalID || out.AutomaticRetry {
						t.Fatalf("HTTP status=%d body=%s", rec.Code, rec.Body.String())
					}
					if mode == "resume" && out.Outcome != "submitted" {
						t.Fatalf("legal input resume failed: %s", rec.Body.String())
					}
					if mode != "resume" && out.Outcome != "unknown" {
						t.Fatalf("uncertainty/cancel changed: %s", rec.Body.String())
					}
				}
				want := 1
				if mode == "revoked" || mode == "canceled" {
					want = 0
				}
				if f.runs != want || f.writes != want || f.claims != 1 || f.op.ID != originalID || f.op.RunID != originalRun {
					t.Fatalf("resume changed identity/replayed work: runs=%d writes=%d claims=%d", f.runs, f.writes, f.claims)
				}
			})
		}
	}
}
