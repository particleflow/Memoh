# Contact reply streaming and visible progress regression

Base: NAS Server/Web `5a42d013e801cb6229ee029113e3a63782bc3e13`.
HAP 1.0.28 source inspected at `12faf4c06db01d8fffc29aad3a247b4a85e884e7`.
The cancellation candidates `ac7eba92fee9c2824f058b1394a6c55362b787e8`
and `013e91f7dd244061a941b7bb63994fb5270e8356` are source candidates,
not deployed versions. This change modifies shared Web rendering only.

## Finding and minimal correction

`673bb95dd10d2125cb53e2fc08a18870974b3660` passed
`hide-process-activity` to native Bot contact panes. `message-item.vue`
then discarded all `reasoning` blocks and returned an empty runtime status.
That hid provider-returned reasoning and existing compacting/retry status,
even while the server and client retained those events.

Remove only those two filters. The existing thinking disclosure and localized
runtime status render again. Raw tools retain the previous contact filter;
completed Q&A, errors, decisions, task/history records, and ordinary text keep
their original behavior. No new reasoning is requested or synthesized. No Agent
runtime, provider, permissions, queue, idempotency, persistence, or service code
is modified.

The separate claim that all reply text waits until completion was **not
reproduced** on this source using incremental runtime frames: the old source
already displays the first text delta before later text and terminal frames.
Do not describe this patch as fixing confirmed server/provider buffering.

## Transport and result paths

- Chat content uses `/bots/{bot_id}/web/ws` (the compatibility local route also
  exists). SSE `/sessions/events` carries identifiers/invalidation, not reply
  bodies. SSE frames flush individually and use `X-Accel-Buffering: no`.
- Application `runHandle.pump` publishes each chunk through the existing runtime
  manager. `runtimeDeltaForAgentEvent` emits `message_appends` for both text and
  reasoning; the WS subscription forwards each event. `createRuntimeClient`
  batches projection notifications per animation frame, then the transcript
  merge and message renderer consume the deltas. Hidden/background browser
  frames may be suspended; no new batching policy is introduced here.
- The native loop calls `Provider.DoStream` once per step and forwards each
  SDK `TextDeltaPart` / `ReasoningDeltaPart` as its corresponding public event.
  Its terminal persistence barrier does not gate those nonterminal deltas.
  This source inspection does not establish how the configured live provider
  actually chunks its response.
- Durable task delivery first waits for the original task's terminal saved
  outcome. It then admits a separate, result-only natural summary turn. That
  summary uses the same streaming runtime as a direct reply. Waiting for the
  original task is different from buffering that summary's generated text.
  `TaskResultReceipt` hides the machine **input** in queue/live/history; final
  publication receipts require completed nontruncated output. Neither condition
  requires withholding live assistant deltas.
- HAP source `SharedChat.ets` permits API/socket resources through the WebView;
  `NativeClient.ets` provides configuration/media/voice commands and has no chat
  text accumulator. `shared-home-admission-final.json` contains the same two
  shared filters, so the final unified source must carry this correction into
  that overlay. No native transport defect was established. This patch does not
  build/sign/install a HAP or alter its version/provenance.

## Executed evidence

Isolated PostgreSQL database `memoh_streaming`; Server compiled from the exact
base with `go build -buildvcs=false`; current Vite at
`http://127.0.0.1:28082/bot/stream-fixture`; actual Chromium. No NAS/browser
control, production deployment, paid provider call, or historical-row deletion.
The test Bot/account/contact belong only to this isolated database.

The [capture harness](nas-streaming-visible-progress/capture-harness.go.txt)
invokes the actual base session manager and UI converter with separate public
status, reasoning, text, and terminal events. It exports direct and host-result
frames; the host-result command has `TaskResultReceipt` and no request user
bubble. The resulting [wire fixtures](../../apps/web/src/pages/home/components/fixtures/contact-runtime-stream.json)
are consumed by the production runtime client, transcript controller and message
component in tests. The fixture origin is synthetic, not a captured NAS/provider
run. Its terminal event is withheld until after both text fragments are visible.

The [browser replay](nas-streaming-visible-progress/replay-ui.cjs) opens the full
running application with real login/contact APIs and substitutes only its chat
WS with those manager-emitted frames. Each fragment is released separately,
with a one-second hold before the next fragment and another before terminal.
The real markdown renderer is used. This is not typewriter playback of an
already-completed reply. The existing renderer's incremental reveal remains.

The [timing data](nas-streaming-visible-progress/ui-timing.json) measures from
immediately **before fixture frame release** to the first nonempty assistant
prose DOM mutation. It includes loopback transport, animation-frame processing
and renderer delay; it is an upper bound on receive-to-DOM latency, not provider
TTFT, paint timing, or NAS/device performance. Single-run observations:

| Current page | Direct | Result summary | Reasoning/status |
| --- | ---: | ---: | --- |
| Baseline contact | 129.2 ms | 132.1 ms | Hidden |
| Corrected contact | 125.6 ms | 126.1 ms | Visible |
| Corrected 390 px, Chinese/dark | 131.0 ms | 136.8 ms | Visible |

Screenshots inspected from these actual runs:
[baseline hidden progress](nas-streaming-visible-progress/red-hidden-progress.png),
[direct first fragment](nas-streaming-visible-progress/direct-first-fragment.png),
[result first fragment](nas-streaming-visible-progress/result-first-fragment.png),
[Chinese dark narrow](nas-streaming-visible-progress/zh-dark-narrow-first-fragment.png).
They show first-fragment content before the second fragment/terminal event;
reasoning is expanded through the existing disclosure. Zero page errors across
baseline/corrected direct/result and corrected narrow runs. Machine result
context never appears as a user bubble.

- Red: both direct reasoning visibility assertions fail with the old filters.
- Green: four affected Web suites, 77 tests pass (message rendering, runtime
  client/projection, transcript). Two new tests hold/release actual wire frames
  through the production client/transcript/DOM path for direct and host-result
  replies. A second fragment cannot be visible before its event is delivered.
- Existing Go result-origin/final-receipt/request-user-turn checks pass (16
  top-level tests). The actual manager's bounded text append test passes.
- Web typecheck passes. Changed-file ESLint passes with the three existing stub
  prop warnings. UI contract guard passes with its three existing warnings.
- Full `mise run lint` initially could not install unrelated Rust tooling into
  read-only `~/.rustup`. Recovery with `MISE_TASK_RUN_AUTO_INSTALL=false mise run
  --skip-tools lint` completed Web lint (zero errors, 41 existing warnings), then
  Go lint failed two `contextcheck` findings in unchanged
  `internal/agent/tool/subagent.go:389,401` (`Tools$1` / `Tools$2`). That file's
  blob is identical to base: `ade8f1e11e6dfa24ab2d30f9d55eb6f07dd8e97b`.
  No Go/core repair is included in this source candidate; full lint is not green.
- Logs and capture inputs are in `/workspace/streaming-evidence`; no tokens are
  included in this report or committed replay data.

## Remaining acceptance

The sole live-site writer must correlate an existing NAS run's first public
reasoning/status/text event, WS arrival, first DOM mutation, later deltas and
terminal frame, distinguishing direct chat from result summaries. No live run
trace or HAR was provided to this isolated environment, so provider buffering,
NAS proxy behavior, WebView suspension, and the reported live nonstreaming
symptom remain unverified. No paid call is needed to replay existing frames.

The unified owner must integrate this shared component into the final Harmony
source overlay and perform the authorized build/device acceptance. Human QA,
NAS production acceptance and HAP 1.0.28 device latency are not claimed.
