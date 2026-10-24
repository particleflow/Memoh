package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// ErrProducerSource reports an unavailable source even if a previously sealed
// job advanced successfully. It must never be collapsed into an idle success.
var ErrProducerSource = errors.New("producer source collection unavailable")

// ErrProducerManifest carries no task identity or raw diagnostic. The collector
// could not safely attribute one or more retained metadata files to an attempt.
var ErrProducerManifest = errors.New("producer metadata collection contains invalid files")

// ProducerAdmission is the same bounded, non-secret stdin envelope accepted by
// enqueue-result. Metadata cannot supply credentials or override resolved roots.
func ProducerAdmission(input io.Reader, revision string) (ConfiguredAction, deliverymeta.Closed, error) {
	raw, err := io.ReadAll(io.LimitReader(input, 16385))
	if err != nil || len(raw) > 16384 {
		return ConfiguredAction{}, deliverymeta.Closed{}, ErrInvalid
	}
	var closed deliverymeta.Closed
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&closed) != nil || decoder.Decode(new(any)) != io.EOF || !closed.Valid() || !validID(revision) {
		return ConfiguredAction{}, closed, ErrInvalid
	}
	return producerAction(closed, revision), closed, nil
}

func producerAction(closed deliverymeta.Closed, revision string) ConfiguredAction {
	t := closed.Task
	id := closed.DeliveryID()
	job := Job{Version: 1, DeliveryID: id, SnapshotID: id, BotID: t.BotID, OperationID: t.OperationID, LocatorRevision: revision, Phase: "queued", Spec: Spec{SizeBytes: closed.SizeBytes, SHA256: closed.SHA256, Mime: closed.Artifact.Mime}, Task: DeviceTask{UserID: t.UserID, SessionID: t.SessionID, TargetID: t.TargetID, RunID: t.RunID, Attempt: t.Attempt, Generation: t.Generation}}
	return ConfiguredAction{Kind: "enqueue-upload", DeliveryID: id, Job: job, SourceRef: closed.Artifact.SourceRef}
}

func EnqueueProducer(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, input io.Reader, transport *http.Client, now time.Time) (QueueStatus, error) {
	action, closed, err := ProducerAdmission(input, revision)
	if err != nil {
		return QueueStatus{}, err
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		return QueueStatus{}, err
	}
	matches := cfg.sourcePath != "" && deliverymeta.RootHash(cfg.sourcePath) == closed.Task.RootHash
	// Bind the exact configuration used for the root-consistency check. The
	// existing admission boundary rejects a concurrent configuration change.
	action.Job.HostBinding = cfg.Binding
	_ = cfg.Close()
	if !matches {
		return QueueStatus{}, ErrBinding
	}
	status, err := InvokeConfiguredHerdr(ctx, getenv, revision, limits, action, transport, now)
	// Preserve correlation even when capacity rejects admission before a journal
	// exists. This is metadata, never evidence that admission succeeded.
	if status.Job.DeliveryID == "" {
		status.Job = action.Job
	}
	return status, err
}

// CollectProducer admits at most one closed manifest and performs bounded
// advances in the existing host action. The durable queue remains the only
// transfer state; the manifest is retained on backpressure/incomplete admission.
func CollectProducer(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, transport *http.Client, now time.Time) (QueueStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		return QueueStatus{}, err
	}
	defer func() { _ = cfg.Close() }()
	names, err := cfg.producerManifests()
	if err != nil {
		// Already admitted jobs own immutable snapshots and must not depend on
		// the continued existence of the producer directory. Preserve the source
		// error alongside bounded queue progress, including when no job is due.
		status, advanceErr := AdvanceProducer(ctx, getenv, revision, limits, transport, now)
		if errors.Is(advanceErr, ErrNoWork) {
			advanceErr = nil
		}
		return status, errors.Join(ErrProducerSource, err, advanceErr)
	}
	var collectionErr error
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return QueueStatus{}, errors.Join(collectionErr, err)
		}
		if strings.HasPrefix(name, deliverymeta.ProducerPagePrefix) {
			raw, readErr := cfg.readProducerPage(name)
			if readErr != nil {
				collectionErr = errors.Join(ErrProducerSource, ErrProducerManifest)
				continue
			}
			status, err := collectProducerPage(ctx, cfg, getenv, revision, limits, name, raw, transport, now)
			return status, errors.Join(collectionErr, err)
		}
		raw, err := cfg.readProducerManifest(name, revision)
		if err != nil {
			// Do not guess identity from malformed bytes or a mismatched filename.
			// Keep the evidence and inspect the next bounded directory candidate.
			collectionErr = errors.Join(ErrProducerSource, ErrProducerManifest)
			continue
		}
		status, err := collectProducerManifest(ctx, cfg, getenv, revision, limits, name, raw, transport, now)
		return status, errors.Join(collectionErr, err)
	}
	// Unattributable source errors must not starve already sealed jobs either.
	status, advanceErr := AdvanceProducer(ctx, getenv, revision, limits, transport, now)
	if collectionErr != nil && errors.Is(advanceErr, ErrNoWork) {
		advanceErr = nil
	}
	return status, errors.Join(collectionErr, advanceErr)
}

