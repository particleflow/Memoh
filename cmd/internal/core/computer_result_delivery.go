package core

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/felinics/memoh/internal/taskresult"
	"github.com/felinics/memoh/internal/toolcontext"
)

// computerResultObserver is the trusted host read the scan uses to pull an
// operation's terminal observation while its conversation is idle. It is the
// same authorized refresh computer_task_result performs inside a turn.
type computerResultObserver interface {
	Observe(context.Context, toolcontext.ComputerBinding, string) (computertask.Result, error)
}

// scanComputerResults extends the existing result scan with PC tasks. The
// endpoint's verified delivery commit is the only idle-time completion signal:
// the scan first pulls operations with committed deliveries to a terminal Pi
// outcome, then flows through the shared reconcile/enqueue path.
func (h *delegationHost) scanComputerResults(ctx context.Context) error {
	rows, err := h.store.ListComputerResultTasks(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		bot, user, session := row.BotID.String(), row.UserID.String(), row.SessionID.String()
		if err := h.pullComputerResult(ctx, row.ID.String(), bot, user, session); err != nil {
			failures = append(failures, err)
			continue
		}
		err := h.reconcileResult(ctx, "computer_task", row.ID.String(), bot, user, session)
		if err != nil && !errors.Is(err, errResultNotReady) && !errors.Is(err, botdelegation.ErrDenied) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// pullComputerResult performs one bounded observation read for an unfinished
// pi-json operation whose endpoint already committed a delivery. Failures stay
// pull-only: the next wake retries with the same immutable evidence.
func (h *delegationHost) pullComputerResult(ctx context.Context, id, bot, user, session string) error {
	if h.computerSource == nil {
		return nil
	}
	r, err := h.store.ReadComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id, BotID: bot, SessionID: session, UserID: user})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	var d computertask.Details
	if json.Unmarshal(r.Details, &d) != nil || d.Batch == nil || computerOutcomeFinal(r.State, d) {
		return nil
	}
	if _, _, err := h.computerResultRouting(ctx, bot, user, session); err != nil {
		return nil
	}
	committed, err := h.computerDeliveryCommitted(ctx, r)
	if err != nil {
		return err
	}
	if !committed {
		return nil
	}
	binding := toolcontext.ComputerBinding{BotID: bot, UserID: user, SessionID: session, TargetID: r.TargetID, RunID: r.RunID}
	observeCtx, err := toolcontext.WithComputerBinding(ctx, binding)
	if err != nil {
		return nil
	}
	if h.computerResume != nil {
		if observeCtx, err = h.computerResume(observeCtx, binding); err != nil {
			return nil
		}
	}
	_, _ = h.computerSource.Observe(observeCtx, binding, id)
	return nil
}

// computerResultRouting reuses the native/delegation source gates: an owned
// live root chat conversation routed through Web with a model runtime. Tasks
// submitted by subagent or delegation child sessions, external channels or
// non-model runtimes never reconnect on their own.
func (h *delegationHost) computerResultRouting(ctx context.Context, bot, user, session string) (botdelegation.Identity, botdelegation.ReturnRoute, error) {
	s, err := h.sessions.Get(ctx, session)
	if err != nil {
		return botdelegation.Identity{}, botdelegation.ReturnRoute{}, err
	}
	if s.ID != session || s.BotID != bot || s.CreatedByUserID != user || s.Type != thread.TypeChat || s.ParentThreadID != "" {
		return botdelegation.Identity{}, botdelegation.ReturnRoute{}, botdelegation.ErrDenied
	}
	b, err := h.service.Authority.Source(ctx, user, bot, session)
	if err != nil {
		return botdelegation.Identity{}, botdelegation.ReturnRoute{}, err
	}
	routing, err := h.SourceRoute(ctx, b)
	if err != nil {
		return botdelegation.Identity{}, botdelegation.ReturnRoute{}, err
	}
	if routing.Channel != "web" || s.RuntimeType != "" && s.RuntimeType != thread.RuntimeModel {
		return botdelegation.Identity{}, botdelegation.ReturnRoute{}, botdelegation.ErrDenied
	}
	return b, routing, nil
}

func (h *delegationHost) computerDeliveryCommitted(ctx context.Context, r dbstore.ComputerOperationRecord) (bool, error) {
	entries, err := h.store.ListComputerDeliveryEntries(ctx, r, "", 16)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		var persisted struct {
			Outcome string `json:"outcome"`
		}
		if json.Unmarshal(entry.Data, &persisted) == nil && persisted.Outcome == "committed" {
			return true, nil
		}
	}
	return false, nil
}

