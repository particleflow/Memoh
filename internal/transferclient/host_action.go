package transferclient

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sort"
	"time"
)

var ErrNoWork = errors.New("no delivery is due")

// ConfiguredAction is metadata only. Enqueue input is a bounded JSON document,
// never source bytes or a credential. SourceRef selects a file in the one
// configured producer root; subsequent actions use only the durable delivery ID.
type ConfiguredAction struct {
	Kind       string `json:"kind"`
	DeliveryID string `json:"delivery_id"`
	Job        Job    `json:"job"`
	SourceRef  string `json:"source_ref,omitempty"`
}

// InvokeConfiguredHerdr is the finite installed-command boundary. The optional
// HTTP client exists for isolated TLS roots in tests; New enforces TLS policy.
// No scheduler, credential issuance or runtime/Agent hook is created here.
func InvokeConfiguredHerdr(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, action ConfiguredAction, transport *http.Client, now time.Time) (QueueStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	q, err := OpenHerdrQueue(getenv, revision, limits)
	if err != nil {
		return QueueStatus{}, err
	}
	defer func() { _ = q.Close() }()
	if action.Kind == "enqueue-upload" || action.Kind == "enqueue-download" || action.Kind == "advance-next" {
		if err := q.ReclaimConfirmed(ctx); err != nil {
			return QueueStatus{}, err
		}
	}
	local := HerdrAction{Kind: action.Kind, DeliveryID: action.DeliveryID, Job: action.Job}
	if action.Kind == "advance-next" {
		id, err := q.nextDue(now)
		if err != nil {
			return QueueStatus{}, err
		}
		action.Kind, action.DeliveryID = "advance", id
		local.Kind, local.DeliveryID = "advance", id
	}
	switch action.Kind {
	case "inspect", "cancel", "retire":
		return q.invokeHerdr(ctx, local, nil, nil, nil, now)
	case "enqueue-upload", "enqueue-download", "advance", "reconcile":
	default:
		return QueueStatus{}, ErrInvalid
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		return QueueStatus{}, err
	}
	defer func() { _ = cfg.Close() }()
	if action.Kind == "enqueue-upload" || action.Kind == "enqueue-download" {
		if local.Job.HostBinding != "" && local.Job.HostBinding != cfg.Binding {
			return QueueStatus{}, ErrVersion
		}
		local.Job.HostBinding = cfg.Binding
		// Validate selectors without obtaining or transmitting any credential.
		direction := "upload"
		if action.Kind == "enqueue-download" {
			direction = "download"
		}
		c, err := NewDeviceTask(cfg.Origin, local.Job, direction, local.Job.Task, transport, cfg.Authorization)
		if err != nil {
			return QueueStatus{}, err
		}
		c.CloseIdleConnections()
		if existing, err := q.Inspect(action.DeliveryID); err == nil && existing.Admitted {
			// Admit rechecks immutable metadata under the job lock. A sealed
			// snapshot no longer depends on the producer file or directory.
			return q.invokeHerdr(ctx, local, nil, nil, nil, now)
		}
		var source *os.File
		if direction == "upload" {
			source, err = cfg.OpenSource(action.SourceRef)
			if err != nil {
				return QueueStatus{}, err
			}
			defer func() { _ = source.Close() }()
		} else if action.SourceRef != "" {
			return QueueStatus{}, ErrInvalid
		}
		return q.invokeHerdr(ctx, local, source, nil, nil, now)
	}
	status, err := q.Inspect(action.DeliveryID)
	if err != nil {
		return status, err
	}
	if status.Job.HostBinding == "" || status.Job.HostBinding != cfg.Binding {
		return QueueStatus{}, ErrVersion
	}
	c, err := NewDeviceTask(cfg.Origin, status.Job, status.Direction, status.Job.Task, transport, cfg.Authorization)
	if err != nil {
		return QueueStatus{}, err
	}
	defer c.CloseIdleConnections()
	binding, closeBinding, err := q.configuredBinding(c, status)
	if err != nil {
		return QueueStatus{}, err
	}
	defer closeBinding()
	return q.invokeHerdr(ctx, local, nil, c, binding, now)
}

// A single bounded selection, not a worker loop. Locks remain owned by RunOne;
// a race with another invocation returns busy and never runs duplicate work.
func (q *DiskQueue) nextDue(now time.Time) (string, error) {
	global, err := q.lock("admission")
	if err != nil {
		return "", err
	}
	defer unlockClose(global)
	records, err := q.admissions()
	if err != nil {
		return "", err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Job.DeliveryID < records[j].Job.DeliveryID })
	for _, admission := range records {
		r, err := q.Inspect(admission.Job.DeliveryID)
		if errors.Is(err, ErrBusy) || errors.Is(err, ErrAdmissionPending) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !r.Admitted || r.Associated || r.AssociationBlocked || r.Job.Phase == "blocked" || r.Job.Phase == "canceled" || r.Job.Phase == "create_unknown" || now.Before(r.Job.NextAttempt) || now.Before(r.AssociationDue) {
			continue
		}
		return r.Job.DeliveryID, nil
	}
	return "", ErrNoWork
}
