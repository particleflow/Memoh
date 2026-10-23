package computertask

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/job"
	"github.com/felinics/memoh/internal/toolcontext"
)

// DeliveryIdentityCheck revalidates the incoming execution credential's binding
// and lifetime, including when the HTTP request is no longer alive. Database
// authorization is checked separately in every transaction. It must not mint or
// refresh a credential. There is no production implementation in this candidate.
type DeliveryIdentityCheck func(context.Context, DeliveryIdentity) error

// AsyncDeliveryService owns finite work inside the existing server process.
// Only an authorized request starts/reclaims a job; there is no polling loop.
type AsyncDeliveryService struct {
	base   *DeliveryService
	check  DeliveryIdentityCheck
	log    *slog.Logger
	life   context.Context
	cancel context.CancelFunc
	slots  chan struct{}
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
	lease  time.Duration
}

func NewAsyncDeliveryService(lifetime context.Context, base *DeliveryService, check DeliveryIdentityCheck, log *slog.Logger) (*AsyncDeliveryService, error) {
	if lifetime == nil || base == nil || base.transactions == nil || base.assets == nil || check == nil {
		return nil, ErrDeliveryBinding
	}
	life, cancel := context.WithCancel(lifetime)
	return &AsyncDeliveryService{base: base, check: check, log: log, life: life, cancel: cancel, slots: make(chan struct{}, 2), lease: 10 * time.Minute}, nil
}

// Close cancels cooperative work. Filesystem calls can still outlive cancellation.
func (s *AsyncDeliveryService) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *AsyncDeliveryService) Commit(ctx context.Context, operation, delivery string, ref DeliveryRequest, publication string) (DeliveryOutcome, error) {
	return s.apply(ctx, operation, delivery, ref, publication, true, false)
}

func (s *AsyncDeliveryService) Resolve(ctx context.Context, operation, delivery string, ref DeliveryRequest, publication string, cancel bool) (DeliveryOutcome, error) {
	return s.apply(ctx, operation, delivery, ref, publication, false, cancel)
}

func (s *AsyncDeliveryService) apply(ctx context.Context, operation, delivery string, ref DeliveryRequest, publication string, commit, cancel bool) (DeliveryOutcome, error) {
	if ctx == nil || s == nil {
		return DeliveryOutcome{}, ErrDeliveryBinding
	}
	if err := ctx.Err(); err != nil {
		return DeliveryOutcome{}, err
	}
	id, ok := ctx.Value(deliveryIdentityKey{}).(DeliveryIdentity)
	b, trusted := toolcontext.HostComputerBinding(ctx)
	if !ok || !trusted || id.Binding != b || operation != id.OperationID {
		return DeliveryOutcome{}, ErrDeliveryBinding
	}
	if !deliveryIDPattern.MatchString(delivery) || !ref.valid() {
		return DeliveryOutcome{}, ErrInvalid
	}
	if ref.Direction == "upload" && publication != "" || ref.Direction == "download" && ((commit && publication != "published") || (cancel && publication != "fenced") || (!commit && !cancel && publication != "")) {
		return DeliveryOutcome{}, ErrDeliveryBinding
	}
	if err := s.check(ctx, id); err != nil {
		return DeliveryOutcome{}, err
	}
	if err := s.life.Err(); err != nil {
		return DeliveryOutcome{}, err
	}
	hasSlot := false
	select {
	case s.slots <- struct{}{}:
		hasSlot = true
	default:
	}
	defer func() {
		if hasSlot {
			<-s.slots
		}
	}()
	epoch := uuid.NewString()
	var claimed bool
	var until time.Time
	out, err := s.transact(ctx, id, delivery, ref, func(e *deliveryEntry, exists bool) (bool, error) {
		claimed = false // A known CAS abort may rerun this mutation.
		reserved := exists && e.Outcome == "reserved"
		if exists && e.Outcome == "committed" {
			return false, nil
		}
		if exists && e.Outcome == "canceled" {
			if commit {
				return false, ErrDeliveryConflict
			}
			return false, nil
		}
		if cancel {
			// A published local file cannot be retracted by a NAS tombstone.
			if e.Publication == "published" {
				return false, ErrDeliveryConflict
			}
			e.Outcome, e.Publication = "canceled", publication
			return true, nil
		}
		if (!exists || reserved) && !commit {
			return false, nil
		}
		if exists && (e.Outcome == "failed" || time.Now().Before(e.Until)) {
			return false, nil
		}
		if e.Attempts >= 3 {
			e.Outcome = "failed"
			return true, nil
		}
		if !hasSlot {
			// The task guard reserves a delivery before its first Commit. It
			// still needs a verifier slot; absent is not a Commit acknowledgement.
			if !exists || reserved {
				return false, ErrDeliveryLimit
			}
			return false, nil
		}
		e.Outcome, e.Lease, e.Until = "pending", epoch, time.Now().UTC().Add(s.lease)
		if !exists || reserved {
			e.Publication = publication
		}
		e.Attempts++
		claimed, until = true, e.Until
		return true, nil
	})
	if err != nil {
		return DeliveryOutcome{}, err
	} // Includes unknown DB commit.
	if claimed {
		s.mu.Lock()
		if !s.closed {
			hasSlot = false // Worker owns the slot until it actually returns.
			s.wg.Add(1)
			job.Go(ctx, s.log, "computer.delivery.verify", job.Options{}, func(jobCtx context.Context) error {
				defer s.wg.Done()
				defer func() { <-s.slots }()
				return s.verify(jobCtx, id, delivery, ref, epoch, until)
			})
		}
		s.mu.Unlock()
	}
	return out, nil
}

