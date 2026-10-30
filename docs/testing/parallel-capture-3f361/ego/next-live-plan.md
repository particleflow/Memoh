# One bounded text delegation: release9270 source review and writer handoff

**890 preflight hold:** No A/B was sent. Page-owned evaluate pacing timed out before readiness; do not execute this older plan or repeat its self-check. See [the scoped diagnosis and one read-only consumer excerpt request](navigation-wait-diagnosis.md).4120 success remains historical evidence; a documented host wait/lifecycle binding is still needed.

This plan supersedes the README's already-completed free self-check. It is preparation only: the cloud owner sent no live requests, ran no old fixtures/self-checks, and changed no product code. The parent reports the4120 writer result:20 serialized reads/20 waits, minimum253ms,10 scoped GET responses completed, stopped=true/fault=false/unknown0/pending0, naturally observed WS sent/received one each. No A/B, model, DB or configuration work; resources cleared. Evidence files are writer-local `ego412-paced-selfcheck/RESULT.json` and `OUTCOME.json`, not independently read here. Natural WS frames do not prove a business message/queue-item mapping. POST, A/B, base64 and cross-round lifetime remain unverified.

## Is parallel execution possible on this version?

Yes for the primary conversation versus a delegated expert; no for two simultaneous native turns inside the same primary session. Nine relevant files were read at exact release `9270da0252128a296d9a641235cdfd32ab44cfda` and byte-compared with owner4120; all match. `parallel-source-review.json` contains their hashes. Source evidence:

| Source at9270 | Consequence |
| --- | --- |
| `internal/agent/runtime/session/admit.go:187–225` | A session admits one active turn. A different B invocation while A is active is busy. |
| `internal/agent/application/queue_continuation.go:166–177,320–339` | Follow-up B starts after a primary terminal boundary, using its durable `follow-up:{item_id}` identity. |
| `internal/agent/tool/bot_delegation.go:71–81` | delegate_to_bot calls Submit and returns its durable view; it does not wait for final expert output. |
| `internal/botdelegation/service.go:164–182,195–216` | A successful durable claim starts an independent job and immediately projects the tool result. The background job creates a destination session and runs there. |
| `cmd/internal/core/bot_delegation.go:274–276,308–315` | The expert has a new internal target session; StartTurn uses target Bot/session, not the fixed primary C. |
| `internal/botdelegation/service.go:358–374` | The scoped delegation GET returns its exact target run and persisted run state. |

Thus the useful overlap is **primary A concludes, primary B answers, expert is still running**. B may be sent while A is active and queue until A finishes, or may be sent after A finishes. Do not require B to overlap A itself or loosen the primary session's fencing lock. No deterministic expert-to-primary serialization defect is established by this source review, so no product concurrency patch is justified here.

The earlier03:51 sample had expert completion before A's terminal boundary, then B admission13ms after A terminated. It cannot prove that the independent expert held the primary lock. The later dedda02f sample lacks A/B queue identities and remains inconclusive. Preserve both histories. Prior barrier tests establish isolated capability only, not live acceptance; they were not rerun.

## Capture must be armed before either send

The existing4120 collector is adequate to start capture before A; it has no lazy Network subscription after observing a business result. `createEgoCapture` awaits Network.enable and an initial drain. The writer then obtains fresh normal GET/socket observations, arms A, and only then clicks Send. The single destructive reader must remain the sole reader for the entire sequence.

`capture.js` correlates the scope's requestWillBeSent by original requestId; POST body is taken from postData or one awaited getRequestPostData. It saves invocation_id before binding the role. responseReceived records status only; loadingFinished triggers awaited getResponseBody. Only the same request's202 and string item_id establish `follow-up:{item_id}`. Interleaved responses cannot substitute another request. The frontend API wrapper at `apps/web/src/composables/api/useChat.chat-api.ts:105–112` sends `{invocation_id,text}` to that queue route; the handler response fields are declared in `internal/handlers/session_queue.go:73–79`. All of these handlers exist before A. Unknown bodies/redirects/shapes are retained without resend. This is source plus prior fixture evidence, not yet a real POST proof.

Normal idle A/B can use WS and legitimately produce no queue POST. Do not force queueing, duplicate a send or manufacture busy work merely to obtain POST coverage. If B queues naturally, the original request and202 mapping must be present; otherwise the sample is inconclusive. If B uses WS, its exact WS invocation must be present and POST coverage remains explicitly unexercised.

Use normal Ego input/click actions, returning after the action, **not an application-store send() Promise that awaits assistant completion** (`apps/web/src/store/chat/send.ts:351–377`). Waiting for that Promise would prevent observation during A. Keep action/drain/sample sequencing inside one supported Ego execution; the free self-check does not prove cross-tool-round object survival. If the writer cannot retain that lifecycle, stop before A and report the exact missing execution capability. Do not invent persistence, external browser access or concurrent SDK behavior.

## Parent-reviewable single-sample scope

Use the currently authorized existing specialist selected through the normal Bot capability, the original fixed C and unchanged model settings. No guessed Bot IDs, new specialist, PC dependency, multimedia, web research, file creation, code execution, schedule change or permission change is needed. Select an existing specialist capable of ordinary text review; if none is actually available, stop before A rather than inventing capability.

