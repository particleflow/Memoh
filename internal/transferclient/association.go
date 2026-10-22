package transferclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	AssociationIdle        = "idle"
	AssociationInFlight    = "in_flight"
	AssociationUnknown     = "unknown"
	AssociationRetryCommit = "retry_commit"
	AssociationCommitted   = "committed"
	AssociationCanceled    = "canceled"
)

type AssociationOutcome string

const (
	OutcomeCommitted AssociationOutcome = "committed"
	OutcomeAbsent    AssociationOutcome = "absent"
	OutcomePending   AssociationOutcome = "pending"
	OutcomeCanceled  AssociationOutcome = "canceled"
)

// AssociationBinding uses the existing task/storage owner, not a second task
// service. Commit must be idempotent by delivery ID plus immutable reference.
// Resolve must reauthorize and inspect that same binding. When cancel is true,
// it must atomically observe committed OR fence the key against delayed commits
// before returning canceled. A plain absent lookup cannot confirm cancellation.
type AssociationBinding struct {
	Commit  Associate
	Resolve func(ctx context.Context, direction string, ref Reference, cancel bool) (AssociationOutcome, error)
}

func associationStateValid(state string) bool {
	switch state {
	case "", AssociationIdle, AssociationInFlight, AssociationUnknown, AssociationRetryCommit, AssociationCommitted, AssociationCanceled:
		return true
	default:
		return false
	}
}

func transientAssociation(err error) bool {
	if errors.Is(err, ErrBusy) || errors.Is(err, ErrTransport) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var h *HTTPError
	return errors.As(err, &h) && (h.Status == http.StatusRequestTimeout || h.Status == http.StatusTooManyRequests || h.Status >= 500 && h.Status <= 599)
}

func (q *DiskQueue) waitAssociation(f *os.File, r QueueStatus, err error, now time.Time) error {
	// Blocked pauses automatic attempts; unknown/retry_commit is retained.
	// It is never evidence that the server did not commit.
	r.AssociationBlocked = !transientAssociation(err) || r.AssociationAttempts >= 12
	if !r.AssociationBlocked {
		r.AssociationDue = now.Add(min(time.Second*time.Duration(1<<r.AssociationAttempts), 5*time.Minute))
	}
	return q.save(f, r)
}

func (q *DiskQueue) finishAssociation(f *os.File, r QueueStatus, outcome AssociationOutcome) error {
	r.AssociationDue = time.Time{}
	r.AssociationBlocked = false
	switch outcome {
	case OutcomeCommitted:
		r.Associated = true
		r.AssociationState = AssociationCommitted
	case OutcomeCanceled:
		r.AssociationState = AssociationCanceled
		r.Job.Phase = "canceled"
		r.Job.Ready = nil
	default:
		return ErrProtocol
	}
	return q.save(f, r)
}

func (q *DiskQueue) resolveAssociation(ctx context.Context, f *os.File, r QueueStatus, binding *AssociationBinding, now time.Time) error {
	if r.AssociationAttempts >= 12 {
		r.AssociationBlocked = true
		return q.save(f, r)
	}
	r.AssociationState = AssociationUnknown
	r.AssociationAttempts++
	if err := q.save(f, r); err != nil {
		return err
	}
	outcome, err := binding.Resolve(ctx, r.Direction, *r.Job.Ready, r.Job.CancelRequested)
	if err != nil {
		return q.waitAssociation(f, r, err, now)
	}
	switch outcome {
	case OutcomeCommitted, OutcomeCanceled:
		return q.finishAssociation(f, r, outcome)
	case OutcomeAbsent:
		// Even absent does not fence an old request still running remotely.
		if !r.Job.CancelRequested {
			r.AssociationState = AssociationRetryCommit
		}
	case OutcomePending:
	default:
		return q.waitAssociation(f, r, ErrProtocol, now)
	}
	return q.waitAssociation(f, r, ErrTransport, now)
}

func (q *DiskQueue) commitAssociation(ctx context.Context, f *os.File, r QueueStatus, binding *AssociationBinding, content io.Reader, now time.Time) error {
	if r.AssociationAttempts >= 12 {
		r.AssociationBlocked = true
		return q.save(f, r)
	}
	r.AssociationState = AssociationInFlight
	r.AssociationAttempts++
	if err := q.save(f, r); err != nil {
		return err
	}
	if err := binding.Commit(ctx, r.Direction, *r.Job.Ready, content); err != nil {
		r.AssociationState = AssociationUnknown
		return q.waitAssociation(f, r, err, now)
	}
	return q.finishAssociation(f, r, OutcomeCommitted)
}

// ResumeAssociation starts one new bounded reconciliation budget explicitly.
// It does not erase unknown outcomes or cancel intent, or confer authorization.
func (q *DiskQueue) ResumeAssociation(id string) error {
	if !validID(id) || id == "admission" {
		return ErrInvalid
	}
	lock, err := q.lock(id)
	if err != nil {
		return err
	}
	defer unlockClose(lock)
	r, f, err := q.load(id)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if r.Job.Phase != "ready" || r.Associated {
		return ErrInvalid
	}
	if r.AssociationState == AssociationInFlight || r.AssociationState == "" && r.AssociationAttempts > 0 {
		r.AssociationState = AssociationUnknown
	}
	r.AssociationAttempts = 0
	r.AssociationBlocked = false
	r.AssociationDue = time.Time{}
	return q.save(f, r)
}
