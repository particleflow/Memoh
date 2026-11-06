package containerfs

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/fallback"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
	"github.com/felinics/memoh/internal/workspace/bridgesvc"
)

type transferGuardServer struct {
	pb.ContainerServiceServer
	mediaRPCs atomic.Int32
}

func (s *transferGuardServer) ReadRaw(req *pb.ReadRawRequest, stream pb.ContainerService_ReadRawServer) error {
	s.mediaRPCs.Add(1)
	return s.ContainerServiceServer.ReadRaw(req, stream)
}

func (s *transferGuardServer) WriteRaw(stream pb.ContainerService_WriteRawServer) error {
	s.mediaRPCs.Add(1)
	return s.ContainerServiceServer.WriteRaw(stream)
}

func (s *transferGuardServer) Stat(ctx context.Context, req *pb.StatRequest) (*pb.StatResponse, error) {
	s.mediaRPCs.Add(1)
	return s.ContainerServiceServer.Stat(ctx, req)
}

func (s *transferGuardServer) ListDir(ctx context.Context, req *pb.ListDirRequest) (*pb.ListDirResponse, error) {
	s.mediaRPCs.Add(1)
	return s.ContainerServiceServer.ListDir(ctx, req)
}

func (s *transferGuardServer) DeleteFile(ctx context.Context, req *pb.DeleteFileRequest) (*pb.DeleteFileResponse, error) {
	s.mediaRPCs.Add(1)
	return s.ContainerServiceServer.DeleteFile(ctx, req)
}

func transferGuardFixture(t *testing.T) (*bridge.Client, *transferGuardServer) {
	t.Helper()
	root := t.TempDir()
	server := &transferGuardServer{ContainerServiceServer: bridgesvc.New(bridgesvc.Options{DefaultWorkDir: root, WorkspaceRoot: root, DataMount: "/data"})}
	client := newRecordingClient(t, server)
	if err := client.WriteFile(t.Context(), "/data/result.pdf", []byte("synthetic producer-owned file")); err != nil {
		t.Fatal(err)
	}
	return client, server
}

func TestManagedComputerCannotServeAsGenericMediaStorage(t *testing.T) {
	root, server := transferGuardFixture(t)
	for _, view := range []*bridge.Client{root.FilesOnly(), root.FilesOnly().WithOutgoingMetadata(map[string]string{"x-synthetic-scope": "selected-pc"})} {
		if view.DirectTransfersAllowed() {
			t.Fatal("restricted view advertises direct media authority")
		}
		p := New(staticClientProvider{client: view})
		for _, operation := range []struct {
			name string
			run  func() error
		}{
			{"put", func() error {
				return p.Put(t.Context(), "bot/file/synthetic.pdf", strings.NewReader("must not upload"))
			}},
			{"open", func() error { _, err := p.Open(t.Context(), "bot/file/synthetic.pdf"); return err }},
			{"delete", func() error { return p.Delete(t.Context(), "bot/file/synthetic.pdf") }},
			{"data path", func() error { _, err := p.OpenContainerFile(t.Context(), "bot", "/data/result.pdf"); return err }},
			{"host absolute path", func() error {
				_, err := p.OpenContainerFile(t.Context(), "bot", filepath.Join(t.TempDir(), "result.pdf"))
				return err
			}},
		} {
			if err := operation.run(); !errors.Is(err, bridge.ErrForbidden) {
				t.Errorf("%s must require producer delivery: %v", operation.name, err)
			}
		}
		if path := p.AccessPath(t.Context(), "bot/file/synthetic.pdf"); path != "" {
			t.Fatalf("managed PC exposed as media path: %q", path)
		}
		if keys, err := p.ListPrefix(t.Context(), "bot/file/synthetic"); err != nil || len(keys) != 0 {
			t.Fatalf("managed PC media listing exposed: %v %v", keys, err)
		}
	}
	if server.mediaRPCs.Load() != 0 {
		t.Fatalf("generic media operations reached managed PC: %d RPCs", server.mediaRPCs.Load())
	}
	// The marker restricts delivery authority, not ordinary file tools.
	r, err := root.FilesOnly().ReadRaw(t.Context(), "/data/result.pdf")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(content) != "synthetic producer-owned file" {
		t.Fatalf("ordinary file read changed: %q %v", content, err)
	}
}

func TestMediaIngestCannotBypassManagedComputerProducer(t *testing.T) {
	root, server := transferGuardFixture(t)
	guarded := New(staticClientProvider{client: root.FilesOnly().WithOutgoingMetadata(map[string]string{"x-synthetic": "view"})})
	service := media.NewService(nil, fallback.New(guarded, localfs.New(t.TempDir())))
	asset, err := service.IngestContainerFile(t.Context(), "synthetic-bot", "/data/result.pdf")
	if !errors.Is(err, bridge.ErrForbidden) || asset.ContentHash != "" || server.mediaRPCs.Load() != 0 {
		t.Fatalf("attachment ingestion bypassed producer via HostFS/fallback: %+v %v calls=%d", asset, err, server.mediaRPCs.Load())
	}
	// Raw media ingestion must not write to this PC as if it were NAS storage.
	_, err = media.NewService(nil, guarded).Ingest(t.Context(), media.IngestInput{BotID: "synthetic-bot", Mime: "text/plain", Reader: strings.NewReader("synthetic upload")})
	if !errors.Is(err, bridge.ErrForbidden) || server.mediaRPCs.Load() != 0 {
		t.Fatalf("media backend write bypassed producer: %v calls=%d", err, server.mediaRPCs.Load())
	}
}

func TestOrdinaryNativeMediaStorageRemainsUsable(t *testing.T) {
	root, server := transferGuardFixture(t)
	if !root.DirectTransfersAllowed() || !root.WithOutgoingMetadata(map[string]string{"x-synthetic": "ordinary"}).DirectTransfersAllowed() {
		t.Fatal("unconfigured native workspace lost media authority")
	}
	p := New(staticClientProvider{client: root})
	service := media.NewService(nil, p)
	asset, err := service.IngestContainerFile(t.Context(), "synthetic-bot", "/data/result.pdf")
	if err != nil || asset.ContentHash == "" || asset.SizeBytes != int64(len("synthetic producer-owned file")) {
		t.Fatalf("ordinary native ingestion regressed: %+v %v", asset, err)
	}
	r, err := p.Open(t.Context(), filepath.Join("synthetic-bot", asset.StorageKey))
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(content) != "synthetic producer-owned file" || server.mediaRPCs.Load() == 0 {
		t.Fatalf("ordinary native stored content changed: %q %v calls=%d", content, err, server.mediaRPCs.Load())
	}
}
