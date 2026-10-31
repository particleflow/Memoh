package userruntime_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/userruntime"
	"github.com/felinics/memoh/internal/workspace"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

const (
	serviceTestRuntimeID = "11111111-1111-4111-8111-111111111111"
	executionBot         = "22222222-2222-4222-8222-222222222222"
	executionTarget      = "33333333-3333-4333-8333-333333333333"
	storageTestShare     = `\\synthetic-nas\existing-share\workspaces`
)

type executionContractRegistry struct {
	dbstore.AgenvoRuntimeStore
	dbstore.UserRuntimeStore
	dbstore.BotRemoteRuntimeBindingStore
	raw       dbstore.JSON
	operation dbstore.ComputerOperationRecord
	key       string
}

func (r *executionContractRegistry) GetUserRuntimeByAPIToken(ctx context.Context, _ string) (dbstore.UserRuntimeRecord, error) {
	return r.OwnedNativeHerdrRuntime(ctx, "owner", serviceTestRuntimeID)
}

func (r *executionContractRegistry) ActivateUserRuntime(ctx context.Context, _, _ string) (dbstore.UserRuntimeRecord, error) {
	return r.OwnedNativeHerdrRuntime(ctx, "owner", serviceTestRuntimeID)
}

func (r *executionContractRegistry) OwnedNativeHerdrRuntime(_ context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	if owner != "owner" || id != serviceTestRuntimeID {
		return dbstore.UserRuntimeRecord{}, db.ErrNotFound
	}
	return dbstore.UserRuntimeRecord{ID: id, UserID: owner, Provider: "memoh", ProviderConfig: r.raw, APIToken: r.key, ActivatedAt: time.Now()}, nil
}

func (r *executionContractRegistry) ConfigureNativeHerdrRuntime(ctx context.Context, owner, id string, before, after dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	if string(before) != string(r.raw) {
		return dbstore.UserRuntimeRecord{}, db.ErrNotFound
	}
	r.raw = after
	return r.OwnedNativeHerdrRuntime(ctx, owner, id)
}

type executionShareRPC struct {
	pb.UnimplementedContainerServiceServer
	root        string
	unavailable *atomic.Bool
	reads       *atomic.Int64
}

