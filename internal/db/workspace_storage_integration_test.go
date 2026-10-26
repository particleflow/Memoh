//go:build integration

package db_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/userruntime"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

const workspaceTestShare = `\\synthetic-nas\existing-share\workspace-storage`

// This is a controlled existing-RPC fixture, not a real Windows/UNC/LAN test.
type workspaceShareRPC struct {
	pb.UnimplementedContainerServiceServer
	root        string
	failureFile string
}

func (s workspaceShareRPC) Stat(_ context.Context, in *pb.StatRequest) (*pb.StatResponse, error) {
	if s.failureFile != "" {
		if _, err := os.Stat(s.failureFile); err == nil {
			return nil, status.Error(codes.Unavailable, "synthetic share disconnected")
		}
	}
	if in.Path != workspaceTestShare && !strings.HasPrefix(in.Path, workspaceTestShare+`\`) {
		return nil, status.Error(codes.PermissionDenied, "outside fixture share")
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(in.Path, workspaceTestShare), `\`)
	info, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(strings.ReplaceAll(relative, `\`, "/"))))
	if err != nil {
		return nil, status.Error(codes.NotFound, "fixture path not found")
	}
	return &pb.StatResponse{Entry: &pb.FileEntry{IsDir: info.IsDir()}}, nil
}

func TestWorkspaceStoragePostgresHTTPPreservesRegistration(t *testing.T) {
	f := newNativeHerdrFixture(t)
	root := t.TempDir()
	failureFile := filepath.Join(t.TempDir(), "synthetic-share-unavailable")
	key := "mrk_" + strings.Repeat("a", 64) // Synthetic key exists only in this fresh test DB.
	f.exec(t, "UPDATE user_runtimes SET api_token=$2 WHERE id=$1", f.runtime, key)
	listener := bufconn.Listen(1024 * 1024)
	rpc := grpc.NewServer()
	pb.RegisterContainerServiceServer(rpc, workspaceShareRPC{root: root, failureFile: failureFile})
	go func() { _ = rpc.Serve(listener) }()
	t.Cleanup(rpc.Stop)
	client, err := grpc.NewClient("passthrough:///workspace-share-fixture", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	hub := userruntime.NewHub(slog.New(slog.DiscardHandler))
	svc := userruntime.NewService(f.store, hub)
	svc.SetWorkspaceStorage(root, workspaceTestShare)
	if err = svc.ActivateConnection(t.Context(), key, f.runtime, userruntime.HandshakeInfo{Version: 1, OS: "win32", WorkspaceBase: `C:\unchanged-local-default`}, &userruntime.Connection{ConnectionID: "synthetic-storage-generation", Client: bridge.NewClientFromConn(client)}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	const secret = "synthetic-workspace-storage-test-jwt"
	token, _, err := auth.GenerateToken(f.owner, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	app := server.NewServerWithSessionValidator(slog.New(slog.DiscardHandler), "", secret, func(context.Context, string) error { return nil }, handlers.NewUserRuntimeHandler(nil, svc))
	httpServer := httptest.NewServer(app)
	defer httpServer.Close()
	call := func(method, suffix, body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, httpServer.URL+"/users/me/runtimes/"+f.runtime+"/workspace-storage"+suffix, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		var out bytes.Buffer
		if _, err = out.ReadFrom(response.Body); err != nil {
			t.Fatal(err)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
		return response.StatusCode, out.Bytes()
	}
	if code, _ := call(http.MethodGet, "", ""); code != http.StatusOK {
		t.Fatal(code)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("GET created a directory", err)
	}
	if code, body := call(http.MethodPost, "/directories", `{"path":"project"}`); code != http.StatusNoContent {
		t.Fatalf("mkdir: %d %s", code, body)
	}
	if code, body := call(http.MethodPut, "", `{"path":"project"}`); code != http.StatusNoContent {
		t.Fatalf("select: %d %s", code, body)
	}
	row, err := f.store.OwnedNativeHerdrRuntime(t.Context(), f.owner, f.runtime)
	if err != nil {
		t.Fatal(err)
	}
	var original, current map[string]any
	if err = json.Unmarshal(f.config, &original); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(row.ProviderConfig, &current); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original["herdr"], current["herdr"]) {
		t.Fatal("Herdr identity changed")
	}
	restarted := userruntime.NewService(f.store, hub)
	restarted.SetWorkspaceStorage(root, workspaceTestShare)
	page, err := restarted.WorkspaceDirectories(t.Context(), f.owner, f.runtime, "")
	if err != nil || page.SelectedPath != "project" || page.WindowsPath != workspaceTestShare+`\`+f.runtime+`\project` {
		t.Fatal("persisted selection lost", err)
	}
	// Optional actual Web exercise, explicitly enabled by the local verifier.
	// The helper receives synthetic credentials only; no secrets are printed.
	if script := os.Getenv("TEST_WORKSPACE_STORAGE_BROWSER_SCRIPT"); script != "" {
		cmd := exec.CommandContext(t.Context(), "node", script)
		cmd.Env = append(os.Environ(), "TEST_STORAGE_URL="+httpServer.URL, "TEST_STORAGE_TOKEN="+token, "TEST_RUNTIME_ID="+f.runtime, "TEST_STORAGE_FAILURE_FILE="+failureFile)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Web exercise: %v\n%s", err, output)
		} else {
			t.Log(string(output))
		}
		if _, err := os.Stat(filepath.Join(root, f.runtime, "project", "browser-project", "unavailable-project")); !os.IsNotExist(err) {
			t.Fatal("unavailable share created a NAS directory")
		}
	}
}
