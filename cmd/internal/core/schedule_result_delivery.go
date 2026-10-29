package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/schedule"
	"github.com/felinics/memoh/internal/taskresult"
)

// This host-owned snapshot is never accepted from model arguments or HTTP JSON.
// Legacy schedules have no snapshot and are not silently enrolled in delivery.
type scheduleResultOrigin struct {
	Version  int                       `json:"version"`
	Identity botdelegation.Identity    `json:"identity"`
	Route    botdelegation.ReturnRoute `json:"route"`
}

type scheduleContactProvider struct {
	base *tools.ScheduleProvider
	host *delegationHost
}

func (p *scheduleContactProvider) Usage(ctx context.Context, s tools.SessionContext, available tools.AvailableTools) string {
	if s.CurrentPlatform != "web" || s.SessionType != "chat" {
		return p.base.Usage(ctx, s, available)
	}
	// Main-contact schedules have a host-owned return path. Do not ask the
	// execution model to guess a person/channel or send a second summary.
	parts := []string{"Scheduled tasks run in independent execution contexts. Their saved results are returned here for you to summarize; do not add a separate send action merely to report completion.", "Interpret cron patterns in the effective timezone in the system context. After creation succeeds, confirm and end the turn; never wait for the schedule to fire.", "Use new_session execution; existing_session is unavailable for contact schedules."}
	for _, id := range []tools.ToolName{tools.ToolCreateSchedule(), tools.ToolListSchedule(), tools.ToolGetSchedule(), tools.ToolUpdateSchedule(), tools.ToolDeleteSchedule()} {
		if ref, ok := available.Ref(id); ok {
			parts = append(parts, ref)
		}
	}
	return strings.Join(parts, "\n")
}

func (p *scheduleContactProvider) Tools(ctx context.Context, s tools.SessionContext) ([]toolexec.Tool, error) {
	list, err := p.base.Tools(ctx, s)
	if err != nil || s.CurrentPlatform != "web" || s.SessionType != "chat" {
		return list, err
	}
	for i := range list {
		if list[i].Name != tools.ToolCreateSchedule().String() {
			continue
		}
		list[i].Description = "Create a cron schedule from this fixed Bot contact. Fill name, description, pattern and command. Each fire runs independently and its saved result returns to this contact for a natural summary. Do not set session_id or add a separate send action merely to report completion. Optional model_id, acp_agent_id/acp_model_id, workdir_id and reasoning_effort select the execution configuration. max_calls=null means unlimited."
		execute := list[i].Execute
		list[i].Execute = func(call *toolexec.ToolExecContext, args sdk.ToolArguments) (sdk.ToolOutput, error) {
			// Resolve at execution, not descriptor construction: permissions may change
			// while the model is thinking. Nothing from args chooses the return route.
			b, e := p.host.service.Authority.Source(call.Context, s.UserID, s.BotID, s.SessionID)
			if e != nil {
				return sdk.ToolOutput{}, e
			}
			route, e := p.host.SourceRoute(call.Context, b)
			if e != nil {
				return sdk.ToolOutput{}, e
			}
			raw, e := json.Marshal(scheduleResultOrigin{Version: 1, Identity: b, Route: route})
			if e != nil {
				return sdk.ToolOutput{}, e
			}
			if _, e = p.host.AuthorizeScheduleResultOrigin(call.Context, s.BotID, raw); e != nil {
				return sdk.ToolOutput{}, e
			}
			bound := *call
			bound.Context = schedule.WithResultOrigin(call.Context, raw)
			return execute(&bound, args)
		}
	}
	return list, nil
}

func (h *delegationHost) AuthorizeScheduleResultOrigin(ctx context.Context, bot string, raw []byte) (string, error) {
	_, b, _, err := h.scheduleOrigin(ctx, bot, raw)
	return b.UserID, err
}

