//go:build linux || darwin

package transferclient

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func readyAssociationQueue(t *testing.T, path string) (*DiskQueue, *Client) {
	t.Helper()
	q := openQueue(t, path, queueLimits())
	admitSource(t, q, []byte("result"))
	_, c := newFixture(t)
	for n := 0; n < 8 && inspectQueue(t, q, "delivery-1").Job.Phase != "ready"; n++ {
		runQueue(t, q, c, nil)
	}
	return q, c
}

func TestAssociationReviewTimeoutIsUnknownAndRecoverable(t *testing.T) {
	for _, mode := range []string{"deadline", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			path := t.TempDir()
			q, c := readyAssociationQueue(t, path)
			committed := false
			commits, resolves := 0, 0
			binding := &AssociationBinding{
				Commit: func(ctx context.Context, _ string, _ Reference, _ io.Reader) error {
					commits++
					r, f, err := q.load("delivery-1")
					if err != nil {
						t.Fatal(err)
					}
					_ = f.Close()
					if r.AssociationState != AssociationInFlight {
						t.Fatal("commit preceded durable in-flight marker")
					}
					committed = true
					if mode == "deadline" {
						short, cancel := context.WithTimeout(ctx, time.Millisecond)
						defer cancel()
						<-short.Done()
						return short.Err()
					}
					short, cancel := context.WithCancel(ctx)
					cancel()
					return short.Err()
				},
				Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
					resolves++
					if !committed {
						t.Fatal("fixture did not commit")
					}
					return OutcomeCommitted, nil
				},
			}
			r := runQueue(t, q, c, binding)
			if r.AssociationBlocked || r.AssociationState != AssociationUnknown {
				t.Fatal("ambiguous context failure lost unknown state")
			}
			_ = q.Close()
			q = openQueue(t, path, queueLimits())
			r = runQueue(t, q, c, binding)
			if !r.Associated || commits != 1 || resolves != 1 {
				t.Fatal("committed association not reconciled exactly once")
			}
		})
	}
}

func TestAssociationReviewCancelMustReconcileLostCommit(t *testing.T) {
	q, c := readyAssociationQueue(t, t.TempDir())
	commits, resolves := 0, 0
	binding := &AssociationBinding{
		Commit: func(context.Context, string, Reference, io.Reader) error { commits++; return ErrTransport },
		Resolve: func(_ context.Context, _ string, _ Reference, cancel bool) (AssociationOutcome, error) {
			resolves++
			if !cancel {
				t.Fatal("cancel intent missing from resolve")
			}
			return OutcomeCommitted, nil
		},
	}
	runQueue(t, q, c, binding)
	if err := q.RequestCancel("delivery-1"); err != nil {
		t.Fatal(err)
	}
	r := runQueue(t, q, c, binding)
	if !r.Associated || !r.Job.CancelRequested || r.AssociationState != AssociationCommitted || commits != 1 || resolves != 1 {
		t.Fatal("cancel hid a committed result")
	}
}

func TestAssociationReviewAbsentQueryDoesNotErasePendingCommit(t *testing.T) {
	for _, lateWins := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel-fences-late-commit", true: "commit-before-cancel-resolve"}[lateWins], func(t *testing.T) {
			q, c := readyAssociationQueue(t, t.TempDir())
			pending := false
			committed := false
			tombstone := false
			binding := &AssociationBinding{
				Commit: func(context.Context, string, Reference, io.Reader) error { pending = true; return ErrTransport },
				Resolve: func(_ context.Context, _ string, _ Reference, cancel bool) (AssociationOutcome, error) {
					if committed {
						return OutcomeCommitted, nil
					}
					if cancel {
						tombstone = true
						return OutcomeCanceled, nil
					}
					return OutcomeAbsent, nil
				},
			}
			runQueue(t, q, c, binding)
			r := runQueue(t, q, c, binding)
			if r.AssociationState != AssociationRetryCommit || !pending {
				t.Fatal("absent query erased unknown original request")
			}
			if lateWins {
				committed = true
			}
			if err := q.RequestCancel("delivery-1"); err != nil {
				t.Fatal(err)
			}
			r = runQueue(t, q, c, binding)
			if !tombstone {
				committed = true
			} // Delayed original request is checked against same fence.
			if lateWins {
				if !r.Associated || !committed {
					t.Fatal("committed race lost")
				}
			} else if committed || r.AssociationState != AssociationCanceled || r.Job.Phase != "canceled" || r.Associated {
				t.Fatal("cancel did not fence delayed commit")
			}
		})
	}
}

