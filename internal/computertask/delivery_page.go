package computertask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

// DeliveryPage is a bounded view of the same operation ledger, not a new task.
// The append cursor survives entry updates and late inserts; references become
// visible only for committed entries. A page is not whole-task completion.
type DeliveryPage struct {
	Version     int                `json:"version"`
	OperationID string             `json:"operation_id"`
	Revision    string             `json:"revision"`
	Files       []DeliveryPageFile `json:"files"`
	NextAfter   string             `json:"next_after"`
	HasMore     bool               `json:"has_more"`
}

type DeliveryPageFile struct {
	DeliveryID string           `json:"delivery_id"`
	State      string           `json:"state"`
	Reference  *DeliveryRequest `json:"reference,omitempty"`
}

func readDeliveryPage(ctx context.Context, store dbstore.ComputerDeliveryEntryStore, r dbstore.ComputerOperationRecord, after, scope string) (DeliveryPage, error) {
	if after != "" {
		cursor, err := strconv.ParseInt(after, 10, 64)
		if err != nil || cursor < 0 || strconv.FormatInt(cursor, 10) != after {
			return DeliveryPage{}, ErrInvalid
		}
	}
	out := DeliveryPage{Version: 1, OperationID: r.ID, Revision: strconv.FormatInt(r.Revision, 10), Files: []DeliveryPageFile{}, NextAfter: after}
	entries, err := store.ListComputerDeliveryEntries(ctx, r, after, deliverymeta.MaxArtifacts+1)
	if err != nil {
		return out, err
	}
	out.HasMore = len(entries) > deliverymeta.MaxArtifacts
	if out.HasMore {
		entries = entries[:deliverymeta.MaxArtifacts]
	}
	for _, stored := range entries {
		var entry deliveryEntry
		if json.Unmarshal(stored.Data, &entry) != nil || !validStoredDeliveryEntry(stored.DeliveryID, entry, stored.Version) || scope != "" && scope != entry.Scope {
			return DeliveryPage{}, ErrDeliveryConflict
		}
		file := DeliveryPageFile{DeliveryID: stored.DeliveryID, State: entry.Outcome}
		if entry.Outcome == "committed" {
			ref := entry.Reference
			file.Reference = &ref
		}
		out.Files = append(out.Files, file)
		out.NextAfter = strconv.FormatInt(stored.Ordinal, 10)
	}
	return out, nil
}

// Page uses the same verified identity and authorization transaction as Commit.
// Existing HTTP Commit/Resolve and receipt formats remain unchanged.
func (s *DeliveryService) Page(ctx context.Context, operation, after string) (DeliveryPage, error) {
	if ctx == nil || s == nil || s.transactions == nil {
		return DeliveryPage{}, ErrDeliveryBinding
	}
	id, ok := ctx.Value(deliveryIdentityKey{}).(DeliveryIdentity)
	b, trusted := toolcontext.HostComputerBinding(ctx)
	if !ok || !trusted || b != id.Binding || operation != id.OperationID {
		return DeliveryPage{}, ErrDeliveryBinding
	}
	// A file-scoped HTTP principal must not acquire operation-wide enumeration.
	// Pages are consumed through the existing authorized task-query context.
	if _, ok := VerifiedTransferFromContext(ctx); ok {
		return DeliveryPage{}, ErrDeliveryBinding
	}
	var out DeliveryPage
	err := s.transactions(ctx, func(tx DeliveryTransaction) error {
		r, _, err := authorizeDeliveryRecord(ctx, tx, id)
		if err != nil {
			return err
		}
		rows, ok := tx.Store.(dbstore.ComputerDeliveryEntryStore)
		if !ok {
			return ErrDeliveryVersion
		}
		raw, _ := json.Marshal(id)
		hash := sha256.Sum256(raw)
		out, err = readDeliveryPage(ctx, rows, r, after, hex.EncodeToString(hash[:]))
		return err
	})
	return out, err
}

func (s *Service) deliveryPage(ctx context.Context, b toolcontext.ComputerBinding, id, after string) (DeliveryPage, error) {
	r, _, err := s.load(ctx, b, id)
	if err != nil {
		return DeliveryPage{}, err
	}
	rows, ok := s.store.(dbstore.ComputerDeliveryEntryStore)
	if !ok {
		return DeliveryPage{}, ErrDeliveryVersion
	}
	return readDeliveryPage(ctx, rows, r, after, "")
}
