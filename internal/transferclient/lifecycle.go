package transferclient

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

// Lifecycle is a bounded projection of this queue, never delivery evidence or
// authorization. It contains no path, credential, task ID or private journal.
type Lifecycle struct {
	Schema     string     `json:"schema"`
	State      string     `json:"state"`
	NextDueAt  *time.Time `json:"next_due_at"`
	ErrorClass string     `json:"error_class"`
}

func lifecycle(state, class string, due time.Time) Lifecycle {
	r := Lifecycle{Schema: "memoh.delivery.lifecycle.v1", State: state, ErrorClass: class}
	if !due.IsZero() {
		utc := due.UTC()
		r.NextDueAt = &utc
	}
	return r
}

func lifecycleErrorClass(err error) string {
	var h *HTTPError
	switch {
	case errors.Is(err, ErrAuthorization):
		return "authentication"
	case errors.As(err, &h) && (h.Status == http.StatusUnauthorized || h.Status == http.StatusForbidden):
		return "authentication"
	case errors.Is(err, ErrPermission), errors.Is(err, os.ErrPermission):
		return "permission"
	case errors.Is(err, ErrBusy):
		return "busy"
	case errors.Is(err, ErrVersion), errors.Is(err, ErrBinding), errors.Is(err, ErrInvalid):
		return "configuration"
	case errors.Is(err, ErrCapacity):
		return "capacity"
	case transientAssociation(err):
		return "transient"
	default:
		return "unknown"
	}
}

// LifecycleFailure never manufactures a retry deadline from a process error.
// Busy/transient without a persisted deadline permit one host retry after 30s
// per external event; another such result suspends wakes until a new event.
func LifecycleFailure(err error) Lifecycle {
	class := lifecycleErrorClass(err)
	state := "blocked"
	if class == "unknown" || class == "busy" || class == "transient" {
		state = "unknown"
	}
	return lifecycle(state, class, time.Time{})
}

// LifecycleSummary opens ONLY existing storage and reads configuration once.
// It never initializes a locator, syncs/truncates a journal, admits, reclaims,
// changes a retry budget, makes a network request or creates a credential.
func LifecycleSummary(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, now time.Time) (Lifecycle, error) {
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		return LifecycleFailure(err), err
	}
	defer func() { _ = cfg.Close() }()
	// Local syntax/presence only: remote authorization remains checked per request.
	if _, err = cfg.Authorization(ctx); err != nil {
		return LifecycleFailure(err), err
	}
	q, err := openHerdrQueue(getenv, revision, limits, true)
	if err != nil {
		return LifecycleFailure(err), err
	}
	defer func() { _ = q.Close() }()
	return q.lifecycleSummary(ctx, cfg.Binding, now)
}

func (q *DiskQueue) lifecycleSummary(ctx context.Context, binding string, now time.Time) (Lifecycle, error) {
	global, err := q.lock("admission")
	if err != nil {
		return LifecycleFailure(err), err
	}
	defer unlockClose(global)
	result := lifecycle("idle", "none", time.Time{})
	err = q.visitAdmissions(func(head QueueStatus) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		r, err := q.Inspect(head.Job.DeliveryID)
		var next Lifecycle
		if err != nil && !errors.Is(err, ErrAdmissionPending) {
			if !errors.Is(err, ErrBusy) {
				return err
			}
			next = LifecycleFailure(err)
		} else {
			next = recordLifecycle(r, binding, now)
		}
		result = combineLifecycle(result, next)
		return nil
	})
	if err != nil {
		return LifecycleFailure(err), err
	}
	return result, nil
}

func recordDue(r QueueStatus) time.Time {
	if r.Job.NextAttempt.After(r.AssociationDue) {
		return r.Job.NextAttempt
	}
	return r.AssociationDue
}

func recordEligible(r QueueStatus) bool {
	return (r.Admitted || r.Registered) && !r.Associated && !r.AssociationBlocked && r.Job.Phase != "blocked" && r.Job.Phase != "canceled" && r.Job.Phase != "create_unknown" && r.Job.Phase != "creating"
}

func recordLifecycle(r QueueStatus, binding string, now time.Time) Lifecycle {
	switch {
	case r.Job.HostBinding != binding || !r.Job.Task.valid():
		return lifecycle("blocked", "configuration", time.Time{})
	case r.Associated:
		if r.PayloadState != PayloadReleased {
			return lifecycle("due", "none", time.Time{})
		}
		return lifecycle("idle", "none", time.Time{})
	case r.AssociationBlocked:
		class := r.AssociationError
		if class == "" || class == "transient" || class == "busy" {
			class = "unknown"
		}
		return lifecycle("unknown", class, time.Time{})
	case r.Job.Phase == "blocked" && r.Job.ErrorCode == "receipt_unavailable":
		// Missing staging metadata is not proof that publication never happened.
		return lifecycle("unknown", "unknown", time.Time{})
	case r.Job.Phase == "blocked":
		class := "unknown"
		switch r.Job.ErrorCode {
		case "authorization":
			class = "authentication"
		case "file_limit", "capacity_blocked":
			class = "capacity"
		case "configuration_changed", "resumable_unavailable":
			class = "configuration"
		}
		return lifecycle("blocked", class, time.Time{})
	case !recordEligible(r):
		return lifecycle("unknown", "unknown", time.Time{})
	}
	due := recordDue(r)
	if due.After(now) {
		if r.Job.ErrorCode == "capacity_wait" || r.AssociationError == "capacity" {
			return lifecycle("waiting", "capacity", due)
		}
		return lifecycle("waiting", "transient", due)
	}
	return lifecycle("due", "none", time.Time{})
}

func combineLifecycle(a, b Lifecycle) Lifecycle {
	// Independent eligible work must not be hidden by an unrelated refusal.
	rank := map[string]int{"idle": 0, "blocked": 1, "unknown": 2, "waiting": 3, "due": 4}
	if rank[b.State] > rank[a.State] {
		return b
	}
	if a.State == "waiting" && b.State == "waiting" && b.NextDueAt.Before(*a.NextDueAt) {
		return b
	}
	return a
}

// Same validated recovery witnesses as load, with no repair or fsync. The caller
// holds the existing job lock, so compaction cannot race these reads.
func (q *DiskQueue) readOnlyRecord(id string) (QueueStatus, *os.File, error) {
	witness, err := q.activeCheckpointHead(queueName(id + ".checkpoint"))
	if err == nil {
		f, err := q.file(id+".journal", os.O_RDONLY, 0o600)
		return witness.Record, f, err
	}
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errCheckpointIncomplete) {
		return QueueStatus{}, nil, err
	}
	r, f, journalErr := q.loadJournal(id)
	if journalErr == nil {
		if errors.Is(err, errCheckpointIncomplete) && r.Version != 4 && r.Version != 5 {
			_ = f.Close()
			return QueueStatus{}, nil, ErrStore
		}
		return r, f, nil
	}
	if errors.Is(err, errCheckpointIncomplete) || errors.Is(journalErr, ErrVersion) {
		return QueueStatus{}, nil, journalErr
	}
	return q.loadReleaseReceipt(id)
}
