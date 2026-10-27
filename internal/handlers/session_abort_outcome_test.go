package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

func TestAbortOutcomePreservesAckAndClassifiesSafely(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		applied bool
		err     error
		class   string
	}{
		{"applied", true, nil, "none"}, {"not_applied", false, nil, "none"}, {"canceled", false, context.Canceled, "canceled"}, {"deadline", false, context.DeadlineExceeded, "deadline_exceeded"}, {"error", false, errors.New("SECRET-CONTROL-ERROR"), "control_error"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var logs lockedBuffer
			h, bot, _ := controlFailureHandler(&logs, &controlFailureRuntime{abortApplied: scenario.applied, abortErr: scenario.err}, invocationTestSessionA)
			h.agentService = nil
			release := make(chan struct{})
			defer close(release)
			q := &abortObservationQueries{release: release, completionCh: make(chan sqlc.CompleteSessionRunAbortControlParams, 1)}
			h.SetAbortObservationStore(q)
			writer := &wsWriter{ch: make(chan []byte, 1), stop: make(chan struct{})}
			const run = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
			h.abortWSRun(t.Context(), writer, bot, wsClientMessage{SessionID: invocationTestSessionA, ControlID: "synthetic-control", AbortReason: "SECRET-CLIENT-REASON"}, run)
			select {
			case out := <-q.completionCh:
				sum := sha256.Sum256([]byte("synthetic-control"))
				if out.RunID.String() != run || out.RequestHash != hex.EncodeToString(sum[:]) || out.ClientReason != "unspecified" || !out.CompletedAt.Valid || out.CompletedAt.Time.Before(out.ReceivedAt.Time) || !out.Applied.Valid || out.Applied.Bool != scenario.applied || out.ErrorClass.String != scenario.class || out.ServerSource.String != "runtime_controller" {
					t.Fatalf("wrong completion: %+v", out)
				}
			case <-time.After(time.Second):
				t.Fatal("completion blocked by incoming observation")
			}
			ack := string(<-writer.ch)
			if strings.Contains(ack, "SECRET-") {
				t.Fatal("private cancel diagnostic leaked")
			}
			var decoded wsOutboundEvent
			if err := json.Unmarshal([]byte(ack), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Applied != scenario.applied {
				t.Fatal("changed acknowledgement", ack)
			}
		})
	}
}

func TestAbortOutcomeAgentBranchAndProjection(t *testing.T) {
	// The observer records the explicitly server-selected branch, not a client claim.
	var logs lockedBuffer
	h, bot, _ := controlFailureHandler(&logs, &controlFailureRuntime{}, invocationTestSessionA)
	q := &abortObservationQueries{completionCh: make(chan sqlc.CompleteSessionRunAbortControlParams, 1)}
	h.SetAbortObservationStore(q)
	const run = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	complete := h.observeAbortControl(t.Context(), bot, invocationTestSessionA, run, "agent-control", "user_stop")
	complete(false, errors.New("SECRET-RESULT"), "agent_service")
	out := <-q.completionCh
	if out.ServerSource.String != "agent_service" || out.ErrorClass.String != "control_error" {
		t.Fatal(out)
	}
	runs := sessionledger.New()
	admitInvocationTestRun(t, runs, run, invocationTestSessionA, invocationTestInvocation)
	reader := newInvocationTestHandler(newInvocationTestQueries(invocationTestSession(invocationTestSessionA, invocationTestOwnerID)), runs)
	q.row = sqlc.GetSessionRunAbortControlRow{ReceivedAt: out.ReceivedAt, Source: "websocket", ClientReason: "user_stop", CompletedAt: out.CompletedAt, Applied: pgtype.Bool{Bool: false, Valid: true}, ErrorClass: out.ErrorClass, ServerSource: out.ServerSource}
	reader.SetAbortObservationStore(q)
	rec, err := callGetSessionInvocation(reader, invocationTestSessionA, invocationTestInvocation, invocationTestOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	body := decodeInvocationBody(t, rec)
	observation := body["abort_control"].(map[string]any)
	if observation["applied"] != false || observation["server_source"] != "agent_service" || observation["error_class"] != "control_error" || observation["completed_at"] == nil || strings.Contains(rec.Body.String(), "SECRET-") {
		t.Fatal(rec.Body.String())
	}
}