func (h *delegationHost) scheduleOrigin(ctx context.Context, bot string, raw []byte) (scheduleResultOrigin, botdelegation.Identity, botdelegation.ReturnRoute, error) {
	var origin scheduleResultOrigin
	var b botdelegation.Identity
	var route botdelegation.ReturnRoute
	deny := func() (scheduleResultOrigin, botdelegation.Identity, botdelegation.ReturnRoute, error) {
		return origin, b, route, botdelegation.ErrDenied
	}
	if json.Unmarshal(raw, &origin) != nil || origin.Version != 1 || origin.Identity.SourceBotID != bot || origin.Identity.SourceSessionID != origin.Identity.SourceRootID {
		return deny()
	}
	var err error
	b, err = h.service.Authority.Source(ctx, origin.Identity.UserID, bot, origin.Identity.SourceSessionID)
	if err != nil {
		return origin, b, route, err
	}
	if b != origin.Identity {
		return deny()
	}
	route, err = h.SourceRoute(ctx, b)
	if err != nil {
		return origin, b, route, err
	}
	if route.Channel != "web" {
		return deny()
	}
	if _, err = validateDelegationReturnRoute(&origin.Route, route); err != nil {
		return deny()
	}
	id, err := h.store.ReadScheduleResultContact(ctx, sqlc.ReadScheduleResultContactParams{BotID: nativeTaskUUID(bot), UserID: b.UserID, SessionID: nativeTaskUUID(b.SourceSessionID)})
	if err != nil {
		return origin, b, route, err
	}
	if id.String() != route.RouteID {
		return deny()
	}
	return origin, b, route, nil
}
func (h *delegationHost) WakeScheduleResults(ctx context.Context) { h.wakeResults(ctx) }
func (h *delegationHost) scanScheduleResults(ctx context.Context) error {
	rows, err := h.store.ListScheduleResultFires(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		var origin scheduleResultOrigin
		if err = json.Unmarshal(row.ResultOrigin, &origin); err != nil {
			failures = append(failures, err)
			continue
		}
		err = h.reconcileResult(ctx, "schedule_fire", row.ID.String(), row.BotID.String(), origin.Identity.UserID, origin.Identity.SourceSessionID)
		if err != nil && !errors.Is(err, errResultNotReady) && !errors.Is(err, botdelegation.ErrDenied) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (h *delegationHost) loadScheduleResult(ctx context.Context, id, bot, user, session string) (resultTask, error) {
	t := resultTask{kind: "schedule_fire", id: id, bot: bot, user: user, session: session, delivery: taskresult.Delivery{}}
	row, err := h.store.ReadScheduleResultFire(ctx, sqlc.ReadScheduleResultFireParams{BotID: nativeTaskUUID(bot), FireID: nativeTaskUUID(id)})
	if err != nil {
		return t, err
	}
	_, b, route, err := h.scheduleOrigin(ctx, bot, row.ResultOrigin)
	if err != nil {
		return t, err
	}
	if b.UserID != user || b.SourceSessionID != session || b.TeamID != row.TeamID.String() {
		return t, botdelegation.ErrDenied
	}
	t.state = "unknown"
	// A run is resolved only by this exact persisted fire, never by latest run.
	if row.SessionID.Valid {
		child, e := h.sessions.Get(ctx, row.SessionID.String())
		if e != nil {
			return t, e
		}
		if child.BotID != bot || child.TeamID != b.TeamID || child.CreatedByUserID != user || child.Type != thread.TypeSchedule || child.ID == session {
			return t, botdelegation.ErrDenied
		}
		run, e := h.store.GetSessionRunByInvocation(ctx, sqlc.GetSessionRunByInvocationParams{SessionID: row.SessionID, InvocationID: "schedule:" + row.ScheduleID.String() + ":" + id})
		switch {
		case e == nil:
			if run.BotID.String() != bot || run.TeamID != row.TeamID {
				return t, botdelegation.ErrDenied
			}
			if !taskresult.Terminal(run.State) {
				return t, errResultNotReady
			}
			t.run = run.RunID.String()
			t.state = run.State
			if t.state == "lost" {
				t.state = "unknown"
			}
		case !errors.Is(e, pgx.ErrNoRows):
			return t, e
		case !row.CompletedAt.Valid:
			return t, errResultNotReady
		}
	} else if !row.CompletedAt.Valid {
		return t, errResultNotReady
	}
	if t.run == "" && row.CompletedAt.Valid && row.Status == "error" {
		t.state = "failed"
		t.text = "Recorded failure code: " + row.ErrorCode
	}
	// The execution receipt is authoritative. A late callback must not change a
	// lost run into failure/success or create a second event for the same answer.
	// Read the same exact run both before and after schedule-log completion.
	if t.state == "completed" {
		t.text, err = h.exactResultText(ctx, bot, row.SessionID.String(), t.run)
		if err != nil {
			return t, err
		}
	}
	t.text = "Scheduled task execution state: " + t.state + ".\n" + t.text
	t.raw = row.ResultDelivery
	t.cmd = delegationDeliveryCommand(botdelegation.Record{Identity: b}, route, t.text, nil)
	t.cmd.DelegationResultID = ""
	t.save = func(ctx context.Context, old, next []byte) error {
		_, err := h.store.SaveScheduleResultDelivery(ctx, sqlc.SaveScheduleResultDeliveryParams{BotID: row.BotID, FireID: row.ID, Origin: row.ResultOrigin, Expected: old, Delivery: next})
		return err
	}
	return t, nil
}
