package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestAdmissionMetadataDeadlineAndBounds(t *testing.T) {
	r, w := io.Pipe()
	defer func() { _ = r.Close(); _ = w.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := readAdmission(ctx, r); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked stdin escaped finite budget", err)
	}
	for _, body := range []string{`{"credential":"must-not-be-input"}`, `{} {}`, `{"source_ref":"` + strings.Repeat("x", 16385) + `"}`} {
		if _, err := readAdmission(t.Context(), strings.NewReader(body)); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}
