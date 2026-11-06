//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestWindowsNativeBudgetPublicationRecovery(t *testing.T) {
	limits := nativeLimits()
	limits.Bytes = 8 << 30
	if path := os.Getenv("MEMOH_NATIVE_BUDGET_ROOT"); path != "" {
		q, err := openQueueLayoutMode(func(string) string { return path }, "revision-1", limits, false, false, true, func(stage string) {
			if stage == os.Getenv("MEMOH_NATIVE_BUDGET_STAGE") {
				os.Exit(85)
			}
		})
		if q != nil {
			_ = q.Close()
		}
		t.Fatal("hook missing", err)
	}
	for _, stage := range []string{"budget_after_base", "budget_after_gate", "budget_after_publication", "budget_after_commit"} {
		t.Run(stage, func(t *testing.T) {
			root := nativeFixtureRoot(t, false)
			env := func(string) string { return root }
			q := nativeQueue(t, root)
			data := []byte("retained native budget fixture")
			ref, source := publicationFixtureSource(t, q, data)
			authorize := func(ctx context.Context, _ Reference) error { return ctx.Err() }
			p, err := q.OpenNativePublication(authorize)
			if err != nil {
				t.Fatal(err)
			}
			if outcome, err := p.Publish(t.Context(), ref, source); err != nil || outcome != OutcomeCommitted {
				t.Fatal(outcome, err)
			}
			source.active = false
			_ = source.file.Close()
			fenced := metadataReference("budget-fenced")
			if outcome, err := p.Resolve(t.Context(), fenced, true); err != nil || outcome != OutcomeCanceled {
				t.Fatal(outcome, err)
			}
			backup := backupNativePublicationLayout(t, q.root)
			lock, err := p.lock()
			if err != nil {
				t.Fatal(err)
			}
			if err := MigrateHerdrBudget(t.Context(), env, "revision-1", limits); !errors.Is(err, ErrBusy) {
				t.Fatal("publication lock", err)
			}
			unlockClose(lock)
			_ = p.Close()
			_ = q.Close()
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWindowsNativeBudgetPublicationRecovery$") //nolint:gosec // Isolated current test executable, fixed action.
			cmd.Env = append(os.Environ(), "MEMOH_NATIVE_BUDGET_ROOT="+root, "MEMOH_NATIVE_BUDGET_STAGE="+stage)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 85 {
				t.Fatalf("child: %v %s", err, out)
			}
			if err := MigrateHerdrBudget(t.Context(), env, "revision-1", limits); err != nil {
				t.Fatal(err)
			}
			q, err = OpenHerdrQueue(env, "revision-1", limits)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = q.Close() }()
			p, err = q.OpenNativePublication(authorize)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = p.Close() }()
			if outcome, err := p.Resolve(t.Context(), fenced, false); err != nil || outcome != OutcomeCanceled {
				t.Fatal("tombstone", outcome, err)
			}
			if outcome, err := p.Resolve(t.Context(), ref, false); err != nil || outcome != OutcomeCommitted {
				t.Fatal("published proof", outcome, err)
			}
			reader, err := p.OpenPublished(t.Context(), ref)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || !bytes.Equal(got, data) {
				t.Fatal("object changed", err)
			}
			backup()
		})
	}
}
