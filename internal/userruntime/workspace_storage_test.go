package userruntime

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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

const storageTestShare = `\\synthetic-nas\existing-share\workspaces`

type storageTestStore struct {
	dbstore.UserRuntimeStore
	raw      dbstore.JSON
	writes   int
	conflict bool
}

func (s *storageTestStore) OwnedNativeHerdrRuntime(_ context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	if owner != "owner" || id != serviceTestRuntimeID {
		return dbstore.UserRuntimeRecord{}, db.ErrNotFound
	}
	return dbstore.UserRuntimeRecord{ID: id, UserID: owner, Provider: "memoh", ProviderConfig: append(dbstore.JSON{}, s.raw...)}, nil
}

func (s *storageTestStore) ConfigureNativeHerdrRuntime(ctx context.Context, owner, id string, before, after dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	if s.conflict || string(before) != string(s.raw) {
		return dbstore.UserRuntimeRecord{}, db.ErrNotFound
	}
	if _, err := s.OwnedNativeHerdrRuntime(ctx, owner, id); err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	s.writes++
	s.raw = after
	return s.OwnedNativeHerdrRuntime(ctx, owner, id)
}

type storageTestRPC struct {
	pb.UnimplementedContainerServiceServer
	root        string
	reads       atomic.Int32
	unavailable atomic.Bool
}

