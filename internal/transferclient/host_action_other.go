//go:build !windows

package transferclient

import (
	"context"
	"errors"
	"os"
)

func (*DiskQueue) configuredBinding(c *Client, status QueueStatus) (*AssociationBinding, func(), error) {
	if status.Direction != "upload" {
		return nil, nil, ErrBinding
	}
	return c.DeliveryBinding(nil), func() {}, nil
}

func (*DiskQueue) MigrateNativePublication(context.Context) (bool, error) { return false, ErrBinding }

func (q *DiskQueue) migratePublicationBudget(bool) error {
	if _, err := q.root.Lstat(publicationDirectory); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return ErrBinding // Never reinterpret a native publication layout on Linux.
}
