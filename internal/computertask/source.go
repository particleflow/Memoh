package computertask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/felinics/memoh/internal/computertarget"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

// Source exposes the same service to any standard MCP tools client. Authority
// comes from the existing host-owned tool mount, never an MCP session header.
type Source struct {
	service       *Service
	logger        *slog.Logger
	associate     func(context.Context, dbstore.ComputerOperationRecord, []DeliveryRequest) error
	resumeContext func(context.Context, toolcontext.ComputerBinding) (context.Context, error)
}

func NewSource(service *Service) *Source { return &Source{service: service} }

// SetLogger injects the existing host logger; no global logger or new sink.
func (s *Source) SetLogger(log *slog.Logger) { s.logger = log }

func (s *Source) logResultReadFailure(ctx context.Context, session mcp.ToolSessionContext, id string, started time.Time, trace *computertarget.BatchReadTrace) {
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
	artifact := schema(map[string]any{"source_ref": text("生产根目录下的结果文件名：字母/数字/下划线/连字符，不含路径，Windows 大小写别名视为重名。仅代理任务且 work_dir 为默认 '.'；不接受日志或任意路径。"), "mime": text("该请求结果的规范 MIME 类型。")}, "source_ref", "mime")
	id := schema(map[string]any{"operation_id": text("Receipt ID returned by computer_task_submit in this conversation.")}, "operation_id")
	return []mcp.ToolDescriptor{
		{Name: "computer_workspaces", Description: "List existing native workspaces on one currently authorized computer. Read-only metadata; no cwd, terminal output, new workspace, task execution or automatic retry. A selected-PC conversation can query only that PC; NAS scope still requires a Bot grant. Native status is not task success. A truncated list has no implied continuation cursor.", InputSchema: schema(map[string]any{"target_id": text("Memoh computer target ID from computer_targets; omit only when unambiguous.")})},
		{Name: "computer_task_recover", Description: "Find this conversation's original operation and observe its stored native references using read-only workspace/pane/agent metadata. History continuity remains unconfirmed: this read-only recovery does not check session files. Report last_invocation checks only as historical evidence, never restored history. Return verified persisted Pi facts separately from current native presence; missing receipts or reused/missing panes remain unknown with a reason. Never resubmits, creates a control pane, types into a terminal, imports logs, rewrites a receipt or infers business success. Authentication unavailable is not proof the device is offline.", InputSchema: id},
		{Name: "computer_targets", Description: "列出本会话授权的 Windows 任务目标及其配置能力。capabilities 中的 foreground_bash / herdr_pi_task 对应 computer_task_submit 的可选 kind（前台命令 / 代理任务）；agent_protocol 字段标示代理任务的协议形态。availability=not_probed 表示未做实时探测，不表示目标离线；目标可用性以任务回执为准。带代理任务能力（herdr_pi_task）的目标附带 available_models：本次列举时读取的可用模型列表。", InputSchema: schema(map[string]any{})},
		{Name: "computer_task_submit", Description: "向授权电脑提交一次任务。选择 kind：一条 shell 能完成、只需短文本结果，用 foreground_bash（前台命令，回执为有界 UTF-8 文本，超 4096 字节截断；文件内容经此路会截断，取文件走工件通路）；多步执行、需要产出文件、需要代理判断，用 herdr_pi_task（委派代理执行，结构化回执）。前台命令以目标电脑的系统账号运行，引用用户目录用绝对路径。任务要产出文件时，提交时以 artifact 或 artifacts（二选一）声明 source_ref（生产根目录下的结果文件名：字母/数字/下划线/连字符，不含路径，Windows 大小写别名视为重名）与 mime，且 work_dir 保持默认 '.'；文件经交付队列回传 NAS，以 computer_task_result 的 delivery.state=delivered 为准，这是电脑文件进入会话的唯一通路。每次代理任务（herdr_pi_task）一并提交 model：按任务需求从本次派发返回的可用模型列表中选一个一并提交；未选或所选不在当次列表时提交被退回并附当前列表，重挑再提交，服务端不代选。target 仅在多目标歧义时省略。逐字相同的请求会命中既有 operation 并返回其原回执，不产生新执行；需要新执行时变更请求内容（如换 artifact 文件名）。未知结果不是重发许可：先读回执。resume_operation_id 仅用于中断的输入准备或工作区确认恢复，须逐字保留原请求；对已完成 operation 调用 resume 返回 resumable=false 与 resume_reason（completed_pi_not_started 表示任务从未启动，其 pi_result.preflight 给出失败预检码 preflight_cwd_failed / preflight_pi_missing / preflight_node_missing / preflight_lock_failed，均为目标电脑侧环境原因）；修复原因后提交新 operation，不要把 resume 当重试。", InputSchema: schema(map[string]any{"resume_operation_id": text("显式的原 operation ID，仅用于中断的输入准备恢复或有限的工作区确认恢复。逐字保留原 task、work_dir、target、inputs 与工件声明。新任务省略此字段。已完成 operation 不会重新执行，其结果以 resumable=false 与 resume_reason 报告。"), "target_id": text("computer_targets 返回的 Memoh 目标 ID；不是传输层设备 ID。"), "kind": map[string]any{"type": "string", "enum": []string{"foreground_bash", "herdr_pi_task"}}, "task": text("前台 Bash 命令或代理任务意图，按 kind 而定。"), "model": text("本次代理任务（herdr_pi_task）使用的模型 id，逐字取自本次派发返回的可用模型列表，按任务需求选择；未选或不在列表会被退回并附当前列表。前台命令（foreground_bash）省略。"), "work_dir": text("根相对工作目录，默认 '.'。同一 work_dir 的代理任务共享同一代理会话（连续性按此判定）；换 work_dir 即新会话、无先前历史。声明工件的任务必须用默认 '.'。"), "inputs": map[string]any{"type": "array", "minItems": 1, "maxItems": deliverymeta.MaxArtifacts, "uniqueItems": true, "items": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}, "description": "本 Bot 已有的 NAS 原件，以 content_hash 标识；不接受本地/服务端路径或 URL。仅代理任务支持。制备到原任务根下后再执行；任务提示提供实际电脑侧文件引用。"}, "artifact": artifact, "artifacts": map[string]any{"type": "array", "minItems": 1, "maxItems": deliverymeta.MaxArtifacts, "items": artifact, "description": "可选的有界结果；artifacts 与单个 artifact 二选一。文件名互异，Windows 大小写别名视为重名。"}}, "kind", "task")},
		{Name: "computer_task_followup", Description: "Read a historical input receipt using its original operation_id, request_id, mode and task. Retired TUI input is never newly reserved, sent or retried. Repeat the original ID and exact arguments after a lost response; a new ID is unsupported. Read followup.state, not the original operation state. Accepted means prompt delivery, not business completion. Unknown never authorizes replay. No target IDs, paths or native refs are accepted.", InputSchema: schema(map[string]any{"operation_id": text("Original historical Agent operation ID from this conversation."), "request_id": text("Canonical UUID identifying this one instruction, retained for duplicate calls and lost responses."), "mode": map[string]any{"type": "string", "enum": []string{"current_agent", "bound_session"}}, "task": text("Exact original historical input, used only to verify the saved request fingerprint.")}, "operation_id", "request_id", "mode", "task")},
		{Name: "computer_task_get", Description: "Read this conversation's durable task receipt, follow-up history and bounded native observation/export history without contacting the Agent. Historical native availability is not rechecked. Successful declared files with committed delivery references are verified and linked to the original conversation automatically. Optional history_offset reads one stored native observation summary with its export references, omitting terminal text and follow-up details; follow next_history_offset for older/newer saved samples. Offsets are local append-only indexes, not native result IDs or an event cursor.", InputSchema: schema(map[string]any{"operation_id": text("Receipt ID in this conversation."), "history_offset": map[string]any{"type": "integer", "minimum": 0, "description": "Optional zero-based native history index. Start at 0; follow next_history_offset. No Agent call."}, "delivery_after": text("Optional delivery ledger page cursor. Empty string starts the first page; retain next_after for append progress. At most 16 entries per page, not a total-file limit. Committed references only; a page is not whole-task completion. Mutually exclusive with history_offset.")}, "operation_id")},
		{Name: "computer_task_result", Description: "读取任务回执，不重发。分三层判读：state 是 operation 生命周期（submitted / completed / not_started / unknown / rejected 等）；pi_result.outcome 是代理任务本次执行结果（succeeded / failed / aborted / incomplete / invalid_protocol / not_started），process_stopped=true 只证明代理进程退出；delivery 是声明工件的送达账（state=delivered 且带 committed NAS 引用才算送达，送达后自动关联本会话；blocked/unknown 按回执处置，不重跑任务）。outcome=not_started 时 pi_result.preflight 给出预检失败码（preflight_cwd_failed / preflight_pi_missing / preflight_node_missing / preflight_lock_failed）：目标电脑侧环境原因，修复后提交新 operation（请求内容需有实际差异），resume 无效；preflight 为空的 not_started 属四码外分支（OS 守卫/基础工具/目录建立），同样按目标电脑侧环境上报。stdout/stderr 以 hash 与 stdout_empty / stderr_empty 呈现，完整内容显式调 computer_task_logs。outcome 是本次执行结果，不是对用户目标本身的独立验收。unknown 状态先读回执核实，不重发。", InputSchema: id},
		{Name: "computer_task_cancel", Description: "Record cancellation intent. No generation-bound stop is available: never kills or closes a workspace. Only a later verified invocation receipt can establish process_stopped; cancellation intent does not prove termination or task success.", InputSchema: id},
		{Name: "computer_task_artifact", Description: "Return already ready references for this task; do not collect logs or arbitrary PC files. Declared artifacts use the existing durable queue and NAS file module: only delivered delivery.reference or delivery.files references are committed. Partial means inspect each file; never rerun the task to retry delivery. A successful Pi invocation does not prove artifact delivery or acceptance of the user's objective. Native mode exports a saved bounded terminal sample; optional observation_id selects older evidence, otherwise use the latest saved sample. Repeated exports reuse the historical hash with current_availability not_rechecked. Sample association is not verified per-input ownership.", InputSchema: schema(map[string]any{"operation_id": text("Receipt ID in this conversation."), "observation_id": text("Optional local native_evidence observation_id; not a Herdr execution ID. Native Agent mode only.")}, "operation_id")},
		{Name: "computer_task_logs", Description: "Explicit diagnostic request only: collect this invocation's complete stdout/stderr into verified NAS media references using bounded reads. Logs can contain private tool content; no raw logs or reasoning are returned here. Collection may need another call and does not decide, change or retry the Pi task outcome. Never required for computer_task_result or ordinary artifact delivery.", InputSchema: id},
		{Name: "computer_task_batch_submit", Description: "把一批代理任务按数组顺序持久入队，服务端按提交顺序逐个派发，只提交一次，不需要逐项轮询回执。每项的字段与 computer_task_submit 相同（kind 固定 herdr_pi_task，必须带 model），target 全批共用一个。队列对同一会话同一 target 严格先进先出：同一时刻只有一项在途，前一项未到终态时后一项不会派发；不设批量条数上限。单项失败只记该项，不中断后续项。结果未知的项按既有规则继续持槽，批次呈 blocked 并给出 blocked_on，不自动重发。提交时先对本批全部 items 校验一次可用模型列表，未命中整批退回并附当前列表；派发时仍按当次列表逐项复核。整批进入终态后自动向本会话汇报一次聚合结果；单项细节用 computer_task_get 按 operation_id 查阅。同一会话同一 run 重复提交逐字相同的批量请求返回原批次，不产生新任务。取消未派发项用 computer_task_batch_cancel。", InputSchema: schema(map[string]any{"target_id": text("computer_targets 返回的 Memoh 目标 ID；本批所有项共用，必填。"), "label": text("可选的批次标签，仅用于辨识。"), "items": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"herdr_pi_task"}}, "task": text("代理任务意图，语义同 computer_task_submit 的 task。"), "model": text("本项使用的模型 id，逐字取自可用模型列表。"), "inputs": map[string]any{"type": "array", "maxItems": deliverymeta.MaxArtifacts, "uniqueItems": true, "items": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}, "description": "本 Bot 已有 NAS 原件的 content_hash 清单，语义同 computer_task_submit。"}, "artifact": artifact, "artifacts": map[string]any{"type": "array", "minItems": 1, "maxItems": deliverymeta.MaxArtifacts, "items": artifact, "description": "可选的有界结果声明，语义同 computer_task_submit；声明工件的项 work_dir 保持默认 '.'。"}, "work_dir": text("根相对工作目录，默认 '.'，语义同 computer_task_submit。")}, "required": []string{"kind", "task", "model"}, "additionalProperties": false}}}, "target_id", "items")},
		{Name: "computer_task_batch_get", Description: "读取批量通道状态，只读，不派发不重发。按 batch_id 读一个批次；省略 batch_id 则列出本会话最近的批次。返回完整计数（total/succeeded/failed/canceled/queued/in_flight）、当前推进头与未成功项清单（有界，其余用 more_items 计数）；成功项的执行细节用 computer_task_get 按 operation_id 逐个查阅。state=blocked 表示在途项仍未决并持有该 target 的槽，blocked_on 给出其 operation_id：先按 computer_task_result 读回执核实，不重发。", InputSchema: schema(map[string]any{"batch_id": text("computer_task_batch_submit 返回的 batch_id；省略则列出本会话最近批次。")})},
		{Name: "computer_task_batch_cancel", Description: "取消一个批次中所有尚未派发的项。已派发的在途项不受影响，其取消按 computer_task_cancel 既有语义单独处理。全部项进入终态后批次照常聚合汇报一次。", InputSchema: schema(map[string]any{"batch_id": text("computer_task_batch_submit 返回的 batch_id。")}, "batch_id")},
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
		if s.logger != nil {
			bound, present := toolcontext.HostComputerBinding(ctx)
			s.logger.WarnContext(ctx, "computer tool host binding unavailable",
				slog.String("tool", name), slog.String("run_id", session.RunID), slog.String("tool_call_id", session.ToolCallID),
				slog.Bool("binding_present", present), slog.Bool("public_request", session.PublicRequest),
				slog.Bool("bot_matches", present && bound.BotID == session.BotID),
				slog.Bool("session_matches", present && bound.SessionID == session.SessionID),
				slog.Bool("user_matches", present && (session.UserID == "" || bound.UserID == session.UserID)))
		}
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
		value, err = s.service.listedTargets(ctx, b)
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
	case "computer_task_batch_submit":
		var input BatchRequest
		if err = decodeArguments(args, &input); err == nil {
			value, err = s.service.BatchSubmit(ctx, b, input)
		}
	case "computer_task_batch_get":
		var input struct {
			ID string `json:"batch_id"`
		}
		if err = decodeArguments(args, &input); err == nil {
			value, err = s.service.BatchStatus(ctx, b, input.ID)
		}
	case "computer_task_batch_cancel":
		var input struct {
			ID string `json:"batch_id"`
		}
		if err = decodeArguments(args, &input); err == nil {
			value, err = s.service.BatchCancel(ctx, b, input.ID)
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
				trace := &computertarget.BatchReadTrace{ActualBytes: -1}
				ctx = computertarget.WithBatchReadTrace(ctx, trace)
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
	// Reading saved details must not create attachment links or require a live
	// registration. Result collection and artifact export retain those checks.
	if err == nil && (name == "computer_task_result" || name == "computer_task_artifact") {
		switch receipt := value.(type) {
		case Result:
			if receipt.ID != "" {
				err = s.associateResult(ctx, b, receipt.ID)
			}
		case DeliveryPage:
			if receipt.OperationID != "" {
				err = s.associateResult(ctx, b, receipt.OperationID)
			}
		}
	}
	// Follow-up receipts can be reconstructed directly from JSON. Capacity is
	// current server configuration, never a stale persisted quota.
	if receipt, ok := value.(Result); ok && receipt.NativeEvidence != nil {
		receipt.NativeEvidence.Limit = s.service.nativeObservationLimit
		receipt.NativeEvidence.Full = receipt.NativeEvidence.Count >= s.service.nativeObservationLimit
		value = receipt
	}
	if err != nil {
		batchSubmit := name == "computer_task_batch_submit"
		if errors.Is(err, ErrBatchUnavailable) {
			// The batch lane is simply not wired on this deployment. Every
			// single-task path stays available, so this states the fact instead
			// of failing the whole tool.
			out := mcp.BuildToolSuccessResult(map[string]any{
				"ok": false, "code": "computer.batches_unavailable", "phase": "configuration",
				"business_dispatched": false,
				"retry":               map[string]any{"automatic": false, "requires": "batch queue configured on this server"},
				"message":             ErrBatchUnavailable.Error(),
			})
			out["isError"] = true
			return out, nil
		}
		if (name == "computer_task_submit" || name == "computer_task_followup" || batchSubmit) && admissionBlocked(err) {
			facts := map[string]any{"ok": false, "code": admissionErrorCode(err), "phase": "admission", "business_dispatched": false, "retry": map[string]any{"automatic": false, "requires": "verified_owner_admission_state"}}
			switch {
			case name == "computer_task_submit":
				facts["operation_created"] = false
			case batchSubmit:
				facts["batch_created"] = false
			default:
				facts["request_reserved"] = false
			}
			if receipt, ok := value.(Result); ok && receipt.ID != "" {
				facts["computer"] = receipt
			}
			out := mcp.BuildToolSuccessResult(facts)
			out["isError"] = true
			return out, nil
		}
		if name == "computer_task_submit" || batchSubmit {
			var selection *ModelSelectionError
			if errors.As(err, &selection) {
				// The live catalog is the correction surface: the caller picks
				// from it. No model is chosen, defaulted or cached here.
				facts := map[string]any{
					"ok": false, "code": "computer.model_selection_required", "phase": "validation",
					"business_dispatched": false, "available_models": selection.Models,
					"message": selection.Error(),
					"retry":   map[string]any{"automatic": false, "requires": "model chosen from available_models in this response"},
				}
				if batchSubmit {
					facts["batch_created"] = false
				} else {
					facts["operation_created"] = false
				}
				out := mcp.BuildToolSuccessResult(facts)
				out["isError"] = true
				return out, nil
			}
		}
		if (name == "computer_task_submit" || batchSubmit) && errors.Is(err, ErrPreflightUnavailable) {
			// These facts describe this call, not the state of any older task.
			// Do not use this envelope for uncertain claim/dispatch outcomes.
			facts := map[string]any{
				"ok": false, "code": "computer.connection_unavailable", "phase": "preflight",
				"business_dispatched": false,
				"retry":               map[string]any{"automatic": false, "requires": "verified_connection_recovery"},
				"message":             ErrPreflightUnavailable.Error(),
			}
			if batchSubmit {
				facts["batch_created"] = false
			} else {
				facts["operation_created"] = false
			}
			out := mcp.BuildToolSuccessResult(facts)
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
	if name == "computer_task_batch_get" {
		// The batch list is one body, not a single {@code computer} object: an
		// omitted batch_id returns ranked rows and a batch_id returns one row.
		if summaries, ok := value.([]BatchSummary); ok {
			return mcp.BuildToolSuccessResult(map[string]any{"batches": summaries}), nil
		}
	}
	return mcp.BuildToolSuccessResult(map[string]any{"computer": value}), nil
}

// Observe performs the same authorized refresh read as computer_task_result
// for a trusted host caller that rebuilt the operation's owning conversation.
// It is the pull the result scan uses when an endpoint delivery commit is the
// only completion signal; it never creates or resubmits an operation.
func (s *Source) Observe(ctx context.Context, b toolcontext.ComputerBinding, id string) (Result, error) {
	if s.service == nil {
		return Result{}, ErrDenied
	}
	return s.service.observe(ctx, b, id, true)
}

// Recover exposes the existing read-only task recovery to authenticated host
// adapters. The binding must be resolved from persisted ownership by the host.
func (s *Source) Recover(ctx context.Context, binding toolcontext.ComputerBinding, id string) (Recovery, error) {
	return s.service.Recover(ctx, binding, id)
}

// ScanBatches advances the durable batch queue under the scan's own authority
// rebuild. It never creates a conversation, and every dispatch still goes
// through the unchanged single-task submit path.
func (s *Source) ScanBatches(ctx context.Context) error {
	if s.service == nil {
		return ErrDenied
	}
	return s.service.ScanBatches(ctx)
}
