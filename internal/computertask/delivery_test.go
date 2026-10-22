package computertask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

// Synthetic transaction fixture; real PostgreSQL contracts are integration tests.
type deliveryFixture struct {
	dbstore.ComputerOperationStore
	mu                  sync.Mutex
	record              dbstore.ComputerOperationRecord
	id                  DeliveryIdentity
	target              agenvo.Target
	asset               media.Asset
	revoked             bool
	authorizations      int
	conflicts           int
	loseResponse        error
	revokeAfterConflict bool
}

func newDeliveryFixture(t *testing.T) (*deliveryFixture, *DeliveryService, context.Context, DeliveryRequest) {
	t.Helper()
	f := &deliveryFixture{}
	f.id = DeliveryIdentity{Binding: toolcontext.ComputerBinding{BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "synthetic-run"}, OperationID: uuid.NewString(), DeviceID: "fixture-device", InstanceID: "fixture-instance", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	f.target = agenvo.Target{DeviceID: f.id.DeviceID, InstanceID: f.id.InstanceID, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1"}}
	details, _ := json.Marshal(Details{Kind: "herdr_pi_task", Batch: &BatchDetails{Progress: agenvo.BatchProgress{Attempt: f.id.Attempt, Generation: f.id.Generation}}, Phase: "execution-completed"})
	b := f.id.Binding
	f.record = dbstore.ComputerOperationRecord{ID: f.id.OperationID, BotID: b.BotID, UserID: b.UserID, SessionID: b.SessionID, TargetID: b.TargetID, RunID: b.RunID, TargetFingerprint: agenvo.TargetFingerprint(f.target), State: "completed", Details: details, Revision: 3}
	ref := DeliveryRequest{Version: 1, Direction: "upload", ContentHash: strings.Repeat("c", 64), SizeBytes: 42, Mime: "application/octet-stream"}
	f.asset = media.Asset{BotID: b.BotID, ContentHash: ref.ContentHash, SizeBytes: ref.SizeBytes, Mime: ref.Mime}
	ctx, err := toolcontext.WithComputerBinding(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = WithDeliveryIdentity(ctx, f.id)
	if err != nil {
		t.Fatal(err)
	}
	return f, NewDeliveryService(f.transact, f), ctx, ref
}

func (f *deliveryFixture) transact(ctx context.Context, fn func(DeliveryTransaction) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	before := f.record
	err := fn(DeliveryTransaction{Store: f, Authorize: func(context.Context, DeliveryIdentity) (agenvo.Target, error) {
		f.authorizations++
		if f.revoked {
			return agenvo.Target{}, ErrDenied
		}
		return f.target, nil
	}})
	if err != nil {
		f.record = before
		return err
	}
	if f.loseResponse != nil {
		err, f.loseResponse = f.loseResponse, nil
		return err // Committed, but the acknowledgement was lost.
	}
	return ctx.Err()
}

func (f *deliveryFixture) ReadComputerOperation(context.Context, dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	return f.record, nil
}

func (f *deliveryFixture) SaveComputerDelivery(_ context.Context, r dbstore.ComputerOperationRecord, ledger dbstore.JSON) (dbstore.ComputerOperationRecord, error) {
	if f.conflicts > 0 {
		f.conflicts--
		f.revoked = f.revokeAfterConflict
		return r, db.ErrNotFound
	}
	if r.Revision != f.record.Revision {
		return r, db.ErrNotFound
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(f.record.Details, &fields)
	fields["delivery_ledger"] = json.RawMessage(ledger)
	r.Details, _ = json.Marshal(fields)
	r.Revision++
	f.record = r
	return r, nil
}

type fixtureMedia struct{ *bytes.Reader }

func (fixtureMedia) Close() error { return nil }

func (f *deliveryFixture) OpenSeekable(context.Context, string, string) (media.ReadSeekCloser, media.Asset, error) {
	return fixtureMedia{bytes.NewReader(make([]byte, f.asset.SizeBytes))}, f.asset, nil
}

func TestDeliveryCommitResolveAndExecutionSeparation(t *testing.T) {
	f, s, ctx, ref := newDeliveryFixture(t)
	out, err := s.Resolve(ctx, f.id.OperationID, "result-1", ref, false)
	if err != nil || out.Outcome != "absent" || f.record.Revision != 3 {
		t.Fatalf("absent: %+v %v", out, err)
	}
	out, err = s.Commit(ctx, f.id.OperationID, "result-1", ref)
	if err != nil || out.Outcome != "committed" || out.Revision != "4" {
		t.Fatalf("commit: %+v %v", out, err)
	}
	f.asset = media.Asset{} // Ledger truth survives media removal.
	for _, cancel := range []bool{false, true} {
		out, err = s.Resolve(ctx, f.id.OperationID, "result-1", ref, cancel)
		if err != nil || out.Outcome != "committed" {
			t.Fatalf("resolve cancel=%v: %+v %v", cancel, out, err)
		}
	}
	out, err = s.Commit(ctx, f.id.OperationID, "result-1", ref)
	if err != nil || out.Revision != "4" || f.authorizations != 5 {
		t.Fatalf("idempotency must reauthorize: %+v %v checks=%d", out, err, f.authorizations)
	}
	var details Details
	_ = json.Unmarshal(f.record.Details, &details)
	if f.record.State != "completed" || f.record.CancelRequested || details.Phase != "execution-completed" || details.Batch.Progress.Attempt != f.id.Attempt {
		t.Fatal("delivery changed execution facts")
	}
}

func TestDeliveryUnknownCommitReconcilesBeforeCancel(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled, db.ErrCommitOutcomeUnknown} {
		t.Run(cause.Error(), func(t *testing.T) {
			f, s, ctx, ref := newDeliveryFixture(t)
			f.loseResponse = cause
			if out, err := s.Commit(ctx, f.id.OperationID, "result", ref); !errors.Is(err, cause) || out.Outcome != "" || f.authorizations != 1 {
				t.Fatalf("unknown commit must not retry or acknowledge: %+v %v", out, err)
			}
			out, err := s.Resolve(ctx, f.id.OperationID, "result", ref, true)
			if err != nil || out.Outcome != "committed" {
				t.Fatalf("cancel cannot retract committed association: %+v %v", out, err)
			}
		})
	}
}

func TestDeliveryFenceRaceAndLateCommit(t *testing.T) {
	f, s, ctx, ref := newDeliveryFixture(t)
	out, err := s.Resolve(ctx, f.id.OperationID, "fenced", ref, true)
	if err != nil || out.Outcome != "canceled" {
		t.Fatalf("fence: %+v %v", out, err)
	}
	if _, err := s.Commit(ctx, f.id.OperationID, "fenced", ref); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("late commit crossed fence: %v", err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_, err := s.Commit(ctx, f.id.OperationID, "race", ref)
				if err != nil && !errors.Is(err, ErrDeliveryConflict) {
					t.Error(err)
				}
			} else if _, err := s.Resolve(ctx, f.id.OperationID, "race", ref, true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	out, err = s.Resolve(ctx, f.id.OperationID, "race", ref, false)
	if err != nil || (out.Outcome != "committed" && out.Outcome != "canceled") || f.record.Revision != 5 {
		t.Fatalf("one durable winner: %+v %v revision=%d", out, err, f.record.Revision)
	}
}

func TestDeliveryRejectsCrossScopeAndForgedIdentity(t *testing.T) {
	mutations := map[string]func(*DeliveryIdentity){
		"bot":        func(i *DeliveryIdentity) { i.Binding.BotID = uuid.NewString() },
		"user":       func(i *DeliveryIdentity) { i.Binding.UserID = uuid.NewString() },
		"session":    func(i *DeliveryIdentity) { i.Binding.SessionID = uuid.NewString() },
		"target":     func(i *DeliveryIdentity) { i.Binding.TargetID = uuid.NewString() },
		"run":        func(i *DeliveryIdentity) { i.Binding.RunID = "another-run" },
		"operation":  func(i *DeliveryIdentity) { i.OperationID = uuid.NewString() },
		"device":     func(i *DeliveryIdentity) { i.DeviceID = "another-device" },
		"instance":   func(i *DeliveryIdentity) { i.InstanceID = "another-instance" },
		"attempt":    func(i *DeliveryIdentity) { i.Attempt = strings.Repeat("d", 32) },
		"generation": func(i *DeliveryIdentity) { i.Generation = strings.Repeat("e", 32) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f, s, ctx, ref := newDeliveryFixture(t)
			id := f.id
			mutate(&id)
			if _, err := WithDeliveryIdentity(ctx, id); !errors.Is(err, ErrDeliveryBinding) {
				t.Fatalf("identity replacement accepted: %v", err)
			}
			forged, _ := toolcontext.WithComputerBinding(context.Background(), id.Binding)
			forged, err := WithDeliveryIdentity(forged, id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Commit(forged, id.OperationID, "result", ref); !errors.Is(err, ErrDeliveryBinding) || f.record.Revision != 3 {
				t.Fatalf("cross scope accepted: %v", err)
			}
		})
	}
	f, s, _, ref := newDeliveryFixture(t)
	if _, err := s.Commit(context.Background(), f.id.OperationID, "result", ref); !errors.Is(err, ErrDeliveryBinding) || f.authorizations != 0 {
		t.Fatal("operation ID established authority")
	}
}

func TestDeliveryRechecksRevocationAndBoundsCASRetries(t *testing.T) {
	for _, mode := range []string{"revoked-before", "revoked-after-commit", "revoke-on-conflict", "retry-success", "retry-exhausted"} {
		t.Run(mode, func(t *testing.T) {
			f, s, ctx, ref := newDeliveryFixture(t)
			switch mode {
			case "revoked-after-commit":
				if _, err := s.Commit(ctx, f.id.OperationID, "result", ref); err != nil {
					t.Fatal(err)
				}
				fallthrough
			case "revoked-before":
				f.revoked = true
			case "revoke-on-conflict":
				f.conflicts, f.revokeAfterConflict = 1, true
			case "retry-success":
				f.conflicts = 2
			case "retry-exhausted":
				f.conflicts = 5
			}
			_, err := s.Commit(ctx, f.id.OperationID, "result", ref)
			switch mode {
			case "retry-success":
				if err != nil || f.authorizations != 3 {
					t.Fatalf("retry: %v checks=%d", err, f.authorizations)
				}
			case "retry-exhausted":
				if !errors.Is(err, ErrDeliveryRetry) || f.authorizations != 3 {
					t.Fatalf("budget: %v", err)
				}
			default:
				if !errors.Is(err, ErrDenied) {
					t.Fatalf("revocation: %v", err)
				}
				if _, err := s.Resolve(ctx, f.id.OperationID, "result", ref, true); !errors.Is(err, ErrDenied) {
					t.Fatalf("resolve leaked revoked outcome: %v", err)
				}
			}
		})
	}
}

func TestDeliveryImmutableReferenceLimitsAndVersions(t *testing.T) {
	f, s, ctx, ref := newDeliveryFixture(t)
	if _, err := s.Commit(ctx, f.id.OperationID, "result", ref); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"direction", "hash", "size", "mime"} {
		changed := ref
		switch field {
		case "direction":
			changed.Direction = "download"
		case "hash":
			changed.ContentHash = strings.Repeat("d", 64)
		case "size":
			changed.SizeBytes++
		case "mime":
			changed.Mime = "text/plain"
		}
		if _, err := s.Resolve(ctx, f.id.OperationID, "result", changed, true); !errors.Is(err, ErrDeliveryConflict) {
			t.Fatalf("rebound %s: %v", field, err)
		}
	}
	for range maxDeliveries - 1 {
		if _, err := s.Resolve(ctx, f.id.OperationID, uuid.NewString(), ref, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Resolve(ctx, f.id.OperationID, "overflow", ref, true); !errors.Is(err, ErrDeliveryLimit) {
		t.Fatalf("bound: %v", err)
	}
	if out, err := s.Resolve(ctx, f.id.OperationID, "overflow", ref, false); err != nil || out.Outcome != "absent" {
		t.Fatalf("plain lookup: %+v %v", out, err)
	}
	f.record.Details = []byte(strings.Replace(string(f.record.Details), `"delivery_ledger":{"version":1`, `"delivery_ledger":{"version":2`, 1))
	if _, err := s.Resolve(ctx, f.id.OperationID, "result", ref, false); !errors.Is(err, ErrDeliveryVersion) {
		t.Fatalf("version: %v", err)
	}
}

func TestDeliveryMediaAndInputValidation(t *testing.T) {
	for _, field := range []string{"bot", "hash", "size", "mime", "direction", "id", "version", "legacy"} {
		t.Run(field, func(t *testing.T) {
			f, s, ctx, ref := newDeliveryFixture(t)
			id := "result"
			switch field {
			case "bot":
				f.asset.BotID = uuid.NewString()
			case "hash":
				f.asset.ContentHash = strings.Repeat("d", 64)
			case "size":
				f.asset.SizeBytes++
			case "mime":
				ref.Mime = "text/plain; charset=utf-8"
			case "direction":
				ref.Direction = "other"
			case "id":
				id = "../result"
			case "version":
				ref.Version = 2
			case "legacy":
				f.target.Agent.ExecutionMode = ""
			}
			if _, err := s.Commit(ctx, f.id.OperationID, id, ref); err == nil || f.record.Revision != 3 {
				t.Fatalf("invalid reference accepted: %v", err)
			}
		})
	}
}

func TestDeliveryTransactionErrorClassification(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		if !errors.Is(deliveryTransactionError(&pgconn.PgError{Code: code}, true), ErrDeliveryRetry) {
			t.Fatal("known abort must retry")
		}
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		out := deliveryTransactionError(err, true)
		if !errors.Is(out, db.ErrCommitOutcomeUnknown) || !errors.Is(out, err) {
			t.Fatalf("lost classification: %v", out)
		}
	}
}
