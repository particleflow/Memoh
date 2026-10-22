package computertask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

var (
	ErrDeliveryBinding  = errors.New("delivery requires a verified execution identity")
	ErrDeliveryConflict = errors.New("delivery binding conflicts with its persisted outcome")
	ErrDeliveryVersion  = errors.New("unsupported delivery ledger version")
	ErrDeliveryLimit    = errors.New("operation delivery limit reached")
	ErrDeliveryRetry    = errors.New("delivery transaction conflicted; resolve before retrying")
	ErrDeliveryAsset    = errors.New("delivery asset is unavailable or does not match")
	errVerifyDelivery   = errors.New("verify delivery outside the database transaction")
	deliveryIDPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	deliveryHash        = regexp.MustCompile(`^[a-f0-9]{64}$`)
	deliveryAttempt     = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

const maxDeliveries = 128

// DeliveryIdentity is supplied by an authenticated host, never by JSON, URL
// fields or credential claims that have not been verified by that host.
type DeliveryIdentity struct {
	Binding                           toolcontext.ComputerBinding
	OperationID, DeviceID, InstanceID string
	Attempt, Generation               string
}

type deliveryIdentityKey struct{}

// WithDeliveryIdentity only attenuates an existing process-local host binding.
// A future PC authenticator must establish both after verifying its credential.
func WithDeliveryIdentity(ctx context.Context, identity DeliveryIdentity) (context.Context, error) {
	b, ok := toolcontext.HostComputerBinding(ctx)
	if !ok || b != identity.Binding || b.RunID == "" || ctx.Err() != nil ||
		identity.DeviceID == "" || len(identity.DeviceID) > 256 || identity.InstanceID == "" || len(identity.InstanceID) > 256 ||
		!deliveryAttempt.MatchString(identity.Attempt) || !deliveryAttempt.MatchString(identity.Generation) {
		return ctx, ErrDeliveryBinding
	}
	for _, id := range []string{b.BotID, b.UserID, b.SessionID, b.TargetID, identity.OperationID} {
		if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
			return ctx, ErrDeliveryBinding
		}
	}
	if current, exists := ctx.Value(deliveryIdentityKey{}).(DeliveryIdentity); exists && current != identity {
		return ctx, ErrDeliveryBinding
	}
	return context.WithValue(ctx, deliveryIdentityKey{}, identity), nil
}

// DeliveryRequest contains only an immutable content reference. It carries no
// authority, filesystem location, bytes or execution output.
type DeliveryRequest struct {
	Version     int    `json:"version"`
	Direction   string `json:"direction"`
	ContentHash string `json:"content_hash"`
	SizeBytes   int64  `json:"size_bytes"`
	Mime        string `json:"mime"`
}

func (r DeliveryRequest) valid() bool {
	if r.Version != 1 || (r.Direction != "upload" && r.Direction != "download") ||
		!deliveryHash.MatchString(r.ContentHash) || r.SizeBytes < 0 || len(r.Mime) > 255 {
		return false
	}
	t, params, err := mime.ParseMediaType(r.Mime)
	return err == nil && t == r.Mime && len(params) == 0
}

type DeliveryOutcome struct {
	Version  int    `json:"version"`
	Outcome  string `json:"outcome"`
	Revision string `json:"revision"`
}

type deliveryEntry struct {
	Reference   DeliveryRequest `json:"reference"`
	Scope       string          `json:"scope"`
	Outcome     string          `json:"outcome"`
	At          time.Time       `json:"at"`
	Lease       string          `json:"lease,omitempty"`
	Until       time.Time       `json:"until,omitempty"`
	Attempts    int             `json:"attempts,omitempty"`
	Publication string          `json:"publication,omitempty"`
	Transfer    *transferAccess `json:"transfer,omitempty"`
}

type deliveryLedger struct {
	Version int                      `json:"version"`
	Entries map[string]deliveryEntry `json:"entries"`
}

