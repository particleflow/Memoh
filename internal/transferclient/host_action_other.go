//go:build !windows

package transferclient

import "context"

func (*DiskQueue) configuredBinding(c *Client, status QueueStatus) (*AssociationBinding, func(), error) {
	if status.Direction != "upload" {
		return nil, nil, ErrBinding
	}
	return c.DeliveryBinding(nil), func() {}, nil
}

func (*DiskQueue) MigrateNativePublication(context.Context) (bool, error) { return false, ErrBinding }
