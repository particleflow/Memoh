package application

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

type nopFence struct{}

func (nopFence) Activate(context.Context, string, string, int64) error { return nil }

// A host result delivery (task result receipt, delegation delivery) projects no
// request user turn: its user row renders no bubble. Its turn source must still
// reach the subscriber on the first projection frame, carried by the run view
// itself — the persisted rows carry it only when history later replaces the
// projection, which is exactly the window that rendered the live receipt as an
// unknown source.
func TestAdmitTurnRunCarriesReceiptSourceOnFirstFrame(t *testing.T) {
	botID := uuid.NewString()
	sessionID := uuid.NewString()

	runs := sessionledger.New()
	manager := sessionruntime.NewManager(sessionruntime.NewMemoryBackend(), sessionruntime.Options{
		OwnerID:       "owner-receipt-source-test",
		StateTTL:      time.Minute,
		OwnerLeaseTTL: time.Second,
		Ledger:        runs,
		Fence:         nopFence{},
	})
	t.Cleanup(func() { _ = manager.Close() })

	sub, err := manager.Subscribe(context.Background(), botID, sessionID)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	t.Cleanup(sub.Close)

	// The receipt ingress resolves provenance through its host ledger and the
	// turn service stamps it on the run context before admission
	// (turn_service.go StartTurn); requestInputSource feeds it from the
	// delegation scope. Scope the same source here.
	runCtx := inputsource.WithContext(context.Background(), inputsource.Source{
		Kind: inputsource.Result, BotID: botID, SessionID: sessionID,
		TaskKind: "bot_delegation", TaskID: "task-7", TaskRunID: "run-7",
	})

	svc := &Service{logger: slog.New(slog.DiscardHandler), sessionRuntime: manager}
	cmd := turn.StartTurnCommand{
		TeamID:             "team-1",
		Mode:               turn.ModeChat,
		BotID:              botID,
		ThreadID:           sessionID,
		Query:              "Background task done.",
		UserVisibleText:    "Background task done.",
		DelegationResultID: "task-7",
		TaskResultReceipt:  "bot_delegation/task-7/evt-1/1",
		CurrentChannel:     "web",
		UserID:             "user-1",
	}
	admission, err := svc.admitTurnRun(runCtx, cmd, func() {}, nil, nil)
	if err != nil {
		t.Fatalf("admitTurnRun() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), 5*time.Second)
		defer cancel()
		_, _ = manager.FinishRun(ctx, admission.Handle, sessionruntime.RunStatusCompleted)
	})

	// The subscription baseline may precede admission; the first frame that
	// carries the admitted run is the one a subscriber attributes live output
	// from, so that is where the source must already be present. Activation
	// reaches subscribers as a delta whose current_run_view is the full run.
	deadline := time.After(5 * time.Second)
	var wire struct {
		RequestUserTurn json.RawMessage     `json:"request_user_turn"`
		InputSource     *inputsource.Source `json:"input_source"`
	}
	for {
		var event sessionruntime.Event
		select {
		case event = <-sub.C:
		case <-deadline:
			t.Fatal("timed out waiting for the admitted run's projection frame")
		}
		var run *sessionruntime.CurrentRunView
		if event.Snapshot != nil {
			run = event.Snapshot.CurrentRunView
		} else if event.Delta != nil && event.Delta.CurrentRunView != nil {
			run = event.Delta.CurrentRunView
		}
		// The admitting checkpoint precedes the admission builder's view; the
		// activation frame is the first one that owes the turn's attribution.
		if run == nil || run.Status != sessionruntime.RunStatusRunning {
			continue
		}
		data, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		break
	}
	if strings.TrimSpace(string(wire.RequestUserTurn)) != "" && string(wire.RequestUserTurn) != "null" {
		t.Fatalf("request_user_turn = %s, want none for a receipt-shaped turn", wire.RequestUserTurn)
	}
	if wire.InputSource == nil {
		t.Fatal("run view carries no input_source on the first frame — the live receipt renders as an unknown source")
	}
	if wire.InputSource.Kind != inputsource.Result || wire.InputSource.TaskID != "task-7" {
		t.Fatalf("run input_source = %+v, want the receipt source (task_result, task-7)", wire.InputSource)
	}
}
