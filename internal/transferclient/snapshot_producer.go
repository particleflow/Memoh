package transferclient

import (
	"context"
	"net/http"
	"time"
)

func retainedUploadSnapshot(r QueueStatus) bool {
	return r.Direction == "upload" && r.Admitted && !r.Registered && r.Source == nil && r.Seal.valid() && r.PayloadState == ""
}

// AdvanceSnapshotProducer is a narrower capability than AdvanceProducer. It
// cannot collect/admit, activate a registered source, index live files, download
// or execute Herdr/Pi. Only an already-admitted retained private upload snapshot
// with this exact HostBinding and original device task may advance. RunOne
// rechecks the capability and snapshot handles under its job lock every time.
// No global reclaim, credential fallback or source-root read is performed.
func AdvanceSnapshotProducer(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, transport *http.Client, now time.Time) (QueueStatus, error) {
	if err := ctx.Err(); err != nil {
		return QueueStatus{}, err
	}
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
	id, err := q.nextDueMatching(now, binding, retainedUploadSnapshot)
	_ = q.Close()
	if err != nil {
		return QueueStatus{}, err
	}
	return advanceProducerIDWithKind(ctx, getenv, revision, limits, id, "advance-snapshot", transport, now)
}