type DeliveryAssets interface {
	OpenSeekable(context.Context, string, string) (media.ReadSeekCloser, media.Asset, error)
}

// DeliveryTransaction runs authorization and CAS in the SAME transaction.
// Authorize must use its transaction's current account/grant/session/target
// records. Implementations must not return success before the DB commit.
type DeliveryTransaction struct {
	Store     dbstore.ComputerDeliveryStore
	Authorize func(context.Context, DeliveryIdentity) (agenvo.Target, error)
}

type DeliveryTransactions func(context.Context, func(DeliveryTransaction) error) error

type DeliveryService struct {
	transactions DeliveryTransactions
	assets       DeliveryAssets
}

func NewDeliveryService(transactions DeliveryTransactions, assets DeliveryAssets) *DeliveryService {
	return &DeliveryService{transactions: transactions, assets: assets}
}

func (s *DeliveryService) Commit(ctx context.Context, operation, delivery string, ref DeliveryRequest) (DeliveryOutcome, error) {
	return s.apply(ctx, operation, delivery, ref, true, false)
}

func (s *DeliveryService) Resolve(ctx context.Context, operation, delivery string, ref DeliveryRequest, cancel bool) (DeliveryOutcome, error) {
	return s.apply(ctx, operation, delivery, ref, false, cancel)
}

func (s *DeliveryService) apply(ctx context.Context, operation, delivery string, ref DeliveryRequest, commit, cancel bool) (DeliveryOutcome, error) {
	if ctx == nil {
		return DeliveryOutcome{}, ErrDeliveryBinding
	}
	if err := ctx.Err(); err != nil {
		return DeliveryOutcome{}, err
	}
	id, ok := ctx.Value(deliveryIdentityKey{}).(DeliveryIdentity)
	b, trusted := toolcontext.HostComputerBinding(ctx)
	if !ok || !trusted || b != id.Binding || operation != id.OperationID || s == nil || s.transactions == nil {
		return DeliveryOutcome{}, ErrDeliveryBinding
	}
	if !deliveryIDPattern.MatchString(delivery) || !ref.valid() {
		return DeliveryOutcome{}, ErrInvalid
	}
	verified := false
	for conflicts := 0; conflicts < 3; {
		var out DeliveryOutcome
		err := s.transactions(ctx, func(tx DeliveryTransaction) error {
			var err error
			out, err = applyDeliveryTransaction(ctx, tx, id, delivery, ref, commit, cancel, verified)
			return err
		})
		if err == nil {
			return out, nil
		}
		if errors.Is(err, errVerifyDelivery) {
			// No mutation has occurred. Release the DB transaction before a
			// potentially long Media integrity scan, then reauthorize in a new
			// snapshot before CAS. Reuse verification across known CAS aborts.
			if err := s.verifyAsset(ctx, b.BotID, ref); err != nil {
				return DeliveryOutcome{}, err
			}
			verified = true
			continue
		}
		// Only a KNOWN aborted transaction may be retried. Timeout/connection
		// loss at commit is unknown, even if a retry could be idempotent.
		if !errors.Is(err, ErrDeliveryRetry) {
			return DeliveryOutcome{}, err
		}
		conflicts++
		if err := ctx.Err(); err != nil {
			return DeliveryOutcome{}, err
		}
	}
	return DeliveryOutcome{}, ErrDeliveryRetry
}

func applyDeliveryTransaction(ctx context.Context, tx DeliveryTransaction, id DeliveryIdentity, delivery string, ref DeliveryRequest, commit, cancel, verified bool) (DeliveryOutcome, error) {
	return mutateDeliveryTransaction(ctx, tx, id, delivery, ref, 1, func(entry *deliveryEntry, exists bool) (bool, error) {
		if exists {
			if commit && entry.Outcome == "canceled" {
				return false, ErrDeliveryConflict
			}
			return false, nil
		}
		if !commit && !cancel {
			return false, nil
		}
		entry.Outcome = "canceled"
		if commit {
			if !verified {
				return false, errVerifyDelivery
			}
			entry.Outcome = "committed"
		}
		return true, nil
	})
}

