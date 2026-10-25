package workspace

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/config"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/userruntime"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

// This in-process server counts actual RPC arrival. A failed remote call is
// insufficient: restricted clients must prevent the command reaching Runtime.
type nativeHerdrRPCRecorder struct {
	pb.UnimplementedContainerServiceServer
	execs, tunnels, reverse, reads, writes atomic.Int64
}

func (s *nativeHerdrRPCRecorder) Exec(stream pb.ContainerService_ExecServer) error {
	s.execs.Add(1)
	if _, err := stream.Recv(); err != nil {
		return err
	}
	return stream.Send(&pb.ExecOutput{Stream: pb.ExecOutput_EXIT, ExitCode: 0})
}

func (s *nativeHerdrRPCRecorder) Tunnel(pb.ContainerService_TunnelServer) error {
	s.tunnels.Add(1)
	return errors.New("unexpected tunnel reached synthetic Runtime")
}

func (s *nativeHerdrRPCRecorder) ReverseHTTP(pb.ContainerService_ReverseHTTPServer) error {
	s.reverse.Add(1)
	return errors.New("unexpected reverse HTTP reached synthetic Runtime")
}

func (s *nativeHerdrRPCRecorder) Stat(context.Context, *pb.StatRequest) (*pb.StatResponse, error) {
	s.reads.Add(1)
	return &pb.StatResponse{Entry: &pb.FileEntry{IsDir: true}}, nil
}

func (s *nativeHerdrRPCRecorder) WriteFile(context.Context, *pb.WriteFileRequest) (*pb.WriteFileResponse, error) {
	s.writes.Add(1)
	return &pb.WriteFileResponse{}, nil
}

func nativeHerdrWorkspaceFixture(t *testing.T, providerConfig string) (*Manager, *bridge.Client, *nativeHerdrRPCRecorder) {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	recorder := &nativeHerdrRPCRecorder{}
	server := grpc.NewServer()
	pb.RegisterContainerServiceServer(server, recorder)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///native-herdr-task-boundary",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	rootClient := bridge.NewClientFromConn(conn)
	store := &fakeRemoteBindingStore{records: []dbstore.BotRemoteRuntimeBindingRecord{{
		ID: remoteTestTargetID, BotID: remoteTestBotID, RuntimeID: remoteTestRuntimeID,
		IsPrimary: true, RuntimeUserID: remoteTestOwnerID, BotOwnerUserID: remoteTestOwnerID,
		RuntimeProvider: "memoh", RuntimeProviderConfig: []byte(providerConfig),
	}}}
	remote := &RemoteWorkspaceService{store: store, runtimes: fakeRuntimeConnections{remoteTestRuntimeID: {
		RuntimeID: remoteTestRuntimeID, Client: rootClient,
		Info: userruntime.RuntimeInfo{OS: "win32", WorkspaceBase: `C:\Synthetic`, Capabilities: []string{userruntime.CapabilityFS, userruntime.CapabilityExec, userruntime.CapabilityHostFS}},
	}}}
	manager := NewManager(slog.New(slog.DiscardHandler), nil, nil, config.WorkspaceConfig{}, "", nil)
	manager.SetRemoteWorkspaceService(remote)
	return manager, rootClient, recorder
}

func assertNativeHerdrExecBlocked(t *testing.T, ctx context.Context, client *bridge.Client, recorder *nativeHerdrRPCRecorder) {
	t.Helper()
	if client == nil {
		t.Fatal("missing file-capable client")
	}
	if _, err := client.ExecWithStdinEnv(ctx, "synthetic-no-op", `C:\Synthetic`, 1, []byte("synthetic input"), []string{"SYNTHETIC=1"}); !errors.Is(err, bridge.ErrForbidden) {
		t.Fatal("hook-style ExecWithStdinEnv lacked local task gate", err)
	}
	if stream, err := client.ExecStreamWithOptions(ctx, "synthetic-no-op", `C:\Synthetic`, 1, bridge.ExecOptions{}); !errors.Is(err, bridge.ErrForbidden) {
		if stream != nil {
			_ = stream.Close()
		}
		t.Fatal("stream Exec lacked local task gate", err)
	}
	if stream, err := client.ExecStreamPTY(ctx, "synthetic-no-op", `C:\Synthetic`, 80, 24); !errors.Is(err, bridge.ErrForbidden) {
		if stream != nil {
			_ = stream.Close()
		}
		t.Fatal("PTY Exec lacked local task gate", err)
	}
	if conn, err := client.DialContext(ctx, "tcp", "127.0.0.1:1"); !errors.Is(err, bridge.ErrForbidden) {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatal("tunnel lacked local task gate", err)
	}
	if stop, err := client.ServeReverseHTTP(ctx, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); !errors.Is(err, bridge.ErrForbidden) {
		if stop != nil {
			stop()
		}
		t.Fatal("reverse HTTP lacked local task gate", err)
	}
	if recorder.execs.Load() != 0 || recorder.tunnels.Load() != 0 || recorder.reverse.Load() != 0 {
		t.Fatalf("blocked operation reached Runtime: exec=%d tunnel=%d reverse=%d", recorder.execs.Load(), recorder.tunnels.Load(), recorder.reverse.Load())
	}
	if _, err := client.Stat(ctx, `C:\Synthetic`); err != nil {
		t.Fatal("restricted client lost existing filesystem reads", err)
	}
	if err := client.WriteFile(ctx, `C:\Synthetic\fixture.txt`, []byte("synthetic")); err != nil {
		t.Fatal("restricted client lost existing filesystem writes", err)
	}
}

