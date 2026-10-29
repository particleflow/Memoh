# Existing-run observation: sole live-site writer only

This is a read-only evidence procedure, not a request to send/retry/resume a turn,
replay an action, reload the application, rebuild, deploy, or enable new model
calls. Do not ask the user to repeat the request. Keep all control with the sole
NAS/browser writer. The source candidate is not a deployed or device-accepted
fix. Existing `subagent.go:389,401` lint findings remain untouched.

## Minimal procedure

1. Select one **existing exact run** from its saved task/turn association. Record
   server/web build, client/HAP version, bot/session/run/turn/invocation IDs and
   whether the run is direct chat or a result-summary run. For a summary, retain
   both the original task execution run ID and its distinct summary run ID;
   inspect only the host receipt marker/association, never render the machine
   input. Do not infer identity from the last reply in the session.
2. Reuse existing WS/provider/adapter traces or the existing browser's recording.
   If the original run is still active, attach observation to its **current**
   `/bots/{bot_id}/web/ws` connection (the compatibility `/local/ws` may appear).
   A CDP `Network.webSocketFrameReceived` listener / DevTools WS Messages view
   can observe that connection without sending a chat command. Record the time,
   target session, epoch, seq, run identity, delta kind, block ID and content
   **length** for each frame. Keep only target-run frames; do not export token
   query strings, Authorization, whole HAR bodies or reasoning/tool text.
   Mark the point capture began; missing earlier frames are unknown, not absent.
3. In the same already-open target pane, keep the page foregrounded and record
   `document.visibilityState`. Observe `[data-message-id] [data-chat-content]`
   for the target assistant message ID and its `.prose` child. Record the first
   nonempty text mutation, later length changes, existing thinking/status
   visibility and terminal state; take a screenshot while the first fragment
   is actually visible. A `MutationObserver` records DOM change, **not paint**;
   a screenshot or frame recording supplies the visible-state evidence.
   Keep a bounded timestamp/length log, not message text.
4. Compare received WS frames with the existing reactive state. On this exact
   source `useChatStore` has Pinia ID `chat`; its exposed
   `runtimeObservationFor(botId, sessionId)` is a read-only getter returning
   `{ synchronized, run }`. Read its run ID, status, block IDs/types/lengths and
   the already-focused store `messages` (only after confirming its bot/session
   matches). Do not call `chatView`, navigation, subscription, initialization,
   refresh, send, retry, abort or recovery methods: some create views/requests.
   If the store is unavailable in production, use already-available Vue devtools
   or nonpausing source-map logpoints at `createRuntimeClient.handleEvent`,
   `flushDeltaBatch` and `applyRuntimeTranscript`; do not change deployed code.
   Record seq gaps, epoch mismatch, `runtime_dropped`, snapshot recovery and
   synchronization state rather than attributing every missing update to render.
5. Align each boundary on a calibrated clock and compare the first public text
   delta, a later delta and the terminal event. WS CDP timestamps, page
   `performance.now()` and server wall clock have different origins: preserve
   each clock domain and calibration uncertainty. Correlate IDs/seq/block first;
   do not subtract uncalibrated clocks. Record direct and summary runs separately.
   Stop/export the observation without sending any execution command.

The browser-only portion needs no model call. A reconnect/current snapshot is
not historical delta replay: the runtime subscription explicitly sends current
state, not a durable event history. If the selected run has already finished and
its original frames/traces were not captured, report **insufficient historical
evidence**. Do not fabricate its first-delta timing from the completed reply or
start another run to fill the gap. Existing sanitized frames can be replayed in
the isolated local fixture later; that result proves consumption behavior only.

## Boundary evidence and interpretation

Use only **already-recorded** public provider/network/adapter events. Do not add
production logging or restart the server for this procedure. Distinguish an
upstream public semantic delta from an HTTP packet, heartbeat or text-start.

| Observed boundary relationship for the same run/attempt | Supported conclusion |
| --- | --- |
| Upstream semantic text/reasoning deltas absent until late; SDK/native/WS/DOM follow promptly | Upstream/provider late output is supported. Raw upstream evidence must cover the interval; a browser with no WS delta alone cannot establish this. |
| Raw upstream semantic deltas arrive incrementally, but SDK `TextDeltaPart` / `ReasoningDeltaPart` first appears in a late batch | Provider SDK/parser/adapter batching is supported. It is not a frontend diagnosis. |
| SDK parts arrive incrementally, but native public `text_delta` / runtime `message_appends` or WS output arrives in a late batch | Delay lies after SDK adaptation: compare native publication, runtime seq and socket/proxy delivery to narrow the server/transport boundary. |
| Incremental WS frames arrive, but synchronized runtime blocks do not change | Frontend event acceptance/projection boundary: verify target identity, epoch/seq continuity, awaiting-snapshot state and foreground frame scheduling before calling it buffering. |
| Runtime blocks change, but focused transcript does not | Transcript/view merge or loading/visibility mask boundary. |
| Transcript changes, but corresponding visible DOM does not | Message filter/renderer boundary. At deployed 5a42, `hideProcessActivity` suppresses public reasoning and runtime status; this candidate removes exactly those filters. |
| DOM grows incrementally, but screen capture is delayed | Renderer scheduling/paint/WebView foreground behavior; DOM timing alone cannot establish paint or provider delay. |

`agent.model.first_part_ms`, if it exists in an already-recorded model span,
measures the first SDK content-related part. This source counts text/reasoning
**start** and tool-input start too; it is not specifically first text delta and
is already downstream of the provider adapter. Persisted `reasoning_timing`
records segment duration, not absolute upstream arrival or time to first token.
Neither scalar can distinguish provider withholding from SDK buffering alone.
A final persisted history row, task completion time or publication receipt also
cannot establish incremental display.

## Exact handoff record

For each selected existing run return: source/client versions; direct vs summary
identity mapping; capture coverage/start; upstream/SDK/native evidence available
or missing; first and later WS delta seq/block/length/times; projected/transcript
lengths and synchronization; first DOM change/visible capture; terminal seq/time;
clock calibration; conclusion and unresolved boundary. If upstream or adapter
traces are missing, say that the upstream-vs-adapter distinction is unverified,
even when the frontend branch can be decided.