func mutateDeliveryTransaction(ctx context.Context, tx DeliveryTransaction, id DeliveryIdentity, delivery string, ref DeliveryRequest, version int, mutate func(*deliveryEntry, bool) (bool, error)) (DeliveryOutcome, error) {
	if version == 3 {
		p, ok := VerifiedTransferFromContext(ctx)
		if !ok || !p.Valid(time.Now()) || p.Identity != id || p.DeliveryID != delivery || p.Reference != ref {
			return DeliveryOutcome{}, ErrDeliveryBinding
		}
	}
	if tx.Store == nil || tx.Authorize == nil {
		return DeliveryOutcome{}, ErrDeliveryBinding
	}
	target, err := tx.Authorize(ctx, id)
	if err != nil {
		return DeliveryOutcome{}, err
	}
	b := id.Binding
	r, err := tx.Store.ReadComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: b.BotID, SessionID: b.SessionID, UserID: b.UserID})
	if err != nil {
		return DeliveryOutcome{}, err
	}
	if r.ID != id.OperationID || r.BotID != b.BotID || r.SessionID != b.SessionID || r.UserID != b.UserID || r.TargetID != b.TargetID || r.RunID != b.RunID ||
		target.DeviceID != id.DeviceID || target.InstanceID != id.InstanceID || r.TargetFingerprint != agenvo.TargetFingerprint(target) || target.Agent.ExecutionMode != "pi-json-v1" {
		return DeliveryOutcome{}, ErrDeliveryBinding
	}
	var details struct {
		Kind   string          `json:"kind"`
		Batch  *BatchDetails   `json:"batch"`
		Ledger json.RawMessage `json:"delivery_ledger"`
	}
	if len(r.Details) > 2*1024*1024 || json.Unmarshal(r.Details, &details) != nil || details.Kind != "herdr_pi_task" || details.Batch == nil ||
		details.Batch.Progress.Attempt != id.Attempt || details.Batch.Progress.Generation != id.Generation {
		return DeliveryOutcome{}, ErrDeliveryBinding
	}
	ledger := &deliveryLedger{}
	if len(details.Ledger) > 0 {
		if string(details.Ledger) == "null" || json.Unmarshal(details.Ledger, ledger) != nil {
			return DeliveryOutcome{}, ErrDeliveryConflict
		}
	} else {
		ledger.Version, ledger.Entries = version, map[string]deliveryEntry{}
	}
	if ledger.Version != version && (version < 2 || ledger.Version < 1 || ledger.Version >= version) {
		return DeliveryOutcome{}, ErrDeliveryVersion
	}
	if ledger.Entries == nil || len(ledger.Entries) > maxDeliveries {
		return DeliveryOutcome{}, ErrDeliveryConflict
	}
	for key, entry := range ledger.Entries {
		if version >= 2 && ledger.Version == 1 && entry.Reference.Direction == "download" {
			return DeliveryOutcome{}, ErrDeliveryVersion
		}
		if !deliveryIDPattern.MatchString(key) || !entry.Reference.valid() || !deliveryHash.MatchString(entry.Scope) || entry.At.IsZero() || !validDeliveryEntry(entry, ledger.Version) {
			return DeliveryOutcome{}, ErrDeliveryConflict
		}
	}
	rawScope, _ := json.Marshal(id)
	hash := sha256.Sum256(rawScope)
	scope := hex.EncodeToString(hash[:])
	out := DeliveryOutcome{Version: version, Outcome: "absent", Revision: strconv.FormatInt(r.Revision, 10)}
	entry, exists := ledger.Entries[delivery]
	if exists {
		if entry.Reference != ref || entry.Scope != scope {
			return DeliveryOutcome{}, ErrDeliveryConflict
		}
		// A v1 download cancellation never attested a local publication fence.
		if version >= 2 && ledger.Version == 1 && ref.Direction == "download" {
			return DeliveryOutcome{}, ErrDeliveryVersion
		}
		out.Outcome = entry.Outcome
	} else {
		entry = deliveryEntry{Reference: ref, Scope: scope}
	}
	changed, err := mutate(&entry, exists)
	if err != nil {
		return DeliveryOutcome{}, err
	}
	if !changed {
		if out.Outcome == "reserved" {
			out.Outcome = "absent"
		}
		return out, nil
	}
	if !exists && len(ledger.Entries) == maxDeliveries {
		return DeliveryOutcome{}, ErrDeliveryLimit
	}
	entry.At = time.Now().UTC()
	ledger.Version = version
	ledger.Entries[delivery] = entry
	out.Outcome = entry.Outcome
	if out.Outcome == "reserved" {
		out.Outcome = "absent"
	}
	data, err := json.Marshal(ledger)
	if err != nil {
		return DeliveryOutcome{}, err
	}
	// Keep the entire existing operation readable by its bounded projections.
	// Conservative JSON overhead allowance avoids creating an unreadable row.
	if len(r.Details)-len(details.Ledger)+len(data)+64 > 2*1024*1024 {
		return DeliveryOutcome{}, ErrDeliveryLimit
	}
	r, err = tx.Store.SaveComputerDelivery(ctx, r, data)
	if errors.Is(err, db.ErrNotFound) {
		return DeliveryOutcome{}, ErrDeliveryRetry
	}
	if err != nil {
		return DeliveryOutcome{}, err
	}
	out.Revision = strconv.FormatInt(r.Revision, 10)
	return out, nil
}