func TestNativeHerdrWorkspaceEntryPointsCannotBypassTaskLedger(t *testing.T) {
	profiles := map[string]string{
		"configured": `{"herdr":{"instance_id":"synthetic","session":"synthetic","work_dir":"C:\\Synthetic","bash_path":"C:\\Synthetic\\Git\\bin\\bash.exe","herdr_cli":"C:\\Synthetic\\herdr.exe","herdr_config_root":"C:\\Synthetic\\herdr"}}`,
		"null":       `{"herdr":null}`,
		"empty":      `{"herdr":{}}`,
		"string":     `{"herdr":"damaged"}`,
		"array":      `{"herdr":[]}`,
	}
	for name, profile := range profiles {
		t.Run(name, func(t *testing.T) {
			manager, rootClient, recorder := nativeHerdrWorkspaceFixture(t, profile)
			ctx, cancel := context.WithTimeout(WithWorkspaceTarget(t.Context(), remoteTestTargetID), 5*time.Second)
			defer cancel()
			target, err := manager.ResolveWorkspaceTarget(ctx, remoteTestBotID, remoteTestTargetID)
			if err != nil || target.Execute == nil {
				t.Fatal("configured Runtime lost explicit computer-task gate", err)
			}
			if out, err := target.Execute(ctx, agenvo.Request{}); !errors.Is(err, agenvo.ErrComputerTaskRequired) || out.State != "not_started" {
				t.Fatalf("Execute gate=%+v %v", out, err)
			}
			assertNativeHerdrExecBlocked(t, ctx, target.Client, recorder)
			client, err := manager.MCPClient(ctx, remoteTestBotID)
			if err != nil {
				t.Fatal(err)
			}
			assertNativeHerdrExecBlocked(t, ctx, client, recorder)
			assertNativeHerdrExecBlocked(t, ctx, client.WithOutgoingMetadata(map[string]string{"synthetic": "value"}), recorder)
			primary, err := manager.MCPClient(t.Context(), remoteTestBotID)
			if err != nil {
				t.Fatal(err)
			}
			assertNativeHerdrExecBlocked(t, ctx, primary, recorder)
			hookConfig, err := manager.HookConfigClient(ctx, remoteTestBotID)
			if err != nil {
				t.Fatal(err)
			}
			assertNativeHerdrExecBlocked(t, ctx, hookConfig, recorder)
			if recorder.reads.Load() != 5 || recorder.writes.Load() != 5 {
				t.Fatal("file operations did not reach the selected native Runtime")
			}
			// The owner Runtime connection remains usable by the separately
			// authorized typed adapter; a restricted view cannot disable it.
			if _, err := rootClient.Exec(ctx, "synthetic-no-op", `C:\Synthetic`, 1); err != nil || recorder.execs.Load() != 1 {
				t.Fatal("file-only view mutated the raw connection", err)
			}
		})
	}
}

func TestNativeRuntimeWithoutHerdrKeepsExistingExec(t *testing.T) {
	for _, profile := range []string{"", "{}", `{"unrelated":"setting"}`} {
		t.Run(profile, func(t *testing.T) {
			manager, _, recorder := nativeHerdrWorkspaceFixture(t, profile)
			ctx, cancel := context.WithTimeout(WithWorkspaceTarget(t.Context(), remoteTestTargetID), 5*time.Second)
			defer cancel()
			target, err := manager.ResolveWorkspaceTarget(ctx, remoteTestBotID, remoteTestTargetID)
			if err != nil || target.Execute != nil {
				t.Fatal("unconfigured native target acquired Herdr gate", err)
			}
			client, err := manager.MCPClient(ctx, remoteTestBotID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ExecWithStdinEnv(ctx, "synthetic-no-op", `C:\Synthetic`, 1, nil, nil); err != nil {
				t.Fatal("ordinary native Exec changed", err)
			}
			for _, pty := range []bool{false, true} {
				var stream *bridge.ExecStream
				if pty {
					stream, err = target.Client.ExecStreamPTY(ctx, "synthetic-no-op", `C:\Synthetic`, 80, 24)
				} else {
					stream, err = target.Client.ExecStream(ctx, "synthetic-no-op", `C:\Synthetic`, 1)
				}
				if err != nil {
					t.Fatal("ordinary native stream changed", err)
				}
				out, err := stream.Recv()
				_ = stream.Close()
				if err != nil || out.GetStream() != pb.ExecOutput_EXIT {
					t.Fatal("synthetic Runtime did not receive ordinary native stream", err)
				}
			}
			if recorder.execs.Load() != 3 {
				t.Fatalf("ordinary native Exec calls=%d", recorder.execs.Load())
			}
		})
	}
}
