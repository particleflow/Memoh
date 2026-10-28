package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// ResultSourceMapping belongs to the existing private job journal. It is not a
// replacement task/context, a source manifest, or an upload authorization.
type ResultSourceMapping struct {
	Request            deliverymeta.ResultRecovery `json:"request"`
	Source             QueuedSource                `json:"source"`
	ContextIdentity    string                      `json:"context_identity"`
	ReceiptIdentity    string                      `json:"receipt_identity"`
	CompletionIdentity string                      `json:"completion_identity"`
	Digest             string                      `json:"digest"`
}

func (m ResultSourceMapping) digest() string {
	m.Digest = ""
	return feedbackChange(m)
}

func (m ResultSourceMapping) validFor(job Job, source *QueuedSource) bool {
	p, ok := m.Request.FilePlan()
	if !ok || source == nil || *source != m.Source || source.Ref != m.Request.PhysicalName || !source.valid() || m.Digest != m.digest() {
		return false
	}
	want := producerAction(deliverymeta.Closed{Plan: p, Closed: true, SHA256: m.Request.SHA256, SizeBytes: m.Request.SizeBytes}, job.LocatorRevision).Job
	return sameAdmission(want, withoutRecoveryBinding(job)) && job.HostBinding == m.Request.HostBinding &&
		m.ContextIdentity != "" && m.ReceiptIdentity != "" && m.CompletionIdentity != ""
}

func withoutRecoveryBinding(job Job) Job {
	job.HostBinding = ""
	return job
}

func sameResultMapping(a, b *ResultSourceMapping) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Digest == b.Digest && a.digest() == b.digest()
}

func sameRecoveryRequest(a, b deliverymeta.ResultRecovery) bool {
	a.RemoteOutcome, b.RemoteOutcome, a.KnownReceipt, b.KnownReceipt = "", "", "", ""
	return feedbackChange(a) == feedbackChange(b)
}

// RecoveryObservation is internal host metadata. ReceiptObserved never means
// mapping/admission/commit happened; the CLI emits only bounded status fields.
type RecoveryObservation struct {
	Queue           QueueStatus
	ReceiptObserved *Receipt
}

// RecoverResultSource is an explicit finite host entry. Input must come from
// the authenticated original operation owner (PrepareProducerResultRecovery),
// never model arguments. It registers metadata only; it NEVER creates, appends,
// completes, cancels or commits an upload, nor dispatches Pi. Existing receipt
// work is strictly Get-only and cannot change a mapping or journal.
func RecoverResultSource(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, request deliverymeta.ResultRecovery, transport *http.Client) (RecoveryObservation, error) {
	return recoverResultSource(ctx, getenv, revision, limits, request, transport, nil)
}