func (r executionShareRPC) Stat(_ context.Context, in *pb.StatRequest) (*pb.StatResponse, error) {
	r.reads.Add(1)
	if r.unavailable.Load() {
		return nil, status.Error(codes.Unavailable, "synthetic share disconnected")
	}
	if in.Path != storageTestShare && !strings.HasPrefix(in.Path, storageTestShare+`\`) {
		return nil, status.Error(codes.PermissionDenied, "outside synthetic share")
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(in.Path, storageTestShare), `\`)
	v, e := os.Stat(filepath.Join(r.root, filepath.FromSlash(strings.ReplaceAll(rel, `\`, "/"))))
	if e != nil {
		return nil, status.Error(codes.NotFound, "synthetic directory absent")
	}
	return &pb.StatResponse{Entry: &pb.FileEntry{IsDir: v.IsDir()}}, nil
}

func executionStorageFixture(t *testing.T) (*userruntime.Service, *executionContractRegistry, *atomic.Bool, *atomic.Int64) {
	t.Helper()
	root := t.TempDir()
	r := &executionContractRegistry{raw: []byte(`{}`), key: "mrk_" + strings.Repeat("a", 64)}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	offline := &atomic.Bool{}
	reads := &atomic.Int64{}
	pb.RegisterContainerServiceServer(server, executionShareRPC{root: root, unavailable: offline, reads: reads})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///synthetic-UNC-source-contract", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	svc := userruntime.NewService(r, userruntime.NewHub(nil))
	svc.SetWorkspaceStorage(root, storageTestShare)
	err = svc.ActivateConnection(t.Context(), r.key, serviceTestRuntimeID, userruntime.HandshakeInfo{Version: 1, OS: "win32", Capabilities: []string{userruntime.CapabilityFS, userruntime.CapabilityExec, userruntime.CapabilityHostFS}}, &userruntime.Connection{ConnectionID: "synthetic-execution-generation", Client: bridge.NewClientFromConn(conn)}, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return svc, r, offline, reads
}

func (r *executionContractRegistry) GetMount(context.Context, string, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	return dbstore.BotRemoteRuntimeBindingRecord{ID: executionTarget, BotID: executionBot, RuntimeID: serviceTestRuntimeID, RuntimeProvider: "memoh", RuntimeUserID: "owner", BotOwnerUserID: "owner", RuntimeProviderConfig: r.raw}, nil
}

func (r *executionContractRegistry) ListMounts(ctx context.Context, bot string) ([]dbstore.BotRemoteRuntimeBindingRecord, error) {
	m, e := r.GetMount(ctx, bot, executionTarget)
	return []dbstore.BotRemoteRuntimeBindingRecord{m}, e
}

func (r *executionContractRegistry) ClaimComputerOperation(_ context.Context, v dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, bool, error) {
	v.State = "dispatching"
	r.operation = v
	return v, true, nil
}

func (r *executionContractRegistry) SaveComputerOperation(_ context.Context, v dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	if r.operation.Revision != v.Revision {
		return v, db.ErrNotFound
	}
	v.Revision++
	r.operation = v
	return v, nil
}

func (r *executionContractRegistry) ReadComputerOperation(_ context.Context, v dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	if v.ID != r.operation.ID || v.BotID != r.operation.BotID || v.SessionID != r.operation.SessionID || v.UserID != r.operation.UserID {
		return v, db.ErrNotFound
	}
	return r.operation, nil
}

func (*executionContractRegistry) PreviousComputerPiOperation(context.Context, dbstore.ComputerOperationRecord, string) (dbstore.ComputerOperationRecord, error) {
	return dbstore.ComputerOperationRecord{}, db.ErrNotFound
}

type executionContractPermission struct{}

func (executionContractPermission) HasBotPermission(context.Context, string, string, string) (bool, error) {
	return true, nil
}

// Exercise the actual owned selection, trusted tool Source and native adapter.
// The controlled native call stops at workspace.create; no Pi input is sent.
// This is not a Windows, SMB, production or producer acceptance test.
type executionProbeOptions struct {
	sessionDirectory string
	offline          bool
}

func selectedNASSubmission(t *testing.T, configuredRoot, requested string, options ...executionProbeOptions) (string, string, string, int, error) {
	t.Helper()
	svc, store, offline, reads := executionStorageFixture(t)
	var option executionProbeOptions
	if len(options) > 0 {
		option = options[0]
	}
	profile := agenvo.NativeHerdrProfile{
		InstanceID: "synthetic", Session: "synthetic", WorkDir: configuredRoot,
		BashPath: `C:\Synthetic\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`,
		HerdrConfigRoot: `C:\Synthetic\herdr`,
		Agent:           agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\Pi`},
		Producer:        agenvo.ProducerLocator{ProgramPath: `C:\Synthetic\transferqueue.exe`, ConfigDir: `C:\Synthetic\transfer-config`, StateDir: `C:\Synthetic\transfer-state`},
	}
	var err error
	store.raw, err = json.Marshal(map[string]any{"herdr": profile})
	if err != nil {
		t.Fatal(err)
	}
	connection, ok := svc.Connection(serviceTestRuntimeID)
	if !ok {
		t.Fatal("missing controlled Runtime")
	}
	connection.Info.WorkspaceBase = profile.WorkDir
	if err = svc.SelectWorkspaceDirectory(t.Context(), "owner", serviceTestRuntimeID, "project"); err != nil {
		t.Fatal(err)
	}
	selected, err := svc.WorkspaceDirectories(t.Context(), "owner", serviceTestRuntimeID, "")
	if err != nil {
		t.Fatal(err)
	}
	registry := store
	relay := agenvo.NewService(registry, executionContractPermission{}, nil)
	calls, actualCWD := 0, ""
	relay.SetNativeCaller(func(_ context.Context, _ agenvo.Scope, _ agenvo.Target, method string, args map[string]any) (agenvo.Receipt, error) {
		calls++
		if method != "workspace.create" {
			t.Fatal("unexpected native mutation", method)
		}
		actualCWD, _ = args["cwd"].(string)
		return agenvo.Receipt{}, agenvo.ErrUnknown
	})
	manager := workspace.NewRemoteWorkspaceService(registry, svc)
	taskService := computertask.NewService(registry, registry, relay, nil, func(ctx context.Context, _ toolcontext.ComputerBinding, _ string, _ string, _ string) error {
		_, err := manager.ResolveMount(ctx, executionBot, executionTarget)
		return err
	}).WithDefaultWorkDir(func(ctx context.Context, _ toolcontext.ComputerBinding, _ string, target agenvo.Target) (string, error) {
		selected, err := svc.SelectedWorkspaceDirectory(ctx, "owner", serviceTestRuntimeID)
		if err != nil || selected == "" {
			return selected, err
		}
		return selected, agenvo.ValidateWorkDir(target.WorkDir, selected)
	})
	binding := toolcontext.ComputerBinding{BotID: executionBot, UserID: "owner", SessionID: "synthetic-session", TargetID: executionTarget, RunID: "synthetic-run"}
	ctx, bindErr := toolcontext.WithComputerBinding(t.Context(), binding)
	if bindErr != nil {
		t.Fatal(bindErr)
	}
	session := mcp.ToolSessionContext{BotID: binding.BotID, UserID: binding.UserID, SessionID: binding.SessionID, RunID: binding.RunID, RuntimeActive: true, RunContext: ctx}
	session.WorkdirPath = option.sessionDirectory
	offline.Store(option.offline)
	reads.Store(0)
	args := map[string]any{"kind": "herdr_pi_task", "task": "synthetic task"}
	if requested != "" {
		args["work_dir"] = requested
	}
	response, callErr := computertask.NewSource(taskService).CallTool(t.Context(), session, "computer_task_submit", args)
	if callErr != nil {
		if calls == 0 && registry.operation.ID != "" {
			t.Fatal("preflight failure reserved an operation")
		}
		return selected.WindowsPath, actualCWD, connection.Info.WorkspaceBase, calls, callErr
	}
	err = nil
	if response["isError"] == true {
		err = agenvo.ErrUnknown
	}
	if calls == 0 && response["isError"] == true {
		raw, _ := json.Marshal(response)
		if !strings.Contains(string(raw), "work_dir must stay within") {
			t.Fatalf("unexpected pre-dispatch error: %s", raw)
		}
		err = agenvo.ErrUnsupported
	}

	explicitPath := option.sessionDirectory
	if requested != "" && requested != "." {
		explicitPath = requested
	}
	fileCtx := toolcontext.WithExplicitWorkdir(t.Context(), explicitPath)
	if calls > 0 && requested == "" && option.sessionDirectory == "" {
		var details computertask.Details
		if json.Unmarshal(registry.operation.Details, &details) != nil || details.DefaultWorkDir != selected.WindowsPath {
			t.Fatal("actual selected cwd was not retained on original operation")
		}
	}
	if calls > 0 {
		var details computertask.Details
		if json.Unmarshal(registry.operation.Details, &details) != nil || details.TargetSnapshot == nil || details.TargetSnapshot.WorkDir != profile.WorkDir || details.TargetSnapshot.Producer != profile.Producer || agenvo.TargetFingerprint(*details.TargetSnapshot) != registry.operation.TargetFingerprint {
			t.Fatal("original root/producer directory pair was not pinned with target fingerprint")
		}
	}
	resolved, resolveErr := workspace.NewRemoteWorkspaceService(registry, svc).ResolveMount(fileCtx, executionBot, executionTarget)
	if resolveErr != nil {
		t.Fatal("file workspace resolution", resolveErr)
	}
	expectedFileBase := selected.WindowsPath
	if explicitPath != "" {
		expectedFileBase = profile.WorkDir
	}
	if resolved.Info.DefaultWorkDir != expectedFileBase {
		t.Fatalf("file default=%q want=%q", resolved.Info.DefaultWorkDir, expectedFileBase)
	}
	if connection.Info.WorkspaceBase != profile.WorkDir {
		t.Fatal("default projection rewrote Runtime handshake")
	}

	if option.offline && reads.Load() != 0 {
		t.Fatal("explicit local request probed unrelated NAS share")
	}
	return selected.WindowsPath, actualCWD, connection.Info.WorkspaceBase, calls, err
}