func (s *storageTestRPC) Stat(_ context.Context, in *pb.StatRequest) (*pb.StatResponse, error) {
	s.reads.Add(1)
	if s.unavailable.Load() {
		return nil, status.Error(codes.Unavailable, "synthetic share disconnected")
	}
	if in.Path != storageTestShare && !strings.HasPrefix(in.Path, storageTestShare+`\`) {
		return nil, status.Error(codes.PermissionDenied, "outside configured share")
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(in.Path, storageTestShare), `\`)
	info, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(strings.ReplaceAll(relative, `\`, "/"))))
	if err != nil {
		return nil, status.Error(codes.NotFound, "synthetic directory absent")
	}
	return &pb.StatResponse{Entry: &pb.FileEntry{IsDir: info.IsDir()}}, nil
}

func storageFixture(t *testing.T) (*Service, *storageTestStore, *storageTestRPC, string) {
	t.Helper()
	root := t.TempDir()
	store := &storageTestStore{raw: []byte(`{"herdr":{"work_dir":"C:\\existing"},"computer_admission":{"phase":"paused"}}`)}
	rpc := &storageTestRPC{root: root}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterContainerServiceServer(server, rpc)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///synthetic-share", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	hub := NewHub(nil)
	if err = hub.registerGuarded(&Connection{RuntimeID: serviceTestRuntimeID, ConnectionID: "synthetic-generation", Info: RuntimeInfo{OS: "win32"}, Client: bridge.NewClientFromConn(conn)}, nil); err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, hub)
	svc.SetWorkspaceStorage(root, storageTestShare)
	return svc, store, rpc, root
}

func TestWorkspaceStorageOwnedSelectionPreservesExecutionAndGETIsReadOnly(t *testing.T) {
	svc, store, _, root := storageFixture(t)
	before := string(store.raw)
	page, err := svc.WorkspaceDirectories(t.Context(), "owner", serviceTestRuntimeID, "")
	if err != nil || page.Path != "." || page.SelectedPath != "." || len(page.Directories) != 0 {
		t.Fatalf("initial read: %+v %v", page, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 || string(store.raw) != before {
		t.Fatal("GET created storage or changed configuration")
	}
	if err = svc.CreateWorkspaceDirectory(t.Context(), "owner", serviceTestRuntimeID, "project"); err != nil {
		t.Fatal(err)
	}
	if store.writes != 0 {
		t.Fatal("mkdir changed selection")
	}
	page, err = svc.WorkspaceDirectories(t.Context(), "owner", serviceTestRuntimeID, ".")
	if err != nil || len(page.Directories) != 1 || page.Directories[0].Path != "project" {
		t.Fatalf("browse: %+v %v", page, err)
	}
	if err = svc.SelectWorkspaceDirectory(t.Context(), "owner", serviceTestRuntimeID, "project"); err != nil {
		t.Fatal(err)
	}
	var values map[string]json.RawMessage
	if err = json.Unmarshal(store.raw, &values); err != nil {
		t.Fatal(err)
	}
	if string(values["herdr"]) != `{"work_dir":"C:\\existing"}` || string(values["computer_admission"]) != `{"phase":"paused"}` || store.writes != 1 {
		t.Fatalf("unrelated config changed: %s", store.raw)
	}
	// A new service after restart reads the same selected path, not local state.
	restarted := NewService(store, svc.hub)
	restarted.SetWorkspaceStorage(root, storageTestShare)
	page, err = restarted.WorkspaceDirectories(t.Context(), "owner", serviceTestRuntimeID, "")
	if err != nil || page.SelectedPath != "project" || page.WindowsPath != storageTestShare+`\`+serviceTestRuntimeID+`\project` {
		t.Fatalf("restart: %+v %v", page, err)
	}
}

func TestWorkspaceStorageDenialOfflineAndTraversalWriteNothing(t *testing.T) {
	svc, store, rpc, root := storageFixture(t)
	if err := svc.CreateWorkspaceDirectory(t.Context(), "other-owner", serviceTestRuntimeID, "x"); !errors.Is(err, db.ErrNotFound) {
		t.Fatal(err)
	}
	for _, relative := range []string{"../other", "/absolute", `C:\Synthetic`, storageTestShare, "a/../b", "a\x00b", "trailing.", "trailing ", "invalid?name"} {
		if err := svc.SelectWorkspaceDirectory(t.Context(), "owner", serviceTestRuntimeID, relative); !errors.Is(err, ErrWorkspaceStoragePath) {
			t.Fatalf("path %q: %v", relative, err)
		}
	}
	if rpc.reads.Load() != 0 {
		t.Fatal("invalid owner/path reached computer")
	}
	rpc.unavailable.Store(true)
	if err := svc.SelectWorkspaceDirectory(t.Context(), "owner", serviceTestRuntimeID, "."); !errors.Is(err, ErrWorkspaceShareUnavailable) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 || store.writes != 0 {
		t.Fatal("off-LAN/failed share created directory or selected a path")
	}
	if _, err = svc.WorkspaceDirectories(t.Context(), "owner", serviceTestRuntimeID, ""); !errors.Is(err, ErrWorkspaceShareUnavailable) {
		t.Fatal(err)
	}
}

func TestWorkspaceStorageConflictAndSymlinkKeepOriginalBinding(t *testing.T) {
	svc, store, _, root := storageFixture(t)
	before := string(store.raw)
	store.conflict = true
	if err := svc.SelectWorkspaceDirectory(t.Context(), "owner", serviceTestRuntimeID, "."); !errors.Is(err, ErrWorkspaceStorageConflict) {
		t.Fatal(err)
	}
	if store.writes != 0 || string(store.raw) != before {
		t.Fatal("CAS conflict changed config")
	}
	if err := os.Remove(filepath.Join(root, serviceTestRuntimeID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, serviceTestRuntimeID)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WorkspaceDirectories(t.Context(), "owner", serviceTestRuntimeID, ""); !errors.Is(err, ErrWorkspaceStoragePath) {
		t.Fatal(err)
	}
}

func TestWorkspaceStorageUnconfiguredNeverUsesLocalDefault(t *testing.T) {
	svc, store, rpc, root := storageFixture(t)
	for _, share := range []string{"", "https://synthetic-nas/files", `\\synthetic-nas\share\..`} {
		svc.SetWorkspaceStorage(root, share)
		if _, err := svc.WorkspaceDirectories(t.Context(), "owner", serviceTestRuntimeID, ""); !errors.Is(err, ErrWorkspaceStorageDisabled) {
			t.Fatal(err)
		}
	}
	if rpc.reads.Load() != 0 || store.writes != 0 {
		t.Fatal("missing NAS config fell back to local execution directory")
	}
}
