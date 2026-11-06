package transferclient

import (
	"context"
	"errors"
	"io"
	"os"
)

var ErrPublicationConflict = errors.New("local publication conflicts with retained identity")

// PublicationAuthorization is the existing host's current task/destination
// authorization. Operation/delivery IDs alone cannot implement this callback.
type PublicationAuthorization func(context.Context, Reference) error

// A source proof is created only after RunOne verifies a private download while
// retaining its handle. It expires when the callback returns. Native publication
// transfers that same object, so there is no second full-file copy or untrusted
// source pathname. External callers cannot manufacture a proof from an io.Reader.
type publicationSource struct {
	*io.SectionReader
	file      *os.File
	queue     *DiskQueue
	reference Reference
	active    bool
}