func TestNASDefaultUnalignedTargetDoesNotDispatch(t *testing.T) {
	want, cwd, base, calls, err := selectedNASSubmission(t, `C:\Synthetic`, "")
	if !errors.Is(err, agenvo.ErrUnsupported) || calls != 0 || cwd != "" {
		t.Fatalf("unaligned new default crossed root guard: calls=%d err=%v", calls, err)
	}
	t.Logf("saved NAS=%q; unchanged local handshake=%q; no operation was dispatched", want, base)
}

func TestNASDefaultTaskSourceAndFileProjection(t *testing.T) {
	t.Run("owner configured NAS root and relative child reaches original native boundary", func(t *testing.T) {
		root := storageTestShare + `\` + serviceTestRuntimeID
		want, cwd, base, calls, err := selectedNASSubmission(t, root, "")
		if !errors.Is(err, agenvo.ErrUnknown) || calls != 1 || cwd != want || base != root {
			t.Fatalf("existing UNC contract: cwd=%q want=%q base=%q calls=%d err=%v", cwd, want, base, calls, err)
		}
	})
	t.Run("model period uses the saved NAS default", func(t *testing.T) {
		root := storageTestShare + `\` + serviceTestRuntimeID
		want, cwd, _, calls, err := selectedNASSubmission(t, root, ".")
		if !errors.Is(err, agenvo.ErrUnknown) || calls != 1 || cwd != want {
			t.Fatalf("period bypassed selected default: cwd=%q want=%q calls=%d err=%v", cwd, want, calls, err)
		}
	})
	t.Run("NAS path cannot escape an existing local execution root", func(t *testing.T) {
		requested := storageTestShare + `\` + serviceTestRuntimeID + `\project`
		_, cwd, base, calls, err := selectedNASSubmission(t, `C:\Synthetic`, requested)
		if !errors.Is(err, agenvo.ErrUnsupported) || calls != 0 || cwd != "" || base != `C:\Synthetic` {
			t.Fatalf("existing root guard: calls=%d err=%v", calls, err)
		}
	})
}

func TestNASDefaultExplicitLocalAndUnavailable(t *testing.T) {
	t.Run("explicit local request survives unrelated NAS disconnect", func(t *testing.T) {
		_, cwd, _, calls, err := selectedNASSubmission(t, `C:\Synthetic`, `C:\Synthetic`, executionProbeOptions{offline: true})
		if !errors.Is(err, agenvo.ErrUnknown) || calls != 1 || cwd != `C:\Synthetic` {
			t.Fatalf("explicit local cwd=%q calls=%d err=%v", cwd, calls, err)
		}
	})
	t.Run("bound local session has precedence over account NAS choice", func(t *testing.T) {
		_, cwd, _, calls, err := selectedNASSubmission(t, `C:\Synthetic`, "", executionProbeOptions{offline: true, sessionDirectory: `C:\Synthetic\local`})
		if !errors.Is(err, agenvo.ErrUnknown) || calls != 1 || cwd != `C:\Synthetic\local` {
			t.Fatalf("bound local cwd=%q calls=%d err=%v", cwd, calls, err)
		}
	})
	t.Run("unavailable NAS default does not dispatch", func(t *testing.T) {
		root := storageTestShare + `\` + serviceTestRuntimeID
		_, cwd, _, calls, err := selectedNASSubmission(t, root, "", executionProbeOptions{offline: true})
		if !errors.Is(err, userruntime.ErrWorkspaceShareUnavailable) || calls != 0 || cwd != "" {
			t.Fatalf("unavailable default dispatched: calls=%d err=%v", calls, err)
		}
	})
}
