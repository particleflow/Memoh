package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestResumeNativeHostUsesOriginalRunWithoutModelTurn(t *testing.T) {
	f, original := newBatchResumeFixture(t)
	finished, cancel := context.WithCancel(original)
	cancel()
	if _, err := toolcontext.ResolveComputerBinding(finished, toolcontext.Session{BotID: f.base.binding.BotID, SessionID: f.base.binding.SessionID}); err == nil {
		t.Fatal("finished original execution context remained usable")
	}
	b := f.base.binding
	b.RunID = ""
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	source := NewSource(f.restart())
	out, err := source.ResumeNative(ctx, b, f.op.ID, f.input)
	if err != nil || out.ID != f.op.ID || out.State != "submitted" {
		t.Fatalf("host resume state=%s error=%v", out.State, err)
	}
	f.assertIdentityAndCounts(t, 1, 1)
	if _, err := NewSource(f.restart()).ResumeNative(ctx, b, f.op.ID, f.input); err != nil {
		t.Fatal(err)
	}
	f.assertIdentityAndCounts(t, 1, 1)
}

func TestResumeNativeHostRejectsUntrustedAndChangedInput(t *testing.T) {
	for _, scenario := range []string{"unbound", "wrong-owner", "wrong-run", "empty-operation", "injected-resume", "changed-task", "changed-directory", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			f, _ := newBatchResumeFixture(t)
			b := f.base.binding
			b.RunID = ""
			ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
			if err != nil {
				t.Fatal(err)
			}
			in, id := f.input, f.op.ID
			switch scenario {
			case "unbound":
				ctx = t.Context()
			case "wrong-owner":
				b.UserID = "untrusted-user"
			case "wrong-run":
				b.RunID = "untrusted-run"
				ctx, err = toolcontext.WithComputerBinding(t.Context(), b)
				if err != nil {
					t.Fatal(err)
				}
			case "empty-operation":
				id = ""
			case "injected-resume":
				in.ResumeOperationID = f.op.ID
			case "changed-task":
				in.Task += " changed"
			case "changed-directory":
				in.WorkDir = "other"
			case "revoked":
				f.policyDenied.Store(true)
			}
			before, _ := json.Marshal(f.base.store.records)
			if _, err := NewSource(f.service()).ResumeNative(ctx, b, id, in); err == nil {
				t.Fatal("unsafe continuation accepted")
			}
			after, _ := json.Marshal(f.base.store.records)
			if string(before) != string(after) || len(f.calls) != 1 {
				t.Fatal("refused continuation changed record or reached native")
			}
			f.assertIdentityAndCounts(t, 0, 0)
		})
	}
}

func TestResumeNativeHostDisconnectedPreservesOriginalOperation(t *testing.T) {
	f, _ := newBatchResumeFixture(t)
	b := f.base.binding
	b.RunID = ""
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	service := f.service()
	service.authorize = func(context.Context, toolcontext.ComputerBinding, string, string, string) error {
		return ErrPreflightUnavailable
	}
	before, _ := json.Marshal(f.base.store.records)
	out, err := NewSource(service).ResumeNative(ctx, b, f.op.ID, f.input)
	if !errors.Is(err, agenvo.ErrUnavailable) || out.ID != f.op.ID || out.State != "unknown" {
		t.Fatalf("disconnected state=%s err=%v", out.State, err)
	}
	after, _ := json.Marshal(f.base.store.records)
	if string(before) != string(after) || len(f.calls) != 1 {
		t.Fatal("disconnected continuation mutated or reached native")
	}
	f.assertIdentityAndCounts(t, 0, 0)
}

func TestResumeNativeHostUnknownNeverReplaysPrompt(t *testing.T) {
	f, _ := newBatchResumeFixture(t)
	f.lostMethod = "pane.run"
	b := f.base.binding
	b.RunID = ""
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSource(f.service()).ResumeNative(ctx, b, f.op.ID, f.input); !errors.Is(err, agenvo.ErrUnknown) {
		t.Fatalf("lost prompt receipt=%v", err)
	}
	if _, err := NewSource(f.restart()).ResumeNative(ctx, b, f.op.ID, f.input); !errors.Is(err, agenvo.ErrUnknown) {
		t.Fatalf("uncertain prompt fence=%v", err)
	}
	f.assertIdentityAndCounts(t, 1, 1)
}
