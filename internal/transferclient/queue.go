package transferclient

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"
)

const maxRetryDelay = 5 * time.Minute

var (
	ErrCheckpoint = errors.New("delivery checkpoint failed")
	ErrSnapshot   = errors.New("frozen delivery snapshot unavailable")
)

// Job is a versioned host-store record, not a model message. Task identifiers
// are correlation only until the host's authorized registration endpoint binds
// them on the server. No secret or physical filesystem path belongs here.
type Job struct {
	Task            DeviceTask `json:"device_task,omitzero"`
	HostBinding     string     `json:"host_binding,omitempty"`
	Version         int        `json:"version"`
	DeliveryID      string     `json:"delivery_id"`
	BotID           string     `json:"bot_id"`
	OperationID     string     `json:"operation_id"`
	SnapshotID      string     `json:"snapshot_id"`
	LocatorRevision string     `json:"locator_revision"`
	Spec            Spec       `json:"spec"`
	Phase           string     `json:"phase"`
	ReceiptID       string     `json:"receipt_id,omitempty"`
	Offset          int64      `json:"offset"`
	ExpiresAt       time.Time  `json:"expires_at,omitempty"`
	ChunkBytes      int        `json:"chunk_bytes"`
	CancelRequested bool       `json:"cancel_requested"`
	Attempts        int        `json:"attempts"`
	NextAttempt     time.Time  `json:"next_attempt,omitempty"`
	ErrorCode       string     `json:"error_code,omitempty"`
	Ready           *Reference `json:"ready,omitempty"`
}

// Reference is the bounded ready payload for the host's durable result outbox.
// It is not proof that an operation succeeded or that association is authorized.
type Reference struct {
	DeliveryID  string `json:"delivery_id"`
	OperationID string `json:"operation_id"`
	BotID       string `json:"bot_id"`
	ContentHash string `json:"content_hash"`
	SizeBytes   int64  `json:"size_bytes"`
	Mime        string `json:"mime"`
}

// Save commits a complete Job atomically and durably, or returns an error.
// The caller holds its existing cross-process job lock for all of Advance,
// including network requests. Admission, locking, fencing and persistence are
// host responsibilities; a memory callback does not constitute a durable queue.
type Save func(Job) error

// Snapshot reads exactly len(dst) bytes from a host-owned immutable snapshot.
// The host checks the locator revision and holds a stable, authorized handle;
// ref must never be resolved with unchecked concatenation or fallback paths.
// On reopen it must verify identity/size/hash, reject links/reparse points, and
// prevent source mutation for the lifetime of the read. The adapter cannot
// establish OS containment from a string or a size/mtime check.
type Snapshot interface {
	ReadAt(ctx context.Context, ref, locatorRevision string, spec Spec, dst []byte, offset int64) error
}

func (j Job) valid() bool {
	if j.Version != 1 || !validID(j.DeliveryID) || !validID(j.BotID) || !validID(j.OperationID) || !validID(j.SnapshotID) || !validID(j.LocatorRevision) || !j.Spec.valid() || j.Offset < 0 || j.Offset > j.Spec.SizeBytes || j.Attempts < 0 || j.Attempts > 12 {
		return false
	}
	if j.ReceiptID != "" && !validID(j.ReceiptID) {
		return false
	}
	if j.ChunkBytes < 0 || j.ChunkBytes > MaxChunkBytes {
		return false
	}
	switch j.Phase {
	case "queued", "creating", "create_unknown", "blocked", "canceled":
		return j.Ready == nil
	case "uploading":
		return j.ReceiptID != "" && j.ChunkBytes > 0 && j.Ready == nil
	case "ready":
		return j.Ready != nil && j.Ready.ContentHash == j.Spec.SHA256 && j.Ready.BotID == j.BotID && j.Ready.OperationID == j.OperationID && j.Ready.DeliveryID == j.DeliveryID && j.Ready.SizeBytes == j.Spec.SizeBytes && j.Ready.Mime == j.Spec.Mime
	default:
		return false
	}
}