func (h *HostConfig) readProducerManifest(name, revision string) ([]byte, error) {
	file, err := h.OpenSource(name)
	if err != nil {
		return nil, ErrProducerManifest
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 16385))
	_ = file.Close()
	if readErr != nil || len(raw) > 16384 {
		return nil, ErrProducerManifest
	}
	_, closed, err := ProducerAdmission(bytes.NewReader(raw), revision)
	if err != nil || name != closed.ManifestName() || closed.Task.RootHash != deliverymeta.RootHash(h.sourcePath) {
		return nil, ErrProducerManifest
	}
	return raw, nil
}

func collectProducerManifest(ctx context.Context, cfg *HostConfig, getenv func(string) string, revision string, limits QueueLimits, name string, raw []byte, transport *http.Client, now time.Time) (QueueStatus, error) {
	// Goes through the exact stdin decoder and existing enqueue implementation.
	status, err := EnqueueProducer(ctx, getenv, revision, limits, bytes.NewReader(raw), transport, now)
	if errors.Is(err, ErrCapacity) || errors.Is(err, ErrBusy) {
		// Keep this manifest and let an already admitted delivery make progress.
		// Backpressure must not starve the very job that can release queue space.
		advanced, advanceErr := AdvanceProducer(ctx, getenv, revision, limits, transport, now)
		if advanceErr == nil {
			return advanced, nil
		}
		return status, err
	}
	if err != nil || !status.Admitted && !status.Registered {
		return status, err
	}
	// Admission fsync precedes consuming the manifest. Lost acknowledgement or a
	// crash before unlink replays the original ID and sealed snapshot, not Create.
	if err = cfg.sources.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return status, ErrStore
	}
	if err = syncQueueDir(cfg.sources); err != nil {
		return status, ErrStore
	}
	return advanceProducerID(ctx, getenv, revision, limits, status.Job.DeliveryID, transport, now)
}

func (h *HostConfig) producerManifests() ([]string, error) {
	if err := h.openSources(); err != nil {
		return nil, err
	}
	dir, err := h.sources.Open(".")
	if err != nil {
		return nil, ErrBinding
	}
	defer func() { _ = dir.Close() }()
	// Enumerate in bounded pages; directory size is not a submission quota.
	var names []string
	for {
		entries, err := dir.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, ErrStore
		}
		for _, entry := range entries {
			name := entry.Name()
			if validID(name) && (strings.HasPrefix(name, deliverymeta.ManifestPrefix) && len(name) == len(deliverymeta.ManifestPrefix)+64 || strings.HasPrefix(name, deliverymeta.ProducerPagePrefix) && len(name) == len(deliverymeta.ProducerPagePrefix)+64) {
				names = append(names, name)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	sort.Strings(names)
	return names, nil
}

func AdvanceProducer(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, transport *http.Client, now time.Time) (QueueStatus, error) {
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		return QueueStatus{}, err
	}
	binding := cfg.Binding
	_ = cfg.Close()
	q, err := OpenHerdrQueue(getenv, revision, limits)
	if err != nil {
		return QueueStatus{}, err
	}
	if err = q.ReclaimConfirmed(ctx); err != nil {
		_ = q.Close()
		return QueueStatus{}, err
	}
	id, err := q.nextDueForBinding(now, binding)
	_ = q.Close()
	if err != nil {
		return QueueStatus{}, err
	}
	return advanceProducerID(ctx, getenv, revision, limits, id, transport, now)
}

func advanceProducerID(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, id string, transport *http.Client, now time.Time) (QueueStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var status QueueStatus
	for range 64 {
		var err error
		status, err = InvokeConfiguredHerdr(ctx, getenv, revision, limits, ConfiguredAction{Kind: "advance", DeliveryID: id}, transport, now)
		if err != nil {
			if errors.Is(err, ErrAdmissionPending) {
				return status, nil
			}
			return status, err
		}
		if status.Associated || status.AssociationBlocked || status.Job.Phase == "blocked" || status.Job.Phase == "canceled" || status.Job.Phase == "create_unknown" || status.Job.NextAttempt.After(now) || status.AssociationDue.After(now) {
			return status, nil
		}
		if ctx.Err() != nil {
			return status, ctx.Err()
		}
		now = time.Now()
	}
	return status, nil
}
