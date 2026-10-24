//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/computertask"
	memohdb "github.com/felinics/memoh/internal/db"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

// Synthetic metadata only: all entries refer to the parent's one real small
// asset. This does not pretend that count files traversed the network.
func exerciseDeliveryEntryPages(ctx context.Context, t *testing.T, store *postgresstore.Store, service *computertask.DeliveryService, id computertask.DeliveryIdentity, ref computertask.DeliveryRequest) {
	t.Helper()
	op := dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID}
	r, err := store.ReadComputerOperation(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := store.ReadComputerDeliveryEntry(ctx, r, "result")
	if err != nil {
		t.Fatal(err)
	}
	wrongOwner := r
	wrongOwner.UserID = "00000000-0000-0000-0000-000000000001"
	if entries, err := store.ListComputerDeliveryEntries(ctx, wrongOwner, "", 16); err != nil || len(entries) != 0 {
		t.Fatal("wrong owner page disclosed entries", err)
	}
	for _, cursor := range []string{"-1", "01", "9223372036854775808", "other"} {
		if _, err := service.Page(ctx, id.OperationID, cursor); !errors.Is(err, computertask.ErrInvalid) {
			t.Fatal("invalid cursor accepted", cursor, err)
		}
	}
	principal := computertask.VerifiedTransfer{Identity: id, DeliveryID: "result", Reference: ref, Audience: computertask.TransferAudience, CredentialID: "fixture", IssuedAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(time.Minute)}
	fileContext, err := computertask.BindVerifiedTransfer(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Page(fileContext, id.OperationID, ""); !errors.Is(err, computertask.ErrDeliveryBinding) {
		t.Fatal("file principal broadened to operation enumeration", err)
	}
	count := 257
	if os.Getenv("MEMOH_TRANSFER_ENTRY_SCALE") == "10000" {
		count = 10000
	}
	started := time.Now()
	stale := r
	for i := range count {
		entry := seed
		entry.DeliveryID = fmt.Sprintf("metadata_%05d", i)
		r, err = store.SaveComputerDeliveryEntry(ctx, r, entry)
		if err != nil {
			t.Fatal(i, err)
		}
		if i == count/2 {
			store, err = postgresstore.New(store.Pool())
			if err != nil {
				t.Fatal(err)
			}
			r, err = store.ReadComputerOperation(ctx, op)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := store.SaveComputerDeliveryEntry(ctx, stale, seed); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("stale CAS accepted", err)
	}
	if len(r.Details) > 8192 {
		t.Fatal("metadata grew the operation JSON", len(r.Details))
	}
	first, err := service.Page(ctx, id.OperationID, "")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := service.Page(ctx, id.OperationID, "")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(repeated)
	if string(a) != string(b) {
		t.Fatal("duplicate page changed")
	}
	// Insert a lexically earlier ID after page 1; an append cursor must still
	// include it. An ID-sorted cursor would silently omit this record.
	late := seed
	late.DeliveryID = "000_late"
	r, err = store.SaveComputerDeliveryEntry(ctx, r, late)
	if err != nil {
		t.Fatal(err)
	}
	seen, pages, foundLate := 0, 0, false
	cursor := ""
	for {
		page, err := service.Page(ctx, id.OperationID, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Files) > 16 || len(page.Files) > 0 && page.NextAfter == cursor {
			t.Fatal("unbounded or nonadvancing page")
		}
		for _, item := range page.Files {
			if strings.HasPrefix(item.DeliveryID, "metadata_") {
				seen++
			}
			foundLate = foundLate || item.DeliveryID == late.DeliveryID
		}
		pages++
		cursor = page.NextAfter
		if !page.HasMore {
			break
		}
	}
	if seen != count || !foundLate {
		t.Fatal("page skipped metadata", seen, count, foundLate)
	}
	for _, key := range []string{"metadata_00000", fmt.Sprintf("metadata_%05d", count-1)} {
		got, err := service.Resolve(ctx, id.OperationID, key, ref, false)
		if err != nil || got.Outcome != "committed" {
			t.Fatal("restart lookup", err)
		}
		again, err := service.Commit(ctx, id.OperationID, key, ref)
		if err != nil || again != got {
			t.Fatal("idempotent replay", err)
		}
		wrong := ref
		wrong.SizeBytes++
		if _, err := service.Commit(ctx, id.OperationID, key, wrong); !errors.Is(err, computertask.ErrDeliveryConflict) {
			t.Fatal("same ID conflict accepted", err)
		}
	}
	t.Logf("ledger metadata=%d pages=%d operation_json_bytes=%d elapsed=%s; restart/CAS/repeated page/late insert/original IDs preserved; one small underlying asset, not bulk network transfer", count, pages, len(r.Details), time.Since(started))
}