Suggested A text (replace only the specialist's verified display name):

> 请把下面这项文本审查交给现有的【专家】后台完成。你收到委派接纳后简短告诉我，结束当前回复，不轮询等待专家；结果完成后自然转告我。请专家基于这些既定要求，交付一张“要求／最容易遗漏的失败场景／可观察验收证据”表，并指出最需要优先验证的两项：固定Bot持续主聊；长任务在独立执行上下文；进行中能继续聊天；真实任务目录保留历史失败和unknown；结果回原主聊且不重复；权限撤销后不能沿用旧授权。只使用本条材料进行文字分析，无需调用其他服务。

This produces a useful bounded text deliverable, not artificial token padding or a forced wait. No task length or timing guarantees a parallel window. Asking the primary to acknowledge the asynchronous handoff is normal product use, not a hidden branch or modified scheduling rule. Unexpected tool actions must be recorded; do not silently change settings or clear their history.

Suggested B text, sent once when the original expert is observed running:

> 请用一句话解释：任务已排队，为什么还不能当作已经执行完成？

This needs a substantive short response rather than a receipt/progress echo. Count one A, one B, one intended expert execution, and the normal result-summary continuation; internal retries/extra tool calls, if they happen, must be recorded. These are four logical pieces of model work (A, expert, B, natural summary), not a promise of exactly four billed requests: tool/model continuation and existing retries may add requests. No fixed currency cost can be inferred from the source. No new fee approval is required by this plan, but the parent schedules the unique writer under the existing authorization. Do not automatically enlarge the task or repeat the sample if it is too fast.

## Execution and decision gates

Proposed observer configuration for parent review: `pollIntervalMs=250`, `timeoutMs=10000`, `stopDrainMs=2000`, and a total live observation budget of120000ms starting immediately before A. These are explicit adjustable local observation settings, not product defaults, a worker duration or an acceptance threshold. On expiry only stop/drain the recorder; no task cancellation or new sample. Existing in-flight Ego calls can extend the observer shutdown by their own timeout; cross-call cancellation is not claimed.

1. Read existing selected-page identity and current source version using the supported UI/API. Keep actual root/credentials local. Paste4120 capture.js. Configure the observed four paths, finite observer/drain budgets and the already field-verified250ms minimum interval. This is observer pacing, not a task timeout or rate guarantee. Obtain fresh scoped GET and socket from one normal refresh, then drain. Do not rerun the separate free self-check or shape probe. Stop before A if fault, wrong C, missing GET/socket, non-idle original C, or inability to keep the object alive in the supported execution.
2. Arm A before the normal one-time Send action. Immediately drain/sample; retain original invocation, exact A run/turn, tool call/result and delegation ID. Do not infer identity from text, newest run, or all historic tasks. A's exact tool output/turn must establish the target association.
3. At the first scoped GET proving that same target run is running, arm and send B once through the normal composer. B may naturally queue behind A. Capture its original WS identity or POST request invocation plus complete matching202/item_id. Record A's exact state as well, so queued B can be distinguished from an independent expert lock. If the expert is already terminal before B can be sent, do not send B just to repeat an already-missed window: record inconclusive and observe the existing result only.
4. Await paced sample calls within the writer's finite observation budget. Public contracts remain GET `/bots/{source}/sessions/{C}/invocations/{invocation}`, GET `/bots/{source}/messages?session_id={C}&limit=100`, and GET `/bots/{source}/sessions/{C}/delegations/{original_id}` with existing auth. No private SQL. A queue invocation is `follow-up:{original item_id}`; its request UUID is retained separately. Any lost mapping is unknown, never guessed from latest run.
5. Pass overlap only when the ordered records show: exact B invocation completed → exact B turn persisted with substantive final assistant answer → GET of the unchanged original delegation reports the same target Bot/session/run and `target_run_state=running`. `overlap_candidate` still needs final-text review.202, progress text, B scheduled/running or a timestamp comparison alone cannot pass. The worker need not remain running for any fixed duration.
6. Observe that same original expert's terminal result and a natural summary in C. Retain exact final expert/primary turn identities and one normal refresh observation. Public UI/API evidence supports visible persistence/nonduplication for this sample, not an internal receipt assertion. If the summary is not complete within the observation budget, preserve pending/unknown and let the real task continue; do not cancel it or recover/replay it automatically.
7. Stop and drain the recorder, keep sanitized sensitive records locally, return a reviewed identity/sequence table. Budget expiry, early expert completion, missing mapping or interrupted observation is inconclusive. An explicit runtime error, wrong identity or repeated persisted summary is a distinct failure. Neither result authorizes an automatic second paid sample. If the expert remains active but B fails to admit after A's terminal boundary, return the exact A/B/queue/worker evidence for a focused product diagnosis.

Return: source version, unchanged C, A invocation/run/turn; original delegation and target identities; B transport/request invocation/response status/item_id/admitted invocation/run/turn; evidence sequence for B complete/history/subsequent worker; final-text review; result/refresh evidence; unknowns/stop status; `pass|inconclusive|failed` and reason. No token, Authorization, private address or raw conversation dump in the handoff.