func (s *DeliveryService) verifyAsset(ctx context.Context, bot string, ref DeliveryRequest) error {
	if s.assets == nil {
		return ErrDeliveryAsset
	}
	reader, asset, err := s.assets.OpenSeekable(ctx, bot, ref.ContentHash)
	if err != nil {
		return err
	}
	// Resolve/Stat does not fill SizeBytes. Use the existing verified Range
	// stream; never treat the request's length or filename as content proof.
	size, seekErr := reader.Seek(0, io.SeekEnd)
	closeErr := reader.Close()
	if seekErr != nil {
		return seekErr
	}
	if closeErr != nil {
		return closeErr
	}
	if asset.BotID != bot || asset.ContentHash != ref.ContentHash || size != ref.SizeBytes {
		return ErrDeliveryAsset
	}
	return nil
}

func validDeliveryEntry(e deliveryEntry, version int) bool {
	if e.Transfer != nil {
		if version < 3 || e.Transfer.Creating && e.Transfer.UploadID != "" ||
			e.Transfer.UploadID != "" && !deliveryIDPattern.MatchString(e.Transfer.UploadID) ||
			e.Reference.Direction != "upload" && (e.Transfer.Creating || e.Transfer.UploadID != "") {
			return false
		}
	}
	if e.Outcome == "reserved" {
		return version == 3 && e.Transfer != nil && e.Publication == "" && e.Lease == "" && e.Until.IsZero() && e.Attempts == 0
	}
	if version == 1 {
		return e.Outcome == "committed" || e.Outcome == "canceled"
	}
	if e.Attempts < 0 || e.Attempts > 3 || (e.Publication != "" && e.Publication != "published" && e.Publication != "fenced") {
		return false
	}
	if e.Reference.Direction == "download" && ((e.Outcome == "canceled" && e.Publication != "fenced") || (e.Outcome != "canceled" && e.Publication != "published")) {
		return false
	}
	if e.Outcome == "pending" {
		return e.Lease != "" && !e.Until.IsZero() && e.Attempts > 0
	}
	return e.Outcome == "committed" || e.Outcome == "canceled" || e.Outcome == "failed"
}