func persist(j *Job, next Job, save Save) error {
	if err := save(next); err != nil {
		return ErrCheckpoint
	}
	*j = next
	return nil
}

func stop(j *Job, phase, code string, save Save) error {
	next := *j
	next.Phase, next.ErrorCode, next.NextAttempt = phase, code, time.Time{}
	return persist(j, next, save)
}

func fail(j *Job, err error, now time.Time, save Save) error {
	if errors.Is(err, ErrAuthorization) {
		return stop(j, "blocked", "authorization", save)
	}
	var h *HTTPError
	code := "protocol"
	retry := errors.Is(err, ErrTransport) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	if retry {
		code = "connection"
	}
	if errors.As(err, &h) {
		switch h.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			retry, code = false, "authorization"
		case statusRequestCanceled:
			retry, code = true, "connection"
		case http.StatusNotFound:
			code = "receipt_unavailable"
		case http.StatusBadRequest:
			code = "input_or_checksum"
		case http.StatusRequestEntityTooLarge:
			code = "file_limit"
		case http.StatusConflict, http.StatusTooManyRequests, http.StatusRequestTimeout:
			retry, code = true, "retryable_http"
		default:
			retry = h.Status >= 500 && h.Status <= 599
			if retry {
				code = "server_unavailable"
			}
		}
	}
	if !retry {
		return stop(j, "blocked", code, save)
	}
	next := *j
	next.Attempts++
	if next.Attempts >= 12 {
		return stop(j, "blocked", "retry_budget", save)
	}
	ceiling := min(time.Second*time.Duration(1<<min(next.Attempts, 9)), maxRetryDelay)
	// Bounded jitter, persisted by the host; Advance never sleeps or schedules.
	delay := ceiling/2 + time.Duration(rand.Int64N(int64(ceiling/2))) //nolint:gosec // Retry jitter has no security/identity role.
	if h != nil && h.RetryAfter > delay {
		delay = min(h.RetryAfter, maxRetryDelay)
	}
	next.NextAttempt, next.ErrorCode = now.Add(delay), code
	return persist(j, next, save)
}

func receiptValid(j Job, r Receipt) bool {
	if !validID(r.ID) || j.ReceiptID != "" && r.ID != j.ReceiptID || r.SizeBytes != j.Spec.SizeBytes || r.Mime != j.Spec.Mime || r.Offset < 0 || r.Offset > r.SizeBytes || r.ExpiresAt.IsZero() {
		return false
	}
	switch r.State {
	case "uploading", "canceled":
		return r.ContentHash == ""
	case "ready":
		return r.ContentHash == j.Spec.SHA256 && r.Offset == r.SizeBytes
	default:
		return false
	}
}

func accept(j *Job, r Receipt, save Save) error {
	if !receiptValid(*j, r) {
		return stop(j, "blocked", "receipt_mismatch", save)
	}
	next := *j
	next.ReceiptID, next.Offset, next.ExpiresAt, next.Phase = r.ID, r.Offset, r.ExpiresAt, r.State
	if r.State == "ready" {
		next.Ready = &Reference{DeliveryID: j.DeliveryID, OperationID: j.OperationID, BotID: j.BotID, ContentHash: r.ContentHash, SizeBytes: r.SizeBytes, Mime: r.Mime}
	}
	if r.State == "ready" || r.State == "canceled" {
		next.Attempts, next.ErrorCode, next.NextAttempt = 0, "", time.Time{}
	}
	return persist(j, next, save)
}