func TestAssociationReviewRetryPauseCanBeReconciled(t *testing.T) {
	q, c := readyAssociationQueue(t, t.TempDir())
	calls := 0
	binding := &AssociationBinding{Commit: func(context.Context, string, Reference, io.Reader) error { calls++; return context.DeadlineExceeded }, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		calls++
		return "", context.Canceled
	}}
	for range 14 {
		runQueue(t, q, c, binding)
	}
	r := inspectQueue(t, q, "delivery-1")
	if calls != 12 || !r.AssociationBlocked || r.AssociationState != AssociationUnknown {
		t.Fatal("retry budget discarded unknown outcome", calls)
	}
	binding.Resolve = func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		return OutcomeCommitted, nil
	}
	if err := q.ResumeAssociation("delivery-1"); err != nil {
		t.Fatal(err)
	}
	if !runQueue(t, q, c, binding).Associated {
		t.Fatal("paused unknown cannot reconcile")
	}
}

func TestAssociationReviewCancelWithoutFenceRemainsUnknown(t *testing.T) {
	q, c := readyAssociationQueue(t, t.TempDir())
	binding := &AssociationBinding{Commit: func(context.Context, string, Reference, io.Reader) error { return ErrTransport }, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) { return OutcomeAbsent, nil }}
	runQueue(t, q, c, binding)
	if err := q.RequestCancel("delivery-1"); err != nil {
		t.Fatal(err)
	}
	r := runQueue(t, q, c, binding)
	if r.AssociationState != AssociationUnknown || r.Job.Phase != "ready" || r.Associated {
		t.Fatal("plain absent lookup falsely confirmed cancellation")
	}
	binding.Resolve = func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		return "", ErrAuthorization
	}
	r = runQueue(t, q, c, binding)
	if r.AssociationState != AssociationUnknown || !r.AssociationBlocked {
		t.Fatal("revoked lookup erased unknown commit")
	}
}

func TestAssociationReviewCancelBeforeCommitDoesNotRegister(t *testing.T) {
	q, c := readyAssociationQueue(t, t.TempDir())
	if err := q.RequestCancel("delivery-1"); err != nil {
		t.Fatal(err)
	}
	r := runQueue(t, q, c, nil)
	if r.Job.Phase != "canceled" || r.AssociationState != AssociationCanceled || r.Associated {
		t.Fatal("never-attempted association cancellation failed")
	}
}

func TestAssociationReviewUnknownDownloadResolvesWithoutRepublishing(t *testing.T) {
	q, c := readyAssociationQueue(t, t.TempDir())
	r, f, err := q.load("delivery-1")
	if err != nil {
		t.Fatal(err)
	}
	r.Direction = "download"
	r.AssociationState = AssociationUnknown
	r.AssociationAttempts = 1
	if err := q.save(f, r); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	// There is intentionally no local .part. Reconciliation must not require
	// rereading/publishing it when the remote destination already accepted it.
	binding := &AssociationBinding{Commit: func(context.Context, string, Reference, io.Reader) error {
		t.Fatal("download republished during resolve")
		return nil
	}, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		return OutcomeCommitted, nil
	}}
	if !runQueue(t, q, c, binding).Associated {
		t.Fatal("download commit could not be resolved")
	}
}

func TestAssociationReviewProcessCrashAfterInFlight(t *testing.T) {
	if path := os.Getenv("MEMOH_ASSOCIATION_CRASH_FIXTURE"); path != "" {
		q := openQueue(t, path, queueLimits())
		_, c := newFixture(t)
		binding := &AssociationBinding{Commit: func(context.Context, string, Reference, io.Reader) error { os.Exit(24); return nil }, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
			t.Fatal("unexpected child resolve")
			return "", ErrProtocol
		}}
		if err := q.RunOne(t.Context(), "delivery-1", c, binding, time.Now()); err != nil {
			t.Fatal(err)
		}
		return
	}
	path := t.TempDir()
	q, c := readyAssociationQueue(t, path)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestAssociationReviewProcessCrashAfterInFlight$") //nolint:gosec // Executes only this test binary and fixed selector.
	cmd.Env = append(os.Environ(), "MEMOH_ASSOCIATION_CRASH_FIXTURE="+path)
	var exit *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != 24 {
		t.Fatal("child did not crash at commit", err)
	}
	r := inspectQueue(t, q, "delivery-1")
	if r.AssociationState != AssociationInFlight {
		t.Fatal("crash marker not durable")
	}
	binding := &AssociationBinding{Commit: func(context.Context, string, Reference, io.Reader) error {
		t.Fatal("blind commit after crash")
		return nil
	}, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		return OutcomeCommitted, nil
	}}
	if !runQueue(t, q, c, binding).Associated {
		t.Fatal("in-flight crash not resolved")
	}
	if _, err := os.Stat(filepath.Join(path, "deliveries-v1", "delivery-1.journal")); err != nil {
		t.Fatal(err)
	}
}
