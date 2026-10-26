package computertask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

// Source exposes the same service to any standard MCP tools client. Authority
// comes from the existing host-owned tool mount, never an MCP session header.
type Source struct {
	service *Service
	logger  *slog.Logger
}

func NewSource(service *Service) *Source { return &Source{service: service} }

// SetLogger injects the existing host logger; no global logger or new sink.
func (s *Source) SetLogger(log *slog.Logger) { s.logger = log }

func (s *Source) logResultReadFailure(ctx context.Context, session mcp.ToolSessionContext, id string, started time.Time, trace *agenvo.BatchReadTrace) {
	s.logger.WarnContext(ctx, "computer task result read failed",
		slog.String("operation_id", id), slog.String("operation_run_id", trace.OperationRunID), slog.String("run_id", session.RunID), slog.String("tool_call_id", session.ToolCallID),
		slog.String("stage", trace.Stage), slog.String("read_stage", trace.ReadStage), slog.String("reason", trace.Reason),
		slog.Int("snapshot_attempts", trace.SnapshotAttempts), slog.Int("snapshot_lines", trace.SnapshotLines), slog.Int64("snapshot_last_elapsed_ms", trace.SnapshotLastElapsedMS),
		slog.Int64("elapsed_ms", time.Since(started).Milliseconds()), slog.Int64("read_elapsed_ms", trace.ReadElapsedMS),
		slog.Int64("feedback_elapsed_ms", trace.FeedbackElapsedMS), slog.Int64("delivery_wake_elapsed_ms", trace.DeliveryWakeElapsedMS),
		slog.Bool("feedback_failed", trace.FeedbackFailed), slog.Bool("delivery_wake_failed", trace.DeliveryWakeFailed),
		slog.Bool("deadline_exceeded", trace.DeadlineExceeded), slog.Bool("canceled", trace.Canceled),
		slog.String("file_kind", trace.FileKind), slog.String("native_method", trace.Method),
		slog.Int64("offset", trace.Offset), slog.Int64("count", trace.Count), slog.Int64("actual_bytes", trace.ActualBytes), slog.Int64("completed_pages", trace.CompletedPages),
		slog.String("save_kind", trace.SaveKind), slog.Int64("expected_revision", trace.ExpectedRevision), slog.String("save_error_class", trace.SaveErrorClass))
}

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
	artifact := schema(map[string]any{"source_ref": text("Explicit closed result filename in the configured producer root; letters, digits, underscore or hyphen. Requires pi-json-v1 and work_dir dot; no logs or arbitrary paths."), "mime": text("Canonical MIME type of this requested result.")}, "source_ref", "mime")
	id := schema(map[string]any{"operation_id": text("Receipt ID returned by computer_task_submit in this conversation.")}, "operation_id")
	return []mcp.ToolDescriptor{
		{Name: "computer_workspaces", Description: "List existing native workspaces on one currently authorized computer. Read-only metadata; no cwd, terminal output, new workspace, task execution or automatic retry. A selected-PC conversation can query only that PC; NAS scope still requires a Bot grant. Native status is not task success. A truncated list has no implied continuation cursor.", InputSchema: schema(map[string]any{"target_id": text("Memoh computer target ID from computer_targets; omit only when unambiguous.")})},
		{Name: "computer_task_recover", Description: "Find this conversation's original operation and observe its stored native references using read-only workspace/pane/agent metadata. History continuity remains unconfirmed: this read-only recovery does not check session files. Report last_invocation checks only as historical evidence, never restored history. Return verified persisted Pi facts separately from current native presence; missing receipts or reused/missing panes remain unknown with a reason. Never resubmits, creates a control pane, types into a terminal, imports logs, rewrites a receipt or infers business success. Authentication unavailable is not proof the device is offline.", InputSchema: id},
		{Name: "computer_targets", Description: "List authorized Windows computer task targets backed by a configured native Memoh Runtime in this conversation. A PC conversation sees only that PC; a NAS conversation sees only Bot grants. Reports configured capabilities, not a live GUI or full filesystem runtime.", InputSchema: schema(map[string]any{})},
		{Name: "computer_task_submit", Description: "Submit once to an authorized computer using its registered native Memoh Runtime/Herdr mapping. foreground_bash executes Bash; herdr_pi_task starts configured Pi/provider through Herdr. No raw methods, device IDs, GUI or fallback. Omit target only when selection is unambiguous. Unknown is not permission to resubmit: query the returned receipt. Only explicit resume_operation_id can continue a native batch stopped before its workspace acknowledgement was saved. Supply the complete unchanged original request; this verifies the existing workspace and preserves the original operation. It never creates another workspace or repeats an uncertain tab/prompt submission.", InputSchema: schema(map[string]any{"resume_operation_id": text("Explicit original operation ID for the bounded native workspace-acknowledgement recovery only. Retain the exact original task, work_dir, target and artifact declarations. Omit for a new operation."), "target_id": text("Memoh computer target ID from computer_targets; never a transport device ID."), "kind": map[string]any{"type": "string", "enum": []string{"foreground_bash", "herdr_pi_task"}}, "task": text("Bash command or Pi task intent, according to kind."), "work_dir": text("Root-relative working directory, default '.'."), "artifact": artifact, "artifacts": map[string]any{"type": "array", "minItems": 1, "maxItems": deliverymeta.MaxArtifacts, "items": artifact, "description": "Optional bounded results; use artifacts OR single artifact, never both. Distinct filenames including Windows case aliases."}}, "kind", "task")},
		{Name: "computer_task_followup", Description: "Send one new instruction to this operation's existing native Herdr Pi Agent; never creates an Agent/workspace, runs a batch or resumes a Pi session. Requires an explicit mode: current_agent permits unknown or changed logical session continuity; bound_session requires the saved official agent_session to match the current observation. Neither mode guarantees atomic identity or strict conversation continuity. Missing/changed bindings never silently downgrade bound_session. Use a new UUID request_id for a genuinely new instruction; repeat the same ID and exact arguments to read its recorded outcome. Unknown blocks further input and must not be resent with another ID. Read followup.state, not the original operation state. Accepted means prompt delivery, not business completion. No target IDs, paths or native refs are accepted.", InputSchema: schema(map[string]any{"operation_id": text("Original native Agent operation ID from this conversation."), "request_id": text("Canonical UUID identifying this one instruction, retained for duplicate calls and lost responses."), "mode": map[string]any{"type": "string", "enum": []string{"current_agent", "bound_session"}}, "task": text("The new instruction for the already associated native Pi Agent; not the original task replay.")}, "operation_id", "request_id", "mode", "task")},
		{Name: "computer_task_get", Description: "Read this conversation's durable task receipt, follow-up history and bounded native observation/export history without contacting the Agent. Historical availability is not rechecked. Optional history_offset reads one stored native observation summary with its export references, omitting terminal text and follow-up details; follow next_history_offset for older/newer saved samples. Offsets are local append-only indexes, not native result IDs or an event cursor.", InputSchema: schema(map[string]any{"operation_id": text("Receipt ID in this conversation."), "history_offset": map[string]any{"type": "integer", "minimum": 0, "description": "Optional zero-based native history index. Start at 0; follow next_history_offset. No Agent call."}, "delivery_after": text("Optional delivery ledger page cursor. Empty string starts the first page; retain next_after for append progress. At most 16 entries per page, not a total-file limit. Committed references only; a page is not whole-task completion. Mutually exclusive with history_offset.")}, "operation_id")},
		{Name: "computer_task_result", Description: "Read this task's evidence without resubmitting. Native Agents append bounded samples; native_evidence.recording_state capacity_reached rejects a new sample while preserving existing evidence. Samples and completion_seq never identify a single input result: verified_result_ownership stays unknown even with accepted follow-ups. completed/process_stopped proves invocation exit only. continuity is separate from pi_result; its last_invocation checks file bytes and supported branch structure, not current history availability or retention of all old messages in model context. Missing, changed, corrupt, unreadable or unconfirmed history must not be described as restored. New Pi invocations return a small correlated result projected locally from the official JSON protocol; full logs and artifact delivery are separate. Only delivery.state delivered with a committed NAS reference proves file delivery; blocked/unknown files can need local action without resubmitting Pi. pi_result distinguishes succeeded, failed, aborted, incomplete or invalid_protocol, not independent acceptance of the user's objective. Legacy invocations retain their original recovery protocol. Unknown must not be resubmitted.", InputSchema: id},
		{Name: "computer_task_cancel", Description: "Record cancellation intent. No generation-bound stop is available: never kills or closes a workspace. Only a later verified invocation receipt can establish process_stopped; cancellation intent does not prove termination or task success.", InputSchema: id},
		{Name: "computer_task_artifact", Description: "Return already ready references for this task; do not collect logs or arbitrary PC files. Declared artifacts use the existing durable queue and NAS file module: only delivered delivery.reference or delivery.files references are committed. Partial means inspect each file; never rerun the task to retry delivery. A successful Pi invocation does not prove artifact delivery or acceptance of the user's objective. Native mode exports a saved bounded terminal sample; optional observation_id selects older evidence, otherwise use the latest saved sample. Repeated exports reuse the historical hash with current_availability not_rechecked. Sample association is not verified per-input ownership.", InputSchema: schema(map[string]any{"operation_id": text("Receipt ID in this conversation."), "observation_id": text("Optional local native_evidence observation_id; not a Herdr execution ID. Native Agent mode only.")}, "operation_id")},
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
			value, err = s.service.Recover(ctx, b, input.ID)
		}
	case "computer_targets":
		if len(args) != 0 {
			return nil, ErrInvalid
		}
		value, err = s.service.targets(ctx, b)
	case "computer_task_followup":
		var input Followup
		if err = decodeArguments(args, &input); err == nil {
			value, err = s.service.followup(ctx, b, input)
		}
	case "computer_task_submit":
		var input Submit
		if err = decodeArguments(args, &input); err == nil {
			value, err = s.service.submit(ctx, b, input)
		}
	case "computer_task_artifact":
		var input struct {
			ID            string `json:"operation_id"`
			ObservationID string `json:"observation_id"`
		}
		if err = decodeArguments(args, &input); err != nil || input.ID == "" {
			return nil, ErrInvalid
		}
		value, err = s.service.artifactForObservation(ctx, b, input.ID, input.ObservationID)
	case "computer_task_get":
		var input struct {
			ID            string  `json:"operation_id"`
			HistoryOffset *int    `json:"history_offset"`
			DeliveryAfter *string `json:"delivery_after"`
		}
		if err = decodeArguments(args, &input); err != nil || input.ID == "" {
			return nil, ErrInvalid
		}
		switch {
		case input.DeliveryAfter != nil:
			if input.HistoryOffset != nil {
				return nil, ErrInvalid
			}
			value, err = s.service.deliveryPage(ctx, b, input.ID, *input.DeliveryAfter)
		case input.HistoryOffset != nil:
			value, err = s.service.nativeHistory(ctx, b, input.ID, *input.HistoryOffset)
		default:
			value, err = s.service.observe(ctx, b, input.ID, false)
		}
	case "computer_task_result", "computer_task_cancel", "computer_task_logs":
		var input struct {
			ID string `json:"operation_id"`
		}
		if err = decodeArguments(args, &input); err != nil || input.ID == "" {
			return nil, ErrInvalid
		}
		switch name {
		case "computer_task_result":
			if s.logger != nil {
				trace := &agenvo.BatchReadTrace{ActualBytes: -1}
				ctx = agenvo.WithBatchReadTrace(ctx, trace)
				started := time.Now()
				defer func() {
					if receipt, ok := value.(Result); err != nil && ok && receipt.ID != "" {
						s.logResultReadFailure(ctx, session, receipt.ID, started, trace)
					}
				}()
			}
			value, err = s.service.observe(ctx, b, input.ID, true)
		case "computer_task_cancel":
			value, err = s.service.cancel(ctx, b, input.ID)
		case "computer_task_logs":
			value, err = s.service.diagnostics(ctx, b, input.ID)
		}
	default:
		return nil, mcp.ErrToolNotFound
	}
	// Follow-up receipts can be reconstructed directly from JSON. Capacity is
	// current server configuration, never a stale persisted quota.
	if receipt, ok := value.(Result); ok && receipt.NativeEvidence != nil {
		receipt.NativeEvidence.Limit = s.service.nativeObservationLimit
		receipt.NativeEvidence.Full = receipt.NativeEvidence.Count >= s.service.nativeObservationLimit
		value = receipt
	}
	if err != nil {
		if (name == "computer_task_submit" || name == "computer_task_followup") && admissionBlocked(err) {
			facts := map[string]any{"ok": false, "code": admissionErrorCode(err), "phase": "admission", "business_dispatched": false, "retry": map[string]any{"automatic": false, "requires": "verified_owner_admission_state"}}
			if name == "computer_task_submit" {
				facts["operation_created"] = false
			} else {
				facts["request_reserved"] = false
			}
			if receipt, ok := value.(Result); ok && receipt.ID != "" {
				facts["computer"] = receipt
			}
			out := mcp.BuildToolSuccessResult(facts)
			out["isError"] = true
			return out, nil
		}
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

// Recover exposes the existing read-only task recovery to authenticated host
// adapters. The binding must be resolved from persisted ownership by the host.
func (s *Source) Recover(ctx context.Context, binding toolcontext.ComputerBinding, id string) (Recovery, error) {
	return s.service.Recover(ctx, binding, id)
}
