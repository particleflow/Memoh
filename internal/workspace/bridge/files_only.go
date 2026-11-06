package bridge

import (
	"context"

	"google.golang.org/grpc"

	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

// FilesOnly returns a non-owning view that keeps filesystem RPCs but denies
// process execution and network transports before any RPC reaches the device.
// Computer-task targets use this view for general tools and Hooks; only their
// separately authorized task adapter may hold the original execution client.
// This is a capability restriction, not a filesystem sandbox or file-delivery
// authorization. The existing target and producer checks remain necessary.
func (c *Client) FilesOnly() *Client {
	if c == nil {
		return nil
	}
	if c.svc == nil {
		return UnsupportedClient().FilesOnly()
	}
	view := *c
	view.ownsConn = false
	view.filesOnly = true
	view.svc = filesOnlyService{client: c.svc}
	return &view
}

// DirectTransfersAllowed reports whether general media storage may use this
// client. Managed computer files remain readable by ordinary workspace tools,
// but media ingress/egress must use the separate producer-authorized delivery
// path rather than treating that computer as the Bot's media backend.
func (c *Client) DirectTransfersAllowed() bool {
	return c != nil && !c.filesOnly
}

// Implement every method explicitly so a newly generated RPC cannot silently
// inherit execution authority through an embedded interface.
type filesOnlyService struct{ client pb.ContainerServiceClient }

var _ pb.ContainerServiceClient = filesOnlyService{}

func (c filesOnlyService) ReadFile(ctx context.Context, in *pb.ReadFileRequest, opts ...grpc.CallOption) (*pb.ReadFileResponse, error) {
	return c.client.ReadFile(ctx, in, opts...)
}

func (c filesOnlyService) WriteFile(ctx context.Context, in *pb.WriteFileRequest, opts ...grpc.CallOption) (*pb.WriteFileResponse, error) {
	return c.client.WriteFile(ctx, in, opts...)
}

func (c filesOnlyService) ListDir(ctx context.Context, in *pb.ListDirRequest, opts ...grpc.CallOption) (*pb.ListDirResponse, error) {
	return c.client.ListDir(ctx, in, opts...)
}

func (c filesOnlyService) Stat(ctx context.Context, in *pb.StatRequest, opts ...grpc.CallOption) (*pb.StatResponse, error) {
	return c.client.Stat(ctx, in, opts...)
}

func (c filesOnlyService) Mkdir(ctx context.Context, in *pb.MkdirRequest, opts ...grpc.CallOption) (*pb.MkdirResponse, error) {
	return c.client.Mkdir(ctx, in, opts...)
}

func (c filesOnlyService) Rename(ctx context.Context, in *pb.RenameRequest, opts ...grpc.CallOption) (*pb.RenameResponse, error) {
	return c.client.Rename(ctx, in, opts...)
}

func (filesOnlyService) Exec(context.Context, ...grpc.CallOption) (grpc.BidiStreamingClient[pb.ExecInput, pb.ExecOutput], error) {
	return nil, ErrForbidden
}

func (filesOnlyService) Tunnel(context.Context, ...grpc.CallOption) (grpc.BidiStreamingClient[pb.TunnelFrame, pb.TunnelFrame], error) {
	return nil, ErrForbidden
}

func (filesOnlyService) ReverseHTTP(context.Context, ...grpc.CallOption) (grpc.BidiStreamingClient[pb.ReverseHTTPFrame, pb.ReverseHTTPFrame], error) {
	return nil, ErrForbidden
}

func (c filesOnlyService) ReadRaw(ctx context.Context, in *pb.ReadRawRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.DataChunk], error) {
	return c.client.ReadRaw(ctx, in, opts...)
}

func (c filesOnlyService) WriteRaw(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[pb.WriteRawChunk, pb.WriteRawResponse], error) {
	return c.client.WriteRaw(ctx, opts...)
}

func (c filesOnlyService) DeleteFile(ctx context.Context, in *pb.DeleteFileRequest, opts ...grpc.CallOption) (*pb.DeleteFileResponse, error) {
	return c.client.DeleteFile(ctx, in, opts...)
}
