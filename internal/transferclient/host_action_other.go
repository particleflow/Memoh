//go:build !windows

package transferclient

func (q *DiskQueue) configuredBinding(c *Client, status QueueStatus) (*AssociationBinding, func(), error) {
	if status.Direction != "upload" {
		return nil, nil, ErrBinding
	}
	return c.DeliveryBinding(nil), func() {}, nil
}