func (s *AsyncDeliveryService) transact(ctx context.Context, id DeliveryIdentity, delivery string, ref DeliveryRequest, mutate func(*deliveryEntry, bool) (bool, error)) (DeliveryOutcome, error) {
	version := 2
	if _, ok := VerifiedTransferFromContext(ctx); ok {
		version = 3
	}
	for range 3 {
		var out DeliveryOutcome
		err := s.base.transactions(ctx, func(tx DeliveryTransaction) error {
			var err error
			out, err = mutateDeliveryTransaction(ctx, tx, id, delivery, ref, version, mutate)
			return err
		})
		if !errors.Is(err, ErrDeliveryRetry) {
			return out, err
		}
		if err := ctx.Err(); err != nil {
			return DeliveryOutcome{}, err
		}
	}
	return DeliveryOutcome{}, ErrDeliveryRetry
}

func (s *AsyncDeliveryService) verify(ctx context.Context, id DeliveryIdentity, delivery string, ref DeliveryRequest, epoch string, until time.Time) error {
	ctx, cancel := context.WithDeadline(ctx, until)
	stop := context.AfterFunc(s.life, cancel) //nolint:contextcheck // Also honor the independently owned server lifetime.
	defer stop()
	defer cancel()
	if err := s.check(ctx, id); err != nil {
		return err
	}
	err := s.base.verifyAsset(ctx, id.Binding.BotID, ref)
	// Reuse only the immutable reference and identity; a fresh deadline permits
	// persisting a failed scan after its cooperative deadline. Shutdown leaves
	// the lease for a later process to reconcile instead of extending lifetime.
	finish, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	stopFinish := context.AfterFunc(s.life, done) //nolint:contextcheck // Link server shutdown without extending its lifetime.
	defer stopFinish()
	defer done()
	if checkErr := s.check(finish, id); checkErr != nil {
		return checkErr
	}
	_, saveErr := s.transact(finish, id, delivery, ref, func(e *deliveryEntry, exists bool) (bool, error) {
		if !exists || e.Outcome != "pending" || e.Lease != epoch {
			return false, nil
		}
		e.Outcome = "failed"
		if err == nil && ctx.Err() == nil {
			e.Outcome = "committed"
		}
		return true, nil
	})
	if saveErr != nil {
		return saveErr
	}
	return err
}
