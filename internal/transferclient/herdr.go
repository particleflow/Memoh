package transferclient

import (
	"context"
	"errors"
	"os"
	"time"
)

// HerdrAction is the bounded input to the plugin command embedding this
// adapter. It contains logical identifiers only; a producer passes its already
// authorized source handle separately. No task status or credential is stored.
type HerdrAction struct {
	Kind       string
	DeliveryID string
	Job        Job
}

// InvokeHerdr is a finite plugin-action boundary. Herdr owns invocation and
// STATE_DIR resolution; its plugin-owned command must supply approved client
// credentials and the existing task/storage registration binding. This prepares
// the integration without inventing an installed binary, daemon or credential
// provider. The isolated tests invoke this exact action with explicit fixtures.
func InvokeHerdr(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, action HerdrAction, source *os.File, client *Client, binding *AssociationBinding, now time.Time) (QueueStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	q, err := OpenHerdrQueue(getenv, revision, limits)
	if err != nil {
		return QueueStatus{}, err
	}
	defer func() { _ = q.Close() }()
	return q.invokeHerdr(ctx, action, source, client, binding, now)
}

func (q *DiskQueue) invokeHerdr(ctx context.Context, action HerdrAction, source *os.File, client *Client, binding *AssociationBinding, now time.Time) (QueueStatus, error) {
	var err error
	id := action.DeliveryID
	switch action.Kind {
	case "enqueue-upload", "enqueue-download":
		if id != action.Job.DeliveryID {
			return QueueStatus{}, ErrInvalid
		}
		direction := "upload"
		if action.Kind == "enqueue-download" {
			direction = "download"
		}
		err = q.Admit(ctx, direction, action.Job, source)
	case "advance":
		if client == nil {
			return QueueStatus{}, ErrBinding
		}
		err = q.RunOne(ctx, id, client, binding, now)
	case "reconcile":
		if client == nil {
			return QueueStatus{}, ErrBinding
		}
		if err = q.ResumeAssociation(id); err == nil {
			err = q.RunOne(ctx, id, client, binding, now)
		}
	case "cancel":
		err = q.RequestCancel(id)
	case "retire":
		return QueueStatus{}, q.retire(ctx, id)
	case "inspect":
	default:
		return QueueStatus{}, ErrInvalid
	}
	if err != nil {
		if (action.Kind == "enqueue-upload" || action.Kind == "enqueue-download" || action.Kind == "advance") && (errors.Is(err, ErrAdmissionPending) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			status, readErr := q.Inspect(id)
			if readErr == nil || errors.Is(readErr, ErrAdmissionPending) {
				return status, err
			}
		}
		return QueueStatus{}, err
	}
	status, err := q.Inspect(id)
	if err == nil && (action.Kind == "advance" || action.Kind == "reconcile") && confirmedDelivery(status) {
		if err = q.ReleaseConfirmed(ctx, id); err != nil {
			return status, err
		}
		return q.Inspect(id)
	}
	return status, err
}
