package bridge

import (
	"context"

	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnsupportedClient has no transport. Limited execution providers use it to
// fail every file/PTY/bridge request explicitly, never dialing a native workspace.
func UnsupportedClient() *Client {
	return &Client{svc: pb.NewContainerServiceClient(unsupportedConnection{}), target: "unsupported"}
}

type unsupportedConnection struct{}

func (unsupportedConnection) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return status.Error(codes.Unimplemented, "This execution target supports foreground commands only.")
}
func (unsupportedConnection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.Unimplemented, "This execution target supports foreground commands only.")
}
