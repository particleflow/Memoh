package transferclient

import (
	"context"
	"errors"
	"net/http"
	"os"
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
	if action.Kind == "enqueue-download" || action.Kind == "advance-next" {
		if err := q.ReclaimConfirmed(ctx); err != nil {
			return QueueStatus{}, err
		}
	}
	local := HerdrAction{Kind: action.Kind, DeliveryID: action.DeliveryID, Job: action.Job}
	if action.Kind == "advance-snapshot" {
		q.snapshotOnly = true
		action.Kind, local.Kind = "advance", "advance"
	}
	if action.Kind == "advance-next" {
		cfg, err := OpenHostConfig(getenv)
		if err != nil {
			return QueueStatus{}, err
		}
		id, err := q.nextDueForBinding(now, cfg.Binding)
		_ = cfg.Close()
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
	if !q.snapshotOnly {
		q.openSource = cfg.OpenSource
	}
	if action.Kind == "enqueue-upload" || action.Kind == "enqueue-download" {
		return q.enqueueConfigured(ctx, cfg, action, transport, now)
	}
	status, err := q.Inspect(action.DeliveryID)
	if err != nil {
		return status, err
	}
	if status.Job.HostBinding == "" || status.Job.HostBinding != cfg.Binding {
		return QueueStatus{}, ErrVersion
	}
	c, err := cfg.newDeviceTask(status.Job, status.Direction, transport)
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

// One opened configuration/queue can admit a bounded producer page. The same
// validation and legacy recovery path are used by single-file callers.
func (q *DiskQueue) enqueueConfigured(ctx context.Context, cfg *HostConfig, action ConfiguredAction, transport *http.Client, now time.Time) (QueueStatus, error) {
	local := HerdrAction{Kind: action.Kind, DeliveryID: action.DeliveryID, Job: action.Job}
	var err error
	if local.Job.HostBinding != "" && local.Job.HostBinding != cfg.Binding {
		return QueueStatus{}, ErrVersion
	}
	local.Job.HostBinding = cfg.Binding
	// Validate selectors without obtaining or transmitting any credential.
	direction := "upload"
	if action.Kind == "enqueue-download" {
		direction = "download"
	}
	c, err := cfg.newDeviceTask(local.Job, direction, transport)
	if err != nil {
		return QueueStatus{}, err
	}
	c.CloseIdleConnections()
	if direction == "upload" {
		if action.DeliveryID != local.Job.DeliveryID {
			return QueueStatus{}, ErrInvalid
		}
		registered, err := q.RegisterUpload(ctx, local.Job, action.SourceRef, cfg.OpenSource)
		if err != nil || registered.Registered || registered.Admitted {
			return registered, err
		}
		// An old partially sealed admission keeps its existing recovery protocol.
	}
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

// A single bounded selection, not a worker loop. Locks remain owned by RunOne;
// a race with another invocation returns busy and never runs duplicate work.
func (q *DiskQueue) nextDue(now time.Time) (string, error) {
	return q.nextDueForBinding(now, "")
}

func (q *DiskQueue) nextDueForBinding(now time.Time, binding string) (string, error) {
	return q.nextDueMatching(now, binding, nil)
}

func (q *DiskQueue) nextDueMatching(now time.Time, binding string, include func(QueueStatus) bool) (string, error) {
	global, err := q.lock("admission")
	if err != nil {
		return "", err
	}
	defer unlockClose(global)
	var selected QueueStatus
	var selectedAt time.Time
	err = q.visitAdmissions(func(admission QueueStatus) error {
		r, err := q.Inspect(admission.Job.DeliveryID)
		if errors.Is(err, ErrBusy) || errors.Is(err, ErrAdmissionPending) {
			return nil
		}
		if err != nil {
			return err
		}
		if !recordEligible(r) || recordDue(r).After(now) || binding != "" && (r.Job.HostBinding != binding || !r.Job.Task.valid()) {
			return nil
		}
		if include != nil && !include(r) {
			return nil
		}
		at := recordDue(r)
		if r.LastRun.After(at) {
			at = r.LastRun
		}
		// Finish already activated files before opening another temporary index.
		active := r.Admitted || r.Activated
		selectedActive := selected.Admitted || selected.Activated
		if selected.Job.DeliveryID == "" || active && !selectedActive || active == selectedActive && (at.Before(selectedAt) || at.Equal(selectedAt) && r.Job.DeliveryID < selected.Job.DeliveryID) {
			selected, selectedAt = r, at
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if selected.Job.DeliveryID != "" {
		return selected.Job.DeliveryID, nil
	}
	return "", ErrNoWork
}