// Advance performs at most one new chunk or completion/cancellation mutation.
// It always reconciles a known receipt first. Host code owns admission, source
// freezing, persisted cancel intent, due-job selection and result registration.
// A nil error means the checkpoint succeeded, not that delivery is ready.
func (c *Client) Advance(ctx context.Context, job Job, save Save, source Snapshot, now time.Time) error {
	if !job.valid() || save == nil {
		return ErrInvalid
	}
	j := &job
	if j.Phase == "ready" || j.Phase == "canceled" || j.Phase == "create_unknown" || j.Phase == "blocked" {
		return nil
	}
	// A prior create may have reached the server before a process crash.
	if j.Phase == "creating" {
		return stop(j, "create_unknown", "create_outcome_unknown", save)
	}
	if j.CancelRequested && j.ReceiptID == "" {
		return stop(j, "canceled", "", save)
	}
	if now.Before(j.NextAttempt) {
		return nil
	}
	if j.Phase == "queued" {
		if j.ReceiptID != "" {
			return ErrInvalid
		}
		limits, err := c.Limits(ctx, j.BotID)
		if err != nil {
			return fail(j, err, now, save)
		}
		if !limits.ResumableEnabled {
			return stop(j, "blocked", "resumable_unavailable", save)
		}
		if j.Spec.SizeBytes > limits.MaxUploadBytes {
			return stop(j, "blocked", "file_limit", save)
		}
		next := *j
		next.ChunkBytes = int(limits.ChunkMaxBytes)
		if j.ChunkBytes > 0 {
			next.ChunkBytes = min(j.ChunkBytes, next.ChunkBytes)
		}
		next.Phase = "creating"
		if err := persist(j, next, save); err != nil {
			return err
		}
		r, err := c.Create(ctx, j.BotID, j.Spec)
		if err != nil {
			var h *HTTPError
			if errors.Is(err, ErrAuthorization) || errors.As(err, &h) && h.Status >= 400 && h.Status < 500 && h.Status != http.StatusRequestTimeout && h.Status != statusRequestCanceled {
				// A definite rejection can be retried according to its status.
				if err := stop(j, "queued", "", save); err != nil {
					return err
				}
				return fail(j, err, now, save)
			}
			return stop(j, "create_unknown", "create_outcome_unknown", save)
		}
		if !receiptValid(*j, r) || r.State != "uploading" || r.Offset != 0 {
			return stop(j, "create_unknown", "create_outcome_unknown", save)
		}
		return accept(j, r, save)
	}
	// No PATCH/complete replay from cached offsets, even after a clean restart.
	r, err := c.Get(ctx, j.BotID, j.ReceiptID)
	if err != nil {
		return fail(j, err, now, save)
	}
	if err := accept(j, r, save); err != nil {
		return err
	}
	if j.Phase != "uploading" {
		return nil
	}
	switch {
	case j.CancelRequested:
		r, err = c.Cancel(ctx, j.BotID, j.ReceiptID)
		if err == nil && r.State != "canceled" {
			err = ErrProtocol
		}
	case j.Offset == j.Spec.SizeBytes:
		r, err = c.Complete(ctx, j.BotID, j.ReceiptID)
		if err == nil && r.State != "ready" {
			err = ErrProtocol
		}
	default:
		body := make([]byte, int(min(int64(j.ChunkBytes), j.Spec.SizeBytes-j.Offset)))
		if source == nil {
			return stop(j, "blocked", "snapshot_unavailable", save)
		}
		if sourceErr := source.ReadAt(ctx, j.SnapshotID, j.LocatorRevision, j.Spec, body, j.Offset); sourceErr != nil {
			if errors.Is(sourceErr, context.Canceled) || errors.Is(sourceErr, context.DeadlineExceeded) {
				return fail(j, sourceErr, now, save)
			}
			return stop(j, "blocked", "snapshot_unavailable", save)
		}
		r, err = c.Patch(ctx, j.BotID, j.ReceiptID, j.Offset, body)
		if err == nil && (r.State != "uploading" || r.Offset != j.Offset+int64(len(body))) {
			err = ErrProtocol
		}
	}
	if err != nil {
		return fail(j, err, now, save)
	}
	if err := accept(j, r, save); err != nil {
		return err
	}
	if j.Phase == "blocked" {
		return nil
	}
	next := *j
	next.Attempts, next.ErrorCode, next.NextAttempt = 0, "", time.Time{}
	return persist(j, next, save)
}
