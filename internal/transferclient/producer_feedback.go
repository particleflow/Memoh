package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
)

var ErrProducerFeedback = errors.New("producer feedback unavailable")

// PublishProducerFeedback writes a bounded advisory projection in the EXISTING
// attempt directory. It never creates directories, credentials, success refs or
// a second queue. Source/config/journal failures remain explicit to the caller.
func PublishProducerFeedback(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, job Job, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		return ErrProducerFeedback
	}
	defer func() { _ = cfg.Close() }()
	if cfg.sourcePath == "" || job.HostBinding != cfg.Binding {
		return ErrProducerFeedback
	}
	plan, root, contextFile, closePlan, err := cfg.sourcePlan(job.Task.Attempt)
	if err != nil {
		return ErrProducerFeedback
	}
	defer closePlan()
	if !feedbackTaskMatches(plan.Task, job) {
		return ErrProducerFeedback
	}
	boundID := false
	for _, file := range plan.FilePlans() {
		boundID = boundID || file.DeliveryID() == job.DeliveryID
	}
	if !boundID {
		return ErrProducerFeedback
	}
	q, err := OpenHerdrQueue(getenv, revision, limits)
	if err != nil {
		return ErrProducerFeedback
	}
	defer func() { _ = q.Close() }()
	global, err := q.lock("admission")
	if err != nil {
		return err
	}
	defer unlockClose(global)
	report, err := q.producerFeedback(ctx, cfg, plan, now)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(report)
	if err != nil || len(raw) >= deliverymeta.FeedbackMaxBytes || !report.ValidFor(plan) {
		return ErrProducerFeedback
	}
	// Concurrent publishers share the existing admission lock. Readers reject a
	// partial JSON document after process death; the authoritative queue is intact.
	// The original attempt context is held while projecting. A replaced context
	// must not redirect an advisory write, including on Unix test hosts.
	current, statErr := root.Lstat(deliverymeta.ProducerContextName)
	opened, openedErr := contextFile.Stat()
	contextRaw, readErr := sourceBytes(contextFile, 16384)
	var currentPlan deliverymeta.Plan
	if statErr != nil || openedErr != nil || !os.SameFile(current, opened) || readErr != nil || json.Unmarshal(contextRaw, &currentPlan) != nil || deliverymeta.PlanHash(currentPlan) != deliverymeta.PlanHash(plan) {
		return ErrProducerFeedback
	}
	f, err := openSourceFile(root, deliverymeta.ProducerFeedbackName, os.O_CREATE|os.O_RDWR, false)
	if err != nil {
		return ErrProducerFeedback
	}
	defer func() { _ = f.Close() }()
	if f.Truncate(0) != nil {
		return ErrProducerFeedback
	}
	if n, err := f.Write(raw); err != nil || n != len(raw) || f.Sync() != nil || syncSourceDir(root) != nil {
		return ErrProducerFeedback
	}
	return nil
}

func feedbackTaskMatches(t deliverymeta.Task, j Job) bool {
	return t.OperationID == j.OperationID && t.BotID == j.BotID && t.UserID == j.Task.UserID && t.SessionID == j.Task.SessionID && t.TargetID == j.Task.TargetID && t.RunID == j.Task.RunID && t.Attempt == j.Task.Attempt && t.Generation == j.Task.Generation
}

