package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/testutil/sessionledger"
)

type abortObservationQueries struct {
	scopeMatched bool
	scopeCalls   atomic.Int32
	writeCh      chan sqlc.RecordSessionRunAbortControlParams
	release      <-chan struct{}
	reads        []sqlc.GetSessionRunAbortControlParams
	row          sqlc.GetSessionRunAbortControlRow
	err          error
}

func (q *abortObservationQueries) SessionRunMatchesAbortScope(context.Context, sqlc.SessionRunMatchesAbortScopeParams) (bool, error) {
	q.scopeCalls.Add(1)
	return q.scopeMatched, nil
}

func (q *abortObservationQueries) RecordSessionRunAbortControl(ctx context.Context, arg sqlc.RecordSessionRunAbortControlParams) (int64, error) {
	if q.writeCh != nil {
		q.writeCh <- arg
	}
	if q.release != nil {
		select {
		case <-q.release:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return 1, q.err
}

func (q *abortObservationQueries) GetSessionRunAbortControl(_ context.Context, arg sqlc.GetSessionRunAbortControlParams) (sqlc.GetSessionRunAbortControlRow, error) {
	q.reads = append(q.reads, arg)
	return q.row, q.err
}

func TestAbortObservationKeepsControlAcknowledgement(t *testing.T) {
	for _, storeErr := range []error{nil, errors.New("synthetic private diagnostic")} {
		t.Run(map[bool]string{true: "store fails", false: "store succeeds"}[storeErr != nil], func(t *testing.T) {
			var logs lockedBuffer
			const runID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
			h, botID, _ := controlFailureHandler(&logs, &controlFailureRuntime{abortApplied: true}, invocationTestSessionA)
			h.agentService = nil // Exercise the existing runtime-controller branch.
			q := &abortObservationQueries{err: storeErr, writeCh: make(chan sqlc.RecordSessionRunAbortControlParams, 1)}
			h.SetAbortObservationStore(q)
			writer := &wsWriter{ch: make(chan []byte, 1), stop: make(chan struct{})}
			h.abortWSRun(t.Context(), writer, botID, wsClientMessage{
				SessionID: invocationTestSessionA, ControlID: "synthetic-stop", AbortReason: "synthetic secret/URL/prompt",
			}, runID)
			select {
			case arg := <-q.writeCh:
				if arg.ClientReason != "unspecified" || arg.RunID.String() != runID || len(arg.RequestHash) != 64 {
					t.Fatalf("observation = %#v", arg)
				}
			case <-time.After(time.Second):
				t.Fatal("observation was not attempted")
			}
			ack := string(<-writer.ch)
			if !strings.Contains(ack, `"applied":true`) || !strings.Contains(ack, `"control_id":"synthetic-stop"`) {
				t.Fatalf("cancel acknowledgement changed: %s", ack)
			}
			if strings.Contains(ack+logs.String(), "synthetic secret") || strings.Contains(ack+logs.String(), "private diagnostic") {
				t.Fatal("observation leaked raw reason or storage diagnostics")
			}
		})
	}
}

func TestAbortObservationClientReasonsAreAnAllowlist(t *testing.T) {
	for _, reason := range []string{"user_stop", "bot_switch", "auth_reset", "workdir_mismatch", "client_cleanup"} {
		if safeAbortClientReason(reason) != reason {
			t.Fatalf("known reason rejected: %s", reason)
		}
	}
	for _, reason := range []string{"", "unspecified", "api_key=synthetic", "user_stop\nsecret", "first_send_timeout"} {
		if safeAbortClientReason(reason) != "unspecified" {
			t.Fatal("unknown or free-text reason retained")
		}
	}
}

func TestSessionInvocationProjectsAbortObservationWithoutPrivateFields(t *testing.T) {
	const runID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	runs := sessionledger.New()
	admitInvocationTestRun(t, runs, runID, invocationTestSessionA, invocationTestInvocation)
	requested := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	runs.Runs[runID].AbortRequestedAt = requested
	q := &abortObservationQueries{row: sqlc.GetSessionRunAbortControlRow{
		ReceivedAt:   pgtype.Timestamptz{Time: requested.Add(-time.Millisecond), Valid: true},
		Source:       "websocket",
		ClientReason: "bot_switch",
	}}
	h := newInvocationTestHandler(newInvocationTestQueries(invocationTestSession(invocationTestSessionA, invocationTestOwnerID)), runs)
	h.SetAbortObservationStore(q)
	for range 2 { // Fresh reads of the same identity, with no writes or controls.
		rec, err := callGetSessionInvocation(h, invocationTestSessionA, invocationTestInvocation, invocationTestOwnerID)
		if err != nil || rec.Code != http.StatusOK {
			t.Fatalf("lookup failed: %v, status %d", err, rec.Code)
		}
		body := decodeInvocationBody(t, rec)
		obs, ok := body["abort_control"].(map[string]any)
		if !ok || obs["source"] != "websocket" || obs["client_reason"] != "bot_switch" || body["abort_requested_at"] != requested.Format(time.RFC3339) {
			t.Fatalf("observation = %#v", body)
		}
		if sortedKeys(obs) != "client_reason,received_at,source" || strings.Contains(rec.Body.String(), "secret payload") {
			t.Fatalf("unexpected public fields: %s", rec.Body.String())
		}
	}
	if len(q.writeCh) != 0 || len(q.reads) != 2 || q.reads[0].BotID.String() != invocationTestBotID || q.reads[0].SessionID.String() != invocationTestSessionA || q.reads[0].RunID.String() != runID {
		t.Fatalf("lookup changed data or scope: %#v", q)
	}
}

func TestSessionInvocationOldAbortRemainsWithoutSource(t *testing.T) {
	const runID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	runs := sessionledger.New()
	admitInvocationTestRun(t, runs, runID, invocationTestSessionA, invocationTestInvocation)
	runs.Runs[runID].AbortRequestedAt = time.Now().UTC()
	q := &abortObservationQueries{}
	h := newInvocationTestHandler(newInvocationTestQueries(invocationTestSession(invocationTestSessionA, invocationTestOwnerID)), runs)
	h.SetAbortObservationStore(q)
	rec, err := callGetSessionInvocation(h, invocationTestSessionA, invocationTestInvocation, invocationTestOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	body := decodeInvocationBody(t, rec)
	if body["abort_requested_at"] == nil || body["abort_control"] != nil || len(q.writeCh) != 0 {
		t.Fatalf("old observation invented or backfilled: %#v", body)
	}
}

func TestSessionInvocationObservationUsesExistingSessionAuthorization(t *testing.T) {
	runs := sessionledger.New()
	admitInvocationTestRun(t, runs, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", invocationTestSessionA, invocationTestInvocation)
	q := &abortObservationQueries{}
	h := newInvocationTestHandler(newInvocationTestQueries(invocationTestSession(invocationTestSessionA, invocationTestOwnerID)), runs)
	h.SetAbortObservationStore(q)
	_, err := callGetSessionInvocation(h, invocationTestSessionA, invocationTestInvocation, invocationTestOtherUser)
	if err == nil || len(q.reads) != 0 || len(q.writeCh) != 0 {
		t.Fatalf("unauthorized lookup reached observations: err=%v reads=%d writes=%d", err, len(q.reads), len(q.writeCh))
	}
}

func TestAbortObservationSlowStoreDoesNotDelayCancellation(t *testing.T) {
	var logs lockedBuffer
	h, botID, _ := controlFailureHandler(&logs, &controlFailureRuntime{abortApplied: true}, invocationTestSessionA)
	h.agentService = nil
	release := make(chan struct{})
	defer close(release)
	q := &abortObservationQueries{writeCh: make(chan sqlc.RecordSessionRunAbortControlParams, 1), release: release}
	h.SetAbortObservationStore(q)
	writer := &wsWriter{ch: make(chan []byte, 1), stop: make(chan struct{})}
	returned := make(chan struct{})
	go func() {
		h.abortWSRun(t.Context(), writer, botID, wsClientMessage{SessionID: invocationTestSessionA, ControlID: "slow-observation", AbortReason: "user_stop"}, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
		close(returned)
	}()
	select {
	case <-returned:
		if ack := string(<-writer.ch); !strings.Contains(ack, `"applied":true`) {
			t.Fatalf("cancel not applied: %s", ack)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("cancel waited for the blocked observation store")
	}
}

func TestSessionInvocationObservationFailureKeepsAuthoritativeResult(t *testing.T) {
	runs := sessionledger.New()
	admitInvocationTestRun(t, runs, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", invocationTestSessionA, invocationTestInvocation)
	h := newInvocationTestHandler(newInvocationTestQueries(invocationTestSession(invocationTestSessionA, invocationTestOwnerID)), runs)
	h.SetAbortObservationStore(&abortObservationQueries{err: errors.New("synthetic private diagnostic")})
	rec, err := callGetSessionInvocation(h, invocationTestSessionA, invocationTestInvocation, invocationTestOwnerID)
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("observation failure hid invocation: %v", err)
	}
	body := decodeInvocationBody(t, rec)
	if body["found"] != true || body["state"] != "accepted" || body["abort_control"] != nil || strings.Contains(rec.Body.String(), "private diagnostic") {
		t.Fatalf("invalid degraded response: %s", rec.Body.String())
	}
}

type observedAbortRuntime struct {
	controlFailureRuntime
	calls atomic.Int32
}

func (r *observedAbortRuntime) AbortControl(context.Context, string, string, string, string) (bool, error) {
	r.calls.Add(1)
	return true, nil
}

func TestWSAbortOtherUsersSessionDoesNotObserveOrCancel(t *testing.T) {
	var logs lockedBuffer
	h, botID, user := controlFailureHandler(&logs, &controlFailureRuntime{}, invocationTestSessionA)
	h.sessionService = newThreadServiceForTest(newInvocationTestQueries(invocationTestSession(invocationTestSessionA, invocationTestOtherUser)))
	runtime := &observedAbortRuntime{}
	h.sessionRuntime = runtime
	q := &abortObservationQueries{writeCh: make(chan sqlc.RecordSessionRunAbortControlParams, 1)}
	h.SetAbortObservationStore(q)
	client := openLocalChannelTestWS(t, h, botID, user)
	if err := client.WriteJSON(map[string]any{"type": "abort", "session_id": invocationTestSessionA, "run_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "control_id": "unauthorized-stop", "abort_reason": "user_stop"}); err != nil {
		t.Fatal(err)
	}
	var ack wsOutboundEvent
	if err := client.ReadJSON(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Applied || ack.Code != "session.not_found" || runtime.calls.Load() != 0 || len(q.writeCh) != 0 {
		t.Fatalf("foreign session control reached execution/observation: ack=%#v calls=%d writes=%d", ack, runtime.calls.Load(), len(q.writeCh))
	}
}

func TestWSAbortMismatchedRunDoesNotObserveOrCancel(t *testing.T) {
	var logs lockedBuffer
	h, botID, user := controlFailureHandler(&logs, &controlFailureRuntime{}, invocationTestSessionA)
	runtime := &observedAbortRuntime{}
	h.sessionRuntime = runtime
	q := &abortObservationQueries{writeCh: make(chan sqlc.RecordSessionRunAbortControlParams, 1)}
	h.SetAbortObservationStore(q)
	client := openLocalChannelTestWS(t, h, botID, user)
	if err := client.WriteJSON(map[string]any{"type": "abort", "session_id": invocationTestSessionA, "run_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "control_id": "mismatched-stop"}); err != nil {
		t.Fatal(err)
	}
	var ack wsOutboundEvent
	if err := client.ReadJSON(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Applied || ack.Code != "session.not_found" || q.scopeCalls.Load() != 1 || runtime.calls.Load() != 0 || len(q.writeCh) != 0 {
		t.Fatalf("mismatched run reached execution/observation: ack=%#v scope=%d calls=%d writes=%d", ack, q.scopeCalls.Load(), runtime.calls.Load(), len(q.writeCh))
	}
}
