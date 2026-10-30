# Ego 2.1.0 collector: observed envelopes and bounded reads

Read the [compatibility incident register at the supplied revision](https://github.com/particleflow/Memoh/blob/3a2e5d97e7f26fdcd47195f0f89bbb5c852b01a5/docs/testing/compatibility-incident-register.md) before reuse. The external463cf Playwright recorder is historical evidence, **not an Ego field deliverable**. Do not import Playwright, obtain an external debugging endpoint, launch another browser, open a listener or substitute another browser framework. C01's Windows `os.setxattr` handoff failure is separate; use these source files as plain text in the existing supported Ego execution.

## Consumer evidence and limits

The unique writer actually observed Ego2.1.0 in turn `01a18c43-321a-764e-af5c-025dac3e5fbc`, relayed by the parent. `contract-ego210.json` records that evidence:

- Events are directly `{method, params, sessionId}`. Paths are `['method']`, `['params']`; request ID is `params.requestId`, request/response are `params.request`/`params.response`.
- `Network.enable` returned `{}`; evaluate returned `{probe,readyState}` directly. Both result paths are `[]`.
- One existing scoped GET finished and immediate `Network.getResponseBody` returned `{body:string,base64Encoded:false}`, length522. Body content was not handed off.
- 2712 reads yielded141 nonempty batches/605 events; largest observed batch47. The2571 empty reads were94.8% of reads. The old probe and final drain loops had no pacing. The observation duration was not supplied, so these counts establish neither field reads/second nor capacity/loss.
- No loadingFailed, WS sent/received/closed, POST body or base64=true was observed. Capacity, overflow detection, long retention, cross-turn lifetime and cancellation of an in-flight Ego call remain unestablished.

The cloud owner did not read the writer-local `F:/NAS-Assistant/tmp/ego349-consumer-probe/` files or execute Ego. Parent reports one no-fee GET refresh, no A/B/DB/model requests, resources cleared. **Do not repeat that shape probe.** `probe.js` is retained with the same pacing correction for future source reuse, not a request for another field probe.

The previously read Ego help/skill permits `page.cdp(method, params?, {timeout?})` on this Page target, milliseconds; `page.events()` reads and clears an array, not an EventEmitter; `page.evaluate(fnOrString, argument?)` takes JSON arguments, with no Node closure capture. CDP invalidates element refs. `capture.js` uses only those interfaces. The Chromium [Network protocol](https://chromedevtools.github.io/devtools-protocol/tot/Network/) and `cdp-reference.json` support the CDP parameter names, not Ego buffering guarantees.

## What changed

All destructive buffer reads, including concurrent callers and final draining, now pass through one serialized reader with an explicit minimum interval after the previous read completed. The interval and finite observation/drain budgets are caller configuration, not task rules. The wait uses a browser timer through `page.evaluate` with a JSON argument. A wait/result mismatch records unknown and stops collection; there is no fast-loop fallback.

`collectFor(durationMs)` only collects existing events and then stops. It does not send messages or query additional APIs. `requestStop(reason)` prevents new samples/arming; `stop()` waits for already-issued samples/body commands, performs paced additional draining, preserves unfinished requests as unknown and is idempotent. Its drain budget bounds additional reads, not cancellation of an in-flight Ego RPC. A stuck Ego call cannot be forcibly cancelled through an undocumented API. The collector never cancels a business run; only its own optional read-only fetches use their existing observer timeout.

`summary()` contains counters and status without request bodies, credentials, actual URLs or task IDs. Full `records()` may contain conversation content despite credential/address redaction; keep those records in the writer's existing restricted evidence workflow and review before sharing. No filesystem/import API is assumed inside Ego.

## Next field step: one no-fee collector self-check

This is the only requested next action. It has **not** run in the field. Use the existing idle fixed-contact Page and its original authorized login. The writer must be the sole `page.events()` consumer and keep this object within one supported Ego execution; do not assume state survives another tool round. Paste the complete `capture.js` source, then run this sequence using existing local API/Bot/contact values (never put those values or Authorization in the handoff):

```js
const recorder = await createEgoCapture(page, {
  apiRoot: existingApiRoot, bot: existingSourceBotId, contact: existingContactId,
  consumer: 'Ego 2.1.0',
  timeoutMs: 10000, stopDrainMs: 2000, pollIntervalMs: 250,
  contract: {
    evidence: 'parent-reported writer turn 01a18c43-321a-764e-af5c-025dac3e5fbc',
    methodPath: ['method'], paramsPath: ['params'],
    cdpResultPath: [], evaluateResultPath: []
  }
});
try {
  if (!recorder.summary().fault) {
    await page.cdp('Page.reload', {}, {timeout: 10000});
    await recorder.collectFor(5000);
  }
} finally {
  await recorder.stop('self_check_complete');
}
const result = recorder.summary();
// Return only result through Ego's existing supported result mechanism.
```

These example numbers are adjustable observer budgets. The sole reload lets the application issue its ordinary GETs. Do not call arm/sample, submit A/B, change permissions, invoke a model or use SQL. Reacquire UI refs afterward. The collector starts before the reload; the immediate first read is allowed, subsequent reads are paced. `minimumReadGapMs` measures completed-read to next-read-start using the observer clock, not server task time. Record observed reads/waits/minimum gap/unknowns, `scopedGetObserved`, stopped/fault/pendingResponses, and the configured budgets. No exact batch count is required. A clock jump, missing GET, wait-contract failure, missing body or unresolved response is a limitation/unknown, not successful self-check. Retention/overflow remain unverified even if the summary is clean. Only the waiting subcase and paced actual collector are new field checks; do not rerun the completed envelope probe.

`Network.disable` is deliberately absent: the adapter does not own Ego's preexisting Network session. It stops its reader and clears its credential reference; Page/task cleanup follows the writer's existing supported Ego lifecycle. An empty batch is not proof of lossless capture. If this consumer cannot encompass the sequence within one supported execution, report that precise lifecycle limitation; do not open another browser or background service.

## Subsequent single real parallel sample — preparation only

Do not execute a paid sample as part of this change. After the free collector self-check, the parent coordinates the unique writer for one already-authorized bounded sample. Do not add a new fee question. The following preserves the existing source/invocation/queue contracts; it does not change scheduling.

1. Create a **new** recorder before any A send, with the observed contract and explicit pacing/budgets. Refresh normally once and drain until a successful scoped GET and fresh source socket have been observed. Inspect actual WS shape if naturally available. No fabricated message may be sent just to manufacture WS coverage. `arm` refuses missing prerequisites. `collectFor` automatically stops its recorder, so do not run it and then try to arm that stopped object.
2. `await recorder.arm('A')`, then use the existing normal Ego UI action to send the one authorized A. Reacquire refs after CDP. Immediately drain/sample. Bind A from its original WS invocation or original queue request plus the matching completed202 body, never text or latest run. Preserve request invocation, response status/body, `item_id`, `enqueued_during_run_id`, and admitted `follow-up:{item_id}` when queued. If real WS/POST shapes differ, preserve unknown and stop further submissions; do not guess wrappers or resend A.
3. `sample()` GETs the exact observed invocation to obtain A's run/turn. A's own tool return or exact turn identifies delegation and target Bot/session/run. Query only that original delegation. When it is actually running, arm B and perform one normal UI send. Do not delay the worker, change a timeout, bypass the queue or branch on sample text/name to create overlap.
4. Continue awaited, paced `drain()`/`sample()` calls inside the same supported execution and the writer's finite observation budget. A queue202 proves receipt only. B must resolve through its own queue item/admitted invocation, reach completed, and have its exact turn persisted with a substantive final assistant reply. The observer then GETs the same original worker. An `overlap_candidate` requires this causal order: B completed lookup → B exact persisted turn → original expert still running. Review final reply content/UI; progress/empty output is not a pass. Do not compare client and server wall clocks to infer overlap.
5. Observe that same original task's terminal result and natural result reply to the same C, then one normal refresh to check the persistent reply does not multiply. Inspect exact run/turn/tool identities, not matching text. Public projections do not expose the internal result receipt; do not claim DB receipt uniqueness or extend SQL access from this evidence. Record what the API/UI actually establish.
6. On deadline, missing response, unresolved mapping, completed worker before B's reply, auth failure or lost observation: stop the observer and retain unknown/inconclusive. Do not cancel the real task, replay A/B, restart the expert or loop paid trials. Await `stop()`, save sanitized full evidence locally, and return only the reviewed result/identity table. A normal late result may still finish independently.

The adapter does not supply or assume concurrent Ego command execution. Each writer action/drain/sample is awaited; no unawaited Promise or cross-round recorder survival is promised. A missing lifecycle capability is a blocker, not permission for a framework workaround. Longer polling intervals can miss a short overlap; observed batching cannot guarantee retention. Such a sample is inconclusive rather than a product failure.

Suggested handoff fields: source version/contact-unchanged, A original invocation/run/turn, B transport/request invocation/HTTP status/item_id/admitted invocation/run/turn, original delegation/target identities, B-completed/history/subsequent-worker evidence sequence numbers, substantive reply review, terminal result and refresh observation, observer unknowns, stop status, and outcome `pass|inconclusive|failed` with exact reason. Keep credentials, private addresses and raw history out of the handoff.

## Correlation and verification

Each read is numbered before any subsequent await. Only loadingFinished triggers awaited getResponseBody; responseReceived alone does not imply a complete body. Delayed responses retain their original request ID; redirects/reused IDs are uncertain. Lost/expired bodies never trigger POST replay. `getRequestPostData`, WS payload decoding, loadingFailed and base64 decoding retain their original fixture coverage, but **have not been verified in this real consumer**. Synthetic fixture events do not close that gap.

The349 fixture evidence (`fixture-result.json`, six groups/sixteen boundaries) is reused without rerunning unchanged tests. `test.mjs` only gained the now-required observer interval in its fake configuration. New `test-pacing.mjs`/`pacing-result.json` cover ten changed boundaries: old/new empty-read behavior; bounded passive reads; serialization; delayed finish/idempotent stop; unresolved-response retention; stop during wait; bad wait result; invalid pacing configuration; in-flight body completion; and in-flight exact GET on stop. All pass with zero production requests, browser launches or model calls. The old1000/new10 read comparison uses a virtual clock with synthetic1ms events-call cost; it is not an Ego performance measurement.

The five source paths in `source-evidence.json` were already verified byte-identical3f361→9270. No product code/build/deployment changed. `compatibility-register.patch` updates only C12 against the exact3a2 register; integrate that entry without overwriting later incidents. The actual envelope remedy is field-observed; this new paced collector self-check, WS/POST mapping, long retention/overflow and actual parallel acceptance remain pending.