func recoverResultSource(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, request deliverymeta.ResultRecovery, transport *http.Client, hook func(string)) (RecoveryObservation, error) {
	p, ok := request.FilePlan()
	if !ok || request.KnownReceipt != "" && !validID(request.KnownReceipt) || ctx.Err() != nil {
		return RecoveryObservation{}, ErrInvalid
	}
	canonical, err := canonicalRecoveryJSON(request.Receipt)
	if err != nil {
		return RecoveryObservation{}, ErrInvalid
	}
	request.Receipt = canonical
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		return RecoveryObservation{}, err
	}
	defer func() { _ = cfg.Close() }()
	if cfg.Binding != request.HostBinding || deliverymeta.RootHash(cfg.sourcePath) != request.Plan.Task.RootHash {
		return RecoveryObservation{}, ErrBinding
	}
	q, err := OpenHerdrQueue(getenv, revision, limits)
	if err != nil {
		return RecoveryObservation{}, err
	}
	defer func() { _ = q.Close() }()
	q.journalHook = hook
	job := producerAction(deliverymeta.Closed{Plan: p, Closed: true, SHA256: request.SHA256, SizeBytes: request.SizeBytes}, revision).Job
	job.HostBinding = cfg.Binding
	// Serializes against first admission and every worker on this original ID.
	global, err := q.lock("admission")
	if err != nil {
		return RecoveryObservation{}, err
	}
	lock, err := q.lock(job.DeliveryID)
	if err != nil {
		unlockClose(global)
		return RecoveryObservation{}, err
	}
	r, journal, inspectErr := q.load(job.DeliveryID)
	if journal != nil {
		_ = journal.Close()
	}
	// Never infer a missing or corrupt queue/history record from a failed read.
	if inspectErr != nil && !errors.Is(inspectErr, os.ErrNotExist) {
		unlockClose(lock)
		unlockClose(global)
		return RecoveryObservation{}, inspectErr
	}
	if inspectErr == nil {
		if r.Direction != "upload" || !sameAdmission(r.Job, job) || r.Recovery != nil && !sameRecoveryRequest(r.Recovery.Request, request) {
			unlockClose(lock)
			unlockClose(global)
			return RecoveryObservation{}, ErrInvalid
		}
	}
	knownReceipt := request.KnownReceipt
	if inspectErr == nil && r.Job.ReceiptID != "" {
		if knownReceipt != "" && knownReceipt != r.Job.ReceiptID {
			unlockClose(lock)
			unlockClose(global)
			return RecoveryObservation{Queue: r}, ErrBinding
		}
		knownReceipt = r.Job.ReceiptID
	}
	if knownReceipt != "" {
		// The current HTTP GET guard reserves an absent NAS ledger entry. Only
		// an owner-confirmed existing binding is safe for read-only reconciliation.
		if request.RemoteOutcome != "existing" || request.KnownReceipt != knownReceipt {
			unlockClose(lock)
			unlockClose(global)
			return RecoveryObservation{Queue: r}, ErrBinding
		}
		// Hold original job lock through readback. No RunOne/Reconcile/Resolve:
		// even an unknown association cannot cause a POST in this entry.
		c, err := cfg.newDeviceTask(job, "upload", transport)
		var receipt Receipt
		if err == nil {
			receipt, err = c.Get(ctx, job.BotID, knownReceipt)
			c.CloseIdleConnections()
			if err == nil && (receipt.ID != knownReceipt || receipt.SizeBytes != job.Spec.SizeBytes || receipt.Mime != job.Spec.Mime || receipt.Offset < 0 || receipt.Offset > job.Spec.SizeBytes || receipt.ContentHash != "" && receipt.ContentHash != job.Spec.SHA256) {
				err = ErrProtocol
			}
		}
		unlockClose(lock)
		unlockClose(global)
		if err != nil {
			return RecoveryObservation{Queue: r}, err
		}
		return RecoveryObservation{Queue: r, ReceiptObserved: &receipt}, nil
	}
	if request.RemoteOutcome != "absent" {
		unlockClose(lock)
		unlockClose(global)
		return RecoveryObservation{Queue: r}, ErrBinding
	}
	if inspectErr == nil {
		if r.Recovery == nil || r.Job.Phase != "queued" || r.Job.CancelRequested || r.AssociationState != AssociationIdle || r.AssociationAttempts != 0 {
			unlockClose(lock)
			unlockClose(global)
			return RecoveryObservation{Queue: r}, ErrBinding
		}
	}
	// Do not manufacture closure while an original manifest may already be in
	// flight. Its existing collector/receipt protocol owns that outcome.
	if inspectErr != nil {
		if err := cfg.openSources(); err != nil {
			unlockClose(lock)
			unlockClose(global)
			return RecoveryObservation{}, err
		}
		if _, err := cfg.sources.Lstat(p.ManifestName()); !errors.Is(err, os.ErrNotExist) {
			unlockClose(lock)
			unlockClose(global)
			return RecoveryObservation{}, ErrBinding
		}
	}
	mapping, file, err := cfg.validateResultRecovery(ctx, request, true)
	if file != nil {
		_ = file.Close()
	}
	if err == nil && r.Recovery != nil && !sameResultMapping(r.Recovery, &mapping) {
		err = ErrBinding
	}
	unlockClose(lock)
	unlockClose(global)
	if err != nil {
		return RecoveryObservation{}, err
	}
	q.markJournal("recovery_validated")
	// RegisterUpload rechecks the same immutable tuple under these locks. Its
	// first fsynced frame commits mapping and registration together: no separate
	// map file, temporary source copy, manifest or partial mapping state exists.
	status, err := q.registerUpload(ctx, job, request.PhysicalName, func(string) (*os.File, error) { return cfg.openResultMapping(mapping) }, &mapping)
	return RecoveryObservation{Queue: status}, err
}

func canonicalRecoveryJSON(raw []byte) ([]byte, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&value) != nil || d.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	return json.Marshal(value)
}

