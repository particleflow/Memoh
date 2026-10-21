package bridge

import (
	"context"

	"google.golang.org/grpc"

	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

// UnsupportedClient has no transport. Limited execution providers use it to
// fail every file/PTY/bridge request explicitly, never dialing a native workspace.
func UnsupportedClient() *Client {
	return &Client{svc: pb.NewContainerServiceClient(unsupportedConnection{}), target: "unsupported"}
}

type unsupportedConnection struct{}

func (unsupportedConnection) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return ErrUnsupported
}

func (unsupportedConnection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, ErrUnsupported
}
