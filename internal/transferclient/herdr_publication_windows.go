//go:build windows

package transferclient

import (
	"context"
	"os"
	"time"
)

// InvokeHerdrNativeDelivery connects the existing finite Herdr action to the
// native destination and candidate NAS association protocol. The caller must
// supply its approved HTTPS client and current task/destination authorizer.
// It does not install a plugin, create credentials or enable a production route.
func InvokeHerdrNativeDelivery(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, action HerdrAction, source *os.File, client *Client, authorize PublicationAuthorization, now time.Time) (QueueStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	q, err := OpenHerdrQueue(getenv, revision, limits)
	if err != nil {
		return QueueStatus{}, err
	}
	defer func() { _ = q.Close() }()
	var binding *AssociationBinding
	if action.Kind == "advance" || action.Kind == "reconcile" {
		if client == nil {
			return QueueStatus{}, ErrBinding
		}
		status, err := q.Inspect(action.DeliveryID)
		if err != nil {
			return status, err
		}
		if status.Direction == "download" {
			publication, err := q.OpenNativePublication(authorize)
			if err != nil {
				return QueueStatus{}, err
			}
			defer func() { _ = publication.Close() }()
			binding = client.DeliveryBinding(publication)
		} else {
			binding = client.DeliveryBinding(nil)
		}
	}
	return q.invokeHerdr(ctx, action, source, client, binding, now)
}