// Protected attempt files are reopened and compared on EVERY resumed source
// access. Matching JSON on stdin alone cannot substitute for an original task.
func recoverySHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (h *HostConfig) validateResultRecovery(ctx context.Context, request deliverymeta.ResultRecovery, hashSource bool) (ResultSourceMapping, *os.File, error) {
	m := ResultSourceMapping{Request: request}
	if _, ok := request.FilePlan(); !ok || h.Binding != request.HostBinding {
		return m, nil, ErrBinding
	}
	plan, root, contextFile, closePlan, err := h.sourcePlan(request.Plan.Task.Attempt)
	if err != nil {
		return m, nil, err
	}
	defer closePlan()
	rawContext, err := sourceBytes(contextFile, 16384)
	if err != nil || deliverymeta.PlanHash(plan) != deliverymeta.PlanHash(request.Plan) || recoverySHA(rawContext) != request.ContextHash {
		return m, nil, ErrBinding
	}
	m.ContextIdentity, err = queueFileIdentity(contextFile)
	if err != nil {
		return m, nil, err
	}
	// Decode explicit wire keys below rather than accepting guessed Go names.
	var wire struct {
		Attempt     string `json:"attempt"`
		Generation  string `json:"generation"`
		RequestHash string `json:"request_hash"`
		PiSessionID string `json:"pi_session_id"`
		WorkspaceID string `json:"workspace_id"`
		PaneID      string `json:"pane_id"`
		TerminalID  string `json:"terminal_id"`
		Started     bool   `json:"started"`
		Exited      bool   `json:"exited"`
		ExitCode    int    `json:"exit_code"`
		Completion  struct {
			Name string `json:"name"`
			Hash string `json:"sha256"`
			Size int64  `json:"size"`
		} `json:"completion"`
	}
	f, err := openSourceFile(root, "receipt.json", os.O_RDONLY, false)
	if err != nil {
		return m, nil, err
	}
	raw, readErr := sourceBytes(f, 4095)
	m.ReceiptIdentity, err = queueFileIdentity(f)
	_ = f.Close()
	actual, canonicalErr := canonicalRecoveryJSON(raw)
	expected, expectedErr := canonicalRecoveryJSON(request.Receipt)
	if readErr != nil || err != nil || canonicalErr != nil || expectedErr != nil || !bytes.Equal(actual, expected) || json.Unmarshal(raw, &wire) != nil ||
		wire.Attempt != plan.Task.Attempt || wire.Generation != plan.Task.Generation || wire.RequestHash != request.RequestHash || !wire.Started || !wire.Exited || wire.ExitCode != 0 || wire.Completion.Name != "result.json" || !validHash(wire.Completion.Hash) || wire.Completion.Size <= 0 || wire.Completion.Size > 8192 {
		return m, nil, ErrBinding
	}
	f, err = openSourceFile(root, "result.json", os.O_RDONLY, false)
	if err != nil {
		return m, nil, err
	}
	raw, readErr = sourceBytes(f, 8192)
	m.CompletionIdentity, err = queueFileIdentity(f)
	_ = f.Close()
	var completed struct {
		Attempt     string `json:"attempt"`
		Generation  string `json:"generation"`
		RequestHash string `json:"request_hash"`
		PiSessionID string `json:"pi_session_id"`
		WorkspaceID string `json:"workspace_id"`
		PaneID      string `json:"pane_id"`
		TerminalID  string `json:"terminal_id"`
		Result      struct {
			Outcome string `json:"outcome"`
		} `json:"result"`
	}
	if readErr != nil || err != nil || int64(len(raw)) != wire.Completion.Size || recoverySHA(raw) != wire.Completion.Hash || json.Unmarshal(raw, &completed) != nil ||
		completed.Attempt != wire.Attempt || completed.Generation != wire.Generation || completed.RequestHash != wire.RequestHash || completed.PiSessionID != wire.PiSessionID || completed.WorkspaceID != wire.WorkspaceID || completed.PaneID != wire.PaneID || completed.TerminalID != wire.TerminalID || completed.Result.Outcome != "succeeded" {
		return m, nil, ErrBinding
	}
	f, err = h.OpenSource(request.PhysicalName)
	if err != nil {
		return m, nil, err
	}
	stable, release, err := guardQueueSource(f)
	if err != nil {
		_ = f.Close()
		return m, nil, err
	}
	defer release()
	before, err := describeSource(request.PhysicalName, stable, request.SizeBytes)
	if err != nil {
		_ = f.Close()
		return m, nil, err
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	for hashSource {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return m, nil, err
		}
		n, readErr := stable.Read(buffer)
		_, _ = hash.Write(buffer[:n])
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = f.Close()
			return m, nil, readErr
		}
	}
	after, err := describeSource(request.PhysicalName, stable, request.SizeBytes)
	if err != nil || *before != *after || hashSource && hex.EncodeToString(hash.Sum(nil)) != request.SHA256 {
		_ = f.Close()
		return m, nil, ErrSnapshot
	}
	m.Source, m.Digest = *after, ""
	m.Digest = m.digest()
	return m, f, nil
}

func (h *HostConfig) openResultMapping(m ResultSourceMapping) (*os.File, error) {
	actual, file, err := h.validateResultRecovery(context.Background(), m.Request, false)
	if err != nil || !sameResultMapping(&actual, &m) {
		if file != nil {
			_ = file.Close()
		}
		return nil, ErrBinding
	}
	return file, nil
}
