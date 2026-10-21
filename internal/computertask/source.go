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
		{Name: "computer_targets", Description: "List authorized Agenvo Windows computers in this conversation. A PC conversation sees only that PC; a NAS conversation sees only Bot grants. Reports configured capabilities, not a live GUI or full filesystem runtime.", InputSchema: schema(map[string]any{})},
		{Name: "computer_task_submit", Description: "Submit once to an authorized computer using its registered Agenvo/Herdr mapping. foreground_bash executes Bash; herdr_pi_task starts configured Pi/provider through Herdr. No raw methods, device IDs, GUI or fallback. Omit target only when selection is unambiguous. Unknown is not permission to resubmit: query the returned receipt.", InputSchema: schema(map[string]any{"target_id": text("Memoh computer target ID from computer_targets; never an Agenvo device ID."), "kind": map[string]any{"type": "string", "enum": []string{"foreground_bash", "herdr_pi_task"}}, "task": text("Bash command or Pi task intent, according to kind."), "work_dir": text("Root-relative working directory, default '.'.")}, "kind", "task")},
		{Name: "computer_task_get", Description: "Read this conversation's durable task receipt without repeating a submission.", InputSchema: id},
		{Name: "computer_task_result", Description: "Read the receipt and, for Pi, observe Herdr read-only. Terminal text/status is unverified evidence, never proof of task success. Unknown must not be resubmitted.", InputSchema: id},
		{Name: "computer_task_cancel", Description: "Record cancellation intent. Herdr has no safe generation-bound stop: process_stopped stays false and unresolved tasks stay unknown. Never kills or closes a workspace.", InputSchema: id},
		{Name: "computer_task_artifact", Description: "Store the bounded terminal snapshot as a ready content-hash text attachment. Does not transfer arbitrary PC files or prove task completion.", InputSchema: id},
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
	case "computer_task_get", "computer_task_result", "computer_task_cancel", "computer_task_artifact":
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
		}
	default:
		return nil, mcp.ErrToolNotFound
	}
	if err != nil {
		if receipt, ok := value.(Result); ok && receipt.ID != "" {
			out := mcp.BuildToolSuccessResult(map[string]any{"computer": receipt, "error": err.Error()})
			out["isError"] = true
			return out, nil
		}
		return nil, err
	}
	return mcp.BuildToolSuccessResult(map[string]any{"computer": value}), nil
}
