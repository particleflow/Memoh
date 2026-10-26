package agenvo

import (
	"errors"
	"testing"
)

func TestBatchReadTracePreservesSentinelIdentity(t *testing.T) {
	trace := &BatchReadTrace{}
	ctx := WithBatchReadTrace(t.Context(), trace)
	for _, err := range []error{ErrUnknown, ErrTargetChanged, ErrBatchControlChanged, errors.New("PRIVATE_NATIVE_ERROR")} {
		got := traceBatchReadFailure(ctx, "snapshot", "native_error", err)
		//nolint:errorlint // Exact sentinel identity, not only unwrapping, is the invariant under test.
		if got != err || !errors.Is(got, err) {
			t.Fatal("diagnostic wrapped or replaced error")
		}
		if trace.ReadStage != "snapshot" || trace.Reason != "native_error" {
			t.Fatal("missing safe diagnostic")
		}
	}
	//nolint:errorlint // The optional diagnostic must return the identical original sentinel.
	if got := traceBatchReadFailure(t.Context(), "frame", "short_page", ErrUnknown); got != ErrUnknown {
		t.Fatal("untraced behavior changed")
	}
}
