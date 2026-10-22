package computertask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

// Source exposes the same service to any standard MCP tools client. Authority
// comes from the existing host-owned tool mount, never an MCP session header.
type Source struct{ service *Service }

func NewSource(service *Service) *Source { return &Source{service: service} }

func (*Source) ListTools(context.Context, mcp.ToolSessionContext) ([]mcp.ToolDescriptor, error) {
	text := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	schema := func(properties map[string]any, required ...string) map[string]any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	id := schema(map[string]any{"operation_id": text("Receipt ID returned by computer_task_submit in this conversation.")}, "operation_id")
	return []mcp.ToolDescriptor{
		{Name: "computer_workspaces", Description: "List existing native workspaces on one currently authorized computer. Read-only metadata; no cwd, terminal output, new workspace, task execution or automatic retry. A selected-PC conversation can query only that PC; NAS scope still requires a Bot grant. Native status is not task success. A truncated list has no implied continuation cursor.", InputSchema: schema(map[string]any{"target_id": text("Memoh computer target ID from computer_targets; omit only when unambiguous.")})},
		{Name: "computer_task_recover", Description: "Find this conversation's original operation and observe its stored native references using read-only workspace/pane/agent metadata. Return verified persisted Pi facts separately from current native presence; missing receipts or reused/missing panes remain unknown with a reason. Never resubmits, creates a control pane, types into a terminal, imports logs, rewrites a receipt or infers business success. Authentication unavailable is not proof the device is offline.", InputSchema: id},
		{Name: "computer_targets", Description: "List authorized Agenvo Windows computers in this conversation. A PC conversation sees only that PC; a NAS conversation sees only Bot grants. Reports configured capabilities, not a live GUI or full filesystem runtime.", InputSchema: schema(map[string]any{})},
		{Name: "computer_task_submit", Description: "Submit once to an authorized computer using its registered Agenvo/Herdr mapping. foreground_bash executes Bash; herdr_pi_task starts configured Pi/provider through Herdr. No raw methods, device IDs, GUI or fallback. Omit target only when selection is unambiguous. Unknown is not permission to resubmit: query the returned receipt.", InputSchema: schema(map[string]any{"target_id": text("Memoh computer target ID from computer_targets; never an Agenvo device ID."), "kind": map[string]any{"type": "string", "enum": []string{"foreground_bash", "herdr_pi_task"}}, "task": text("Bash command or Pi task intent, according to kind."), "work_dir": text("Root-relative working directory, default '.'.")}, "kind", "task")},
		{Name: "computer_task_get", Description: "Read this conversation's durable task receipt without repeating a submission.", InputSchema: id},
		{Name: "computer_task_result", Description: "Read this task's evidence without resubmitting. completed/process_stopped proves invocation exit only. New Pi invocations return a small correlated result projected locally from the official JSON protocol; full logs and artifact delivery are separate. pi_result distinguishes succeeded, failed, aborted, incomplete or invalid_protocol, not independent acceptance of the user's objective. Legacy invocations retain their original recovery protocol. Unknown must not be resubmitted.", InputSchema: id},
		{Name: "computer_task_cancel", Description: "Record cancellation intent. No generation-bound stop is available: never kills or closes a workspace. Only a later verified invocation receipt can establish process_stopped; cancellation intent does not prove termination or task success.", InputSchema: id},
		{Name: "computer_task_artifact", Description: "Return already ready references for this task; do not collect logs or arbitrary PC files. Business artifacts must be registered through the NAS file module. A successful Pi invocation does not prove artifact delivery or acceptance of the user's objective. Legacy mode can store a bounded terminal snapshot.", InputSchema: id},
		{Name: "computer_task_logs", Description: "Explicit diagnostic request only: collect this invocation's complete stdout/stderr into verified NAS media references using bounded reads. Logs can contain private tool content; no raw logs or reasoning are returned here. Collection may need another call and does not decide, change or retry the Pi task outcome. Never required for computer_task_result or ordinary artifact delivery.", InputSchema: id},
	}, nil
}

func decodeArguments(args map[string]any, dest any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(dest); err != nil {
		return ErrInvalid
	}
	if err = d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func (s *Source) CallTool(ctx context.Context, session mcp.ToolSessionContext, name string, args map[string]any) (map[string]any, error) {
	ctx, cancel := toolcontext.Bind(ctx, session)
	defer cancel()
	if err := toolcontext.ValidateRuntimeGuard(ctx, session); err != nil {
		return nil, err
	}
	b, err := toolcontext.ResolveComputerBinding(ctx, session)
	if err != nil {
		return nil, err
	}
	if s.service == nil {
		return nil, ErrDenied
	}
	var value any
	switch name {
	case "computer_workspaces":
		var input struct {
			TargetID string `json:"target_id"`
		}
		if err = decodeArguments(args, &input); err == nil {
			value, err = s.service.workspaces(ctx, b, input.TargetID)
		}
	case "computer_task_recover":
		var input struct {
			ID string `json:"operation_id"`
		}
		if err = decodeArguments(args, &input); err == nil {
			if input.ID == "" {
				return nil, ErrInvalid
			}
			value, err = s.service.recover(ctx, b, input.ID)
		}
	case "computer_targets":
		if len(args) != 0 {
			return nil, ErrInvalid
		}
		value, err = s.service.targets(ctx, b)
	case "computer_task_submit":
		var input Submit
		if err = decodeArguments(args, &input); err == nil {
			value, err = s.service.submit(ctx, b, input)
		}
	case "computer_task_get", "computer_task_result", "computer_task_cancel", "computer_task_artifact", "computer_task_logs":
		var input struct {
			ID string `json:"operation_id"`
		}
		if err = decodeArguments(args, &input); err != nil || input.ID == "" {
			return nil, ErrInvalid
		}
		switch name {
		case "computer_task_get":
			value, err = s.service.observe(ctx, b, input.ID, false)
		case "computer_task_result":
			value, err = s.service.observe(ctx, b, input.ID, true)
		case "computer_task_cancel":
			value, err = s.service.cancel(ctx, b, input.ID)
		case "computer_task_artifact":
			value, err = s.service.artifact(ctx, b, input.ID)
		case "computer_task_logs":
			value, err = s.service.diagnostics(ctx, b, input.ID)
		}
	default:
		return nil, mcp.ErrToolNotFound
	}
	if err != nil {
		if name == "computer_task_submit" && errors.Is(err, ErrPreflightUnavailable) {
			// These facts describe this call, not the state of any older task.
			// Do not use this envelope for uncertain claim/dispatch outcomes.
			out := mcp.BuildToolSuccessResult(map[string]any{
				"ok": false, "code": "computer.connection_unavailable", "phase": "preflight",
				"operation_created": false, "business_dispatched": false,
				"retry":   map[string]any{"automatic": false, "requires": "verified_connection_recovery"},
				"message": ErrPreflightUnavailable.Error(),
			})
			out["isError"] = true
			return out, nil
		}
		if receipt, ok := value.(Result); ok && receipt.ID != "" {
			out := mcp.BuildToolSuccessResult(map[string]any{"computer": receipt, "error": err.Error()})
			out["isError"] = true
			return out, nil
		}
		return nil, err
	}
	return mcp.BuildToolSuccessResult(map[string]any{"computer": value}), nil
}