func feedbackChange(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func queueFeedback(r QueueStatus, binding string, now time.Time) deliverymeta.FileFeedback {
	f := deliverymeta.FileFeedback{DeliveryID: r.Job.DeliveryID, State: "pending", Code: "queued", Retryable: true, Change: feedbackChange(r)}
	switch {
	case r.Job.HostBinding != binding:
		f.State, f.Code, f.Retryable = "blocked", "configuration_changed", false
	case r.Associated:
		f.State, f.Code, f.Retryable = "unknown", "awaiting_nas_ledger", false
	case r.AssociationBlocked:
		f.State, f.Code, f.Retryable = "unknown", "association_requires_reconciliation", false
	case r.Job.Phase == "blocked" && r.Job.ErrorCode == "receipt_unavailable":
		f.State, f.Code, f.Retryable = "unknown", "receipt_unavailable", false
	case r.Job.Phase == "blocked":
		f.State, f.Code, f.Retryable = "blocked", r.Job.ErrorCode, false
	case r.Job.Phase == "create_unknown" || r.Job.Phase == "creating":
		f.State, f.Code, f.Retryable = "unknown", "create_outcome_unknown", false
	case r.Job.Phase == "canceled":
		f.State, f.Code, f.Retryable = "unknown", "local_canceled", false
	case !r.Admitted:
		f.Code = "admission_pending"
	case r.AssociationDue.After(now):
		f.Code, f.RetryAt = "retry_backoff", r.AssociationDue
	case r.Job.NextAttempt.After(now):
		f.Code, f.RetryAt = "retry_backoff", r.Job.NextAttempt
	case r.Job.Phase == "ready":
		f.Code = "association_pending"
	case r.Job.Phase == "uploading":
		f.Code = "uploading"
	}
	if !f.Valid() {
		f.State, f.Code, f.Retryable, f.RetryAt = "blocked", "protocol", false, time.Time{}
	}
	return f
}

func (q *DiskQueue) producerFeedback(ctx context.Context, cfg *HostConfig, plan deliverymeta.Plan, now time.Time) (deliverymeta.Feedback, error) {
	report := deliverymeta.Feedback{Version: 1, Task: plan.Task, PlanHash: deliverymeta.PlanHash(plan)}
	// Caller holds the admission lock through projection and file synchronization.
	if err := cfg.openSources(); err != nil {
		return report, ErrProducerFeedback
	}
	// Only the declared page is projected. Do not materialize the entire queue
	// to report a handful of files; global queue health belongs to lifecycle.
	for _, file := range plan.FilePlans() {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		r, err := q.Inspect(file.DeliveryID())
		if err == nil || errors.Is(err, ErrAdmissionPending) {
			if !feedbackTaskMatches(plan.Task, r.Job) || r.Job.Spec.Mime != file.Artifact.Mime {
				return report, ErrProducerFeedback
			}
			report.Files = append(report.Files, queueFeedback(r, cfg.Binding, now))
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			code := "queue_unavailable"
			if errors.Is(err, ErrBusy) {
				code = "queue_busy"
			}
			report.Files = append(report.Files, deliverymeta.FileFeedback{DeliveryID: file.DeliveryID(), State: "pending", Code: code, Retryable: true, RetryAt: now.Add(30 * time.Second), Change: feedbackChange(code)})
			continue
		}
		local := deliverymeta.FileFeedback{DeliveryID: file.DeliveryID(), State: "unknown", Code: "closure_or_admission_unknown", Change: feedbackChange(file)}
		_, existsErr := cfg.sources.Lstat(file.ManifestName())
		source, err := cfg.OpenSource(file.ManifestName())
		if existsErr == nil && err != nil {
			local.State, local.Code = "blocked", "manifest_invalid"
		}
		if existsErr != nil && !errors.Is(existsErr, os.ErrNotExist) {
			return report, ErrProducerFeedback
		}
		if err == nil {
			raw, readErr := io.ReadAll(io.LimitReader(source, 16385))
			_ = source.Close()
			_, closed, parseErr := ProducerAdmission(bytes.NewReader(raw), q.revision)
			if readErr != nil || parseErr != nil || closed.Task != plan.Task || closed.Artifact != file.Artifact {
				local.State, local.Code = "blocked", "manifest_invalid"
			} else {
				local.State, local.Code, local.Retryable = "pending", "queued", true
				local.Change = feedbackChange(closed)

			}
		}
		report.Files = append(report.Files, local)
	}
	return report, nil
}
