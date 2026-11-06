//go:build windows

package transferclient

import "context"

func (q *DiskQueue) configuredBinding(c *Client, status QueueStatus) (*AssociationBinding, func(), error) {
	if status.Direction == "upload" {
		return c.DeliveryBinding(nil), func() {}, nil
	}
	j := status.Job
	ref := Reference{DeliveryID: j.DeliveryID, OperationID: j.OperationID, BotID: j.BotID, ContentHash: j.Spec.SHA256, SizeBytes: j.Spec.SizeBytes, Mime: j.Spec.Mime}
	p, err := q.OpenNativePublication(func(ctx context.Context, candidate Reference) error {
		if candidate != ref {
			return ErrAuthorization
		}
		// Existing task HTTP guard rechecks device revocation AND live operation,
		// target and Bot authorization. This is never a local always-allow shim.
		_, err := c.Limits(ctx, ref.BotID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return c.DeliveryBinding(p), func() { _ = p.Close() }, nil
}