// computerTerminalObserved decides between an automatic reconnect and a
// covered in-turn read. A runtime fence belongs to a live turn of the owning
// conversation: that turn already holds the outcome and reports it, so the
// event is pinned as attention instead of enqueueing a duplicate turn.
func (h *delegationHost) computerTerminalObserved(ctx context.Context, r dbstore.ComputerOperationRecord, d computertask.Details) {
	if _, fenced := runtimefence.FromContext(ctx); fenced {
		h.markComputerResultAttention(ctx, r, d)
		return
	}
	h.wakeResults(ctx)
}

func (h *delegationHost) markComputerResultAttention(ctx context.Context, r dbstore.ComputerOperationRecord, d computertask.Details) {
	if !computerOutcomeFinal(r.State, d) {
		return
	}
	if _, _, err := h.computerResultRouting(ctx, r.BotID, r.UserID, r.SessionID); err != nil {
		return
	}
	state, text := computerResultState(r.State), computerResultText(r, d)
	key := taskresult.EventID(state, r.RunID, text)
	raw, err := h.store.ReadComputerResultDelivery(ctx, nativeTaskUUID(r.ID))
	if err != nil {
		return
	}
	delivery := taskresult.Delivery{}
	if len(raw) != 0 {
		if json.Unmarshal(raw, &delivery) != nil {
			return
		}
	}
	if _, exists := delivery[key]; exists {
		return
	}
	delivery[key] = taskresult.Event{ReportState: "attention", State: state, RunID: r.RunID, Text: text, Attempt: 1}
	next, _ := json.Marshal(delivery)
	_, _ = h.store.SaveComputerResultDelivery(ctx, sqlc.SaveComputerResultDeliveryParams{ID: nativeTaskUUID(r.ID), Expected: raw, Delivery: next})
}

// computerOutcomeFinal reports whether the persisted receipt already carries
// the immutable business outcome a reconnect can report. pi_exited_* phases
// stay pull-only until the completion descriptor is parsed.
func computerOutcomeFinal(state string, d computertask.Details) bool {
	return d.Batch != nil && d.Batch.Result != nil && (state == "completed" || state == "not_started" || state == "rejected")
}

// computerResultState maps the operation state onto the shared result states.
// The invocation slot release ("completed") is reported together with the Pi
// outcome in the receipt text; not started or rejected executions failed.
func computerResultState(state string) string {
	switch state {
	case "completed":
		return "completed"
	case "not_started", "rejected":
		return "failed"
	default:
		return "unknown"
	}
}

// computerResultText is the stable receipt body: header, parsed outcome, exit
// code, bounded output and the immutable declared artifact names. Volatile
// delivery progress is deliberately excluded so the event key never changes.
func computerResultText(r dbstore.ComputerOperationRecord, d computertask.Details) string {
	outcome := ""
	if d.Batch != nil && d.Batch.Result != nil {
		outcome = d.Batch.Result.Outcome
	}
	text := "Computer task " + r.ID + " (target " + r.TargetID + ", kind " + d.Kind + ") Pi outcome: " + outcome + "."
	if d.ExitCode != nil {
		text += " Exit code: " + strconv.Itoa(*d.ExitCode) + "."
	}
	if output := strings.TrimSpace(d.Output); output != "" {
		if bounded := []rune(output); len(bounded) > 1500 {
			output = string(bounded[:1500]) + " …[truncated]"
		}
		text += "\n\n" + output
	}
	if d.Batch != nil && d.Batch.Progress.Producer != nil {
		for _, artifact := range d.Batch.Progress.Producer.FilePlans() {
			text += "\nDeclared result file: " + artifact.Artifact.SourceRef + " (" + artifact.Artifact.Mime + ")"
		}
	}
	return text
}

// ComputerDeliveryCommitWake exposes the existing result-delivery wake for the
// endpoint transfer path: a verified delivery commit is the server's only
// idle-time completion signal for a PC task. The callback only signals.
func ComputerDeliveryCommitWake(delegations *botdelegation.Service) func(context.Context, computertask.DeliveryIdentity) {
	host, ok := delegations.Host.(*delegationHost)
	if !ok || host == nil {
		return nil
	}
	return func(ctx context.Context, _ computertask.DeliveryIdentity) {
		host.wakeResults(ctx)
	}
}
