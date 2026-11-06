package bridge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/metadata"

	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
	"github.com/felinics/memoh/internal/workspace/bridgesvc"
)

type filesOnlyTestServer struct {
	pb.ContainerServiceServer
	execs        atomic.Int32
	tunnels      atomic.Int32
	reverse      atomic.Int32
	statMetadata chan metadata.MD
}

func (s *filesOnlyTestServer) Exec(stream pb.ContainerService_ExecServer) error {
	s.execs.Add(1)
	if _, err := stream.Recv(); err != nil {
		return err
	}
	return stream.Send(&pb.ExecOutput{Stream: pb.ExecOutput_EXIT})
}

func (s *filesOnlyTestServer) Tunnel(pb.ContainerService_TunnelServer) error {
	s.tunnels.Add(1)
	return ErrForbidden
}

func (s *filesOnlyTestServer) ReverseHTTP(pb.ContainerService_ReverseHTTPServer) error {
	s.reverse.Add(1)
	return ErrForbidden
}

func (s *filesOnlyTestServer) Stat(ctx context.Context, in *pb.StatRequest) (*pb.StatResponse, error) {
	if s.statMetadata != nil {
		md, _ := metadata.FromIncomingContext(ctx)
		s.statMetadata <- md
	}
	return s.ContainerServiceServer.Stat(ctx, in)
}

func filesOnlyFixture(t *testing.T) (*Client, *filesOnlyTestServer) {
	t.Helper()
	root := t.TempDir()
	s := &filesOnlyTestServer{ContainerServiceServer: bridgesvc.New(bridgesvc.Options{DefaultWorkDir: root, WorkspaceRoot: root, DataMount: "/data"})}
	return newTestClient(t, s), s
}

func assertControlDenied(t *testing.T, view *Client) {
	t.Helper()
	for _, call := range []struct {
		name string
		run  func() error
	}{
		{"exec", func() error { _, err := view.Exec(t.Context(), "must not run", "/data", 1); return err }},
		{"hook exec", func() error {
			_, err := view.ExecWithStdinEnv(t.Context(), "must not run", "/data", 1, []byte("hook JSON"), []string{"MEMOH_HOOK_EVENT=synthetic"})
			return err
		}},
		{"stream", func() error { _, err := view.ExecStream(t.Context(), "must not run", "/data", 1); return err }},
		{"pty", func() error { _, err := view.ExecStreamPTY(t.Context(), "must not run", "/data", 80, 24); return err }},
		{"tunnel", func() error { _, err := view.DialContext(t.Context(), "tcp", "127.0.0.1:1"); return err }},
		{"reverse HTTP", func() error {
			_, err := view.ServeReverseHTTP(t.Context(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			return err
		}},
	} {
		if err := call.run(); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s must be denied locally: %v", call.name, err)
		}
	}
}

func TestFilesOnlyDeniesControlBeforeRPCAndDoesNotOwnConnection(t *testing.T) {
	root, server := filesOnlyFixture(t)
	view := root.FilesOnly()
	assertControlDenied(t, view)
	if server.execs.Load() != 0 || server.tunnels.Load() != 0 || server.reverse.Load() != 0 {
		t.Fatalf("denied control reached runtime: exec=%d tunnel=%d reverse=%d", server.execs.Load(), server.tunnels.Load(), server.reverse.Load())
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	// The dedicated task transport retains its original connection and rights.
	if result, err := root.Exec(t.Context(), "synthetic acknowledgement", "/data", 1); err != nil || result.ExitCode != 0 || server.execs.Load() != 1 {
		t.Fatalf("view altered or closed original client: %+v %v calls=%d", result, err, server.execs.Load())
	}
}

func TestFilesOnlyPreservesFilesystemRPCs(t *testing.T) {
	root, server := filesOnlyFixture(t)
	view := root.FilesOnly()
	if err := view.Mkdir(t.Context(), "/data/sub"); err != nil {
		t.Fatal(err)
	}
	if err := view.WriteFile(t.Context(), "/data/sub/a.txt", []byte("synthetic text\n")); err != nil {
		t.Fatal(err)
	}
	if got, err := view.ReadFile(t.Context(), "/data/sub/a.txt", 0, 0); err != nil || got.Content != "synthetic text\n" {
		t.Fatalf("read: %+v %v", got, err)
	}
	if _, err := view.Stat(t.Context(), "/data/sub/a.txt"); err != nil {
		t.Fatal(err)
	}
	if list, err := view.ListDirAll(t.Context(), "/data/sub", false); err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := view.Rename(t.Context(), "/data/sub/a.txt", "/data/sub/b.txt"); err != nil {
		t.Fatal(err)
	}
	content := []byte{0, 1, 2, 0xff}
	if written, err := view.WriteRaw(t.Context(), "/data/sub/raw.bin", bytes.NewReader(content)); err != nil || written != int64(len(content)) {
		t.Fatalf("write raw: %d %v", written, err)
	}
	r, err := view.ReadRaw(t.Context(), "/data/sub/raw.bin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("raw bytes changed: %v %v", got, err)
	}
	if err := view.DeleteFile(t.Context(), "/data/sub", true); err != nil {
		t.Fatal(err)
	}
	if _, err := view.Stat(t.Context(), "/data/sub"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete: %v", err)
	}
	if server.execs.Load() != 0 {
		t.Fatal("filesystem RPC started a process")
	}
}

func TestFilesOnlyMetadataCannotRestoreControl(t *testing.T) {
	root, server := filesOnlyFixture(t)
	server.statMetadata = make(chan metadata.MD, 3)
	for _, view := range []*Client{
		root.FilesOnly().WithOutgoingMetadata(map[string]string{"x-synthetic-scope": "selected-pc"}),
		root.WithOutgoingMetadata(map[string]string{"x-synthetic-scope": "selected-pc"}).FilesOnly(),
		root.FilesOnly().WithOutgoingMetadata(map[string]string{"x-prior": "one"}).FilesOnly().WithOutgoingMetadata(map[string]string{"x-synthetic-scope": "selected-pc"}),
	} {
		if view == nil {
			t.Fatal("missing restricted metadata view")
		}
		assertControlDenied(t, view)
		if _, err := view.Stat(t.Context(), "/data"); err != nil {
			t.Fatal(err)
		}
		if got := (<-server.statMetadata).Get("x-synthetic-scope"); len(got) != 1 || got[0] != "selected-pc" {
			t.Fatalf("metadata not delivered: %v", got)
		}
		if err := view.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if server.execs.Load() != 0 || server.tunnels.Load() != 0 || server.reverse.Load() != 0 {
		t.Fatalf("metadata restored control: exec=%d tunnel=%d reverse=%d", server.execs.Load(), server.tunnels.Load(), server.reverse.Load())
	}
}
