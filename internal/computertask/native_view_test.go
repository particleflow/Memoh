package computertask

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestNativeContentReadOnlyBoundedAndRevocation(t *testing.T) {
	source, store, transport, _, initial := evidenceFixture(t)
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	before := observationSnapshot(t, store)
	transport.output = strings.Repeat("sample", 4000)
	got, err := source.NativeContent(ctx, b, initial.ID)
	if err != nil || !got.Supported || got.Observation == nil || got.Observation.OutputFidelity != "terminal_snapshot_unverified" || !got.Observation.Truncated || len(got.Observation.Output) > 16384 || got.Observation.TaskSuccessVerified {
		t.Fatalf("sample=%+v err=%v", got, err)
	}
	if !bytes.Equal(before, observationSnapshot(t, store)) || !slices.Equal(transport.base.methods, []string{"agent.get", "agent.read"}) {
		t.Fatal("content read wrote evidence or called another method")
	}
	transport.readFailure = true
	got, err = source.NativeContent(ctx, b, initial.ID)
	if err != nil || got.Observation != nil || got.Availability != "connection_unavailable" {
		t.Fatalf("failed read retained output: %+v %v", got, err)
	}
	transport.afterRead = func() { store.revoked = true }
	_, err = source.NativeContent(ctx, b, initial.ID)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("late grant: %v", err)
	}
	if !bytes.Equal(before, observationSnapshot(t, store)) {
		t.Fatal("failed reads changed evidence")
	}
}

func TestNativeViewFollowupKeepsOperationRunAndUnknownDedup(t *testing.T) {
	source, store, transport, initial := followupFixture(t, true)
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	transport.fault = "lost"
	in := Followup{OperationID: initial.ID, RequestID: uuid.NewString(), Mode: "bound_session", Task: "synthetic followup"}
	got, err := source.FollowupNative(ctx, b, in)
	if !errors.Is(err, agenvo.ErrUnknown) || got.Followup == nil || got.Followup.State != "unknown" {
		t.Fatalf("out=%+v err=%v", got, err)
	}
	originalRun := store.records[initial.ID].RunID
	var d Details
	if err := json.Unmarshal(store.records[initial.ID].Details, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Followups) != 1 || d.Followups[0].RunID != originalRun {
		t.Fatal("host changed persisted run attribution")
	}
	_, _ = source.FollowupNative(ctx, b, in)
	in.RequestID = uuid.NewString()
	got, err = source.FollowupNative(ctx, b, in)
	if !errors.Is(err, ErrConflict) || got.BlockedByRequest == "" || len(transport.prompts) != 1 {
		t.Fatal("unknown submitted twice")
	}
}

func TestNativeContentBatchUnsupportedWithoutRemoteIO(t *testing.T) {
	source, store, transport, initial := followupFixture(t, true)
	r := store.records[initial.ID]
	var d Details
	if err := json.Unmarshal(r.Details, &d); err != nil {
		t.Fatal(err)
	}
	d.Batch = &BatchDetails{}
	r.Details, _ = json.Marshal(d)
	store.records[initial.ID] = r
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.NativeContent(ctx, b, initial.ID)
	if err != nil || got.Supported || got.FollowupAvailable || len(transport.methods) != 0 {
		t.Fatalf("batch content: %+v %v", got, err)
	}
	_, err = source.FollowupNative(ctx, b, Followup{OperationID: initial.ID, RequestID: uuid.NewString(), Mode: "current_agent", Task: "synthetic"})
	if !errors.Is(err, agenvo.ErrUnsupported) || len(transport.methods) != 0 {
		t.Fatalf("batch followup: %v", err)
	}
}
