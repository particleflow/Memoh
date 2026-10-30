# Ego 2.1.0 consumer adaptation — field contract still pending

Read [the compatibility incident register at the supplied exact revision](https://github.com/particleflow/Memoh/blob/3a2e5d97e7f26fdcd47195f0f89bbb5c852b01a5/docs/testing/compatibility-incident-register.md) before reuse. The C01 text path works around a handoff failure, not a browser-runtime mismatch. The earlier external Playwright recorder is **not an Ego-supported field deliverable**. Do not import Playwright, obtain a debugging endpoint, launch another browser, open a listener or substitute another browser framework for this consumer.

Consumer facts are from the original writer's actual help/skill read, relayed by the parent: Ego2.1.0, `page.cdp(method, params?, {timeout?})` on this Page's target session; timeout in milliseconds. `page.events()` returns and clears a buffered array. `page.evaluate(fnOrString, argument?)` takes JSON arguments; a browser function cannot capture Node variables. CDP invalidates old element refs. The cloud owner did not independently read the writer's local `C:/Users/Begonia/.agents/skills/ego-browser/SKILL.md`, run Ego, or access the field. Parent reports Server/Web9270 healthy and the previous external recorder never started/no A/B sent.

## What is ready, and what is not

`capture.js` preserves the463cf correlation and ordered reads using **only the supplied Page's cdp/events/evaluate interface**. No `.on`, `.off`, import, external endpoint, alternate browser or permanent worker exists in it. `test.mjs` verifies the logic against explicitly declared fake Page contracts. These fixtures do not establish Ego's actual envelopes or real event retention.

The factory requires explicit `methodPath`, `paramsPath`, `cdpResultPath`, `evaluateResultPath` plus the evidence identifying the consumer readback. It has no default guessed Ego layout. Real paid recording must wait for the one probe below. Do not copy a fixture layout into field configuration as if it had been observed.

The only currently missing field evidence is the real array-entry method/params layout, the cdp result wrapper (especially body/base64 fields) and evaluate return wrapper. We also need whether this one sample actually yields the expected request/response/finish sequence and immediate body read. A successful sample cannot establish buffer capacity, silent-overflow detection, long retention, cross-tool-round continuity or cancellation. Those remain explicitly unverified; the adapter assumes no such guarantee.

## Exactly one no-fee consumer observation request

Give the existing unique writer `probe.js` as plain source text. It defines `probeEgoNetworkShape`; paste it into the existing supported Ego execution with the writer's **already selected, idle fixed-contact Page**. Do not discover an endpoint or create a browser. Confirm sole ownership of `page.events()` during this observation; another reader would clear the same buffer. Do not run A/B.

In that same Ego execution call:

```js
const observation = await probeEgoNetworkShape(page, {
  apiRoot: /* existing API root, kept local */,
  bot: /* existing source Bot ID */,
  contact: /* existing fixed contact ID */,
  timeoutMs: 10000,
  durationMs: 5000,
  reloadExistingPage: true
});
// Return only observation through the supported Ego result mechanism.
```

The numbers above are adjustable **observer** budgets, not task delays/pass criteria. The probe enables Network with no invented subscription or buffer parameters, reads a tiny DOM-ready-state value, and reloads the current fixed-contact Page once through `Page.reload`. That reload lets the existing application issue its normal authorized GETs. It neither submits a message nor modifies model, queue, schedule, permissions or stored history.

It reads/clears the Page buffer until the observation budget or the first exact scoped GET loadingFinished. It inspects actual objects rather than assuming their wrapper. For at most one such GET, it calls `Network.getResponseBody` with the **observed** requestId. It returns only shapes, candidate field paths, method names, batch counts, body length/encoding and operation status. No body text, requestId, credentials, URL or private address is returned. The helper does not infer a second event layout or retry an unknown body read. If nothing suitable is found, the result is `not_observed`; return that once rather than experimenting repeatedly on the live site.

Requested return fields:

- Actual outer event entry shape and candidate `methodPath`/`paramsPath` for requestWillBeSent, responseReceived, loadingFinished/loadingFailed and any WebSocket events observed.
- cdp enable reply shape and getResponseBody reply shape/candidate result path, `body` type and `base64Encoded` type/value; successful read or precise unknown/not-observed status.
- evaluate reply shape and the path to the returned `{probe, readyState}` value.
- Event batch counts/read-clear behavior observed during this one call, and whether the existing GET completed. A missing method in this finite sample is not proof of unsupported capability.
- No additional paid task, no browser/endpoint creation and whether any command was denied, unknown or failed. Reacquire all UI element refs afterward through normal Ego facilities.

This is the requested minimum new field action. It is **not executed by the cloud owner**. Current local output is fixture-only. A tool denial is not permission to substitute Playwright or another channel.

## Adapter lifecycle after that readback is validated

Paste `capture.js` into one existing Ego execution. Supply verified consumer paths; for example the following deliberately uses placeholders instead of a guessed layout:

```js
const recorder = await createEgoCapture(page, {
  apiRoot: /* existing local value */, bot: /* source */, contact: /* fixed C */,
  consumer: 'Ego 2.1.0', timeoutMs: 10000, stopDrainMs: 5000,
  contract: {
    evidence: /* exact successful observation reference */,
    methodPath: /* actual path array */, paramsPath: /* actual path array */,
    cdpResultPath: /* actual path array */, evaluateResultPath: /* actual path array */
  }
});
```

Within the same execution, await each operation. First obtain a fresh source socket and successful scoped GET with the normal approved page refresh, then `await recorder.drain()`. `await recorder.arm('A')` drains pre-arm events before assigning the next UI send. Only then may the already authorized writer's **normal Ego UI action** send A; the adapter contains no send/click/fetch-POST implementation. Drain immediately afterward and sample the exact invocation. When the real original expert is running, do the corresponding `await recorder.arm('B')` and normal one-time B action. Reacquire element refs after CDP, not just after navigation.

Call `await recorder.sample()` on observed state changes or the writer's bounded observation cadence; it drains incoming events, performs exact supported GETs and drains again. Use the original procedure's substantive-final-reply and identity review. No fixed task sleep, held worker, text matching, latest-run selection or queue bypass is introduced.

The writer must keep the recorder object in the **same supported Ego execution** and await operations in order. No Node-global persistence, unawaited background Promise or cross-turn object lifetime is promised. If that execution cannot encompass the existing UI actions and drains, stop at the contract readback and report the precise lifecycle limitation; do not manufacture a new framework/service to keep it alive.

`recorder.records(afterSeq)` is a non-destructive sanitized in-memory read. Save returned batches through the writer's existing supported local evidence workflow. The adapter does not assume a filesystem/import API in Ego. Do not return raw CDP events, credentials or application state. `await recorder.stop()` first waits for serialized body/drain work, then drains known pending responses until its budget. Missing bodies are recorded as unknown. The budget bounds extra draining, not cancellation of an already running cdp command; each such command retains its own timeout.

There is deliberately no invented unsubscribe/cursor/cancel method. `Network.disable` is a Chromium command but does not establish ownership of Ego's existing Network session, so the adapter does **not** disable it or claim underlying buffering was cleaned up. It stops its own reader and clears its credential reference. Page/task cleanup remains the existing writer's supported Ego lifecycle. No completeness/overflow guarantee follows from an empty batch.

## How asynchronous identity is preserved

The single serialized read/clear loop numbers each batch and entry before awaiting another command. requestWillBeSent retains the original invocation; responseReceived records only status, not a supposedly complete body. Only loadingFinished triggers one awaited getResponseBody. A body that expired/failed is unknown and never causes a POST retry. Events arriving while that RPC is pending remain for the next drain in the fixture; actual Ego retention still needs the probe.

The matching202 body supplies `item_id`; queued B then resolves via the existing `follow-up:{item_id}` invocation GET. Redirect/reused request IDs are marked uncertain rather than silently remapped. Orphan completion/error events without a known or intentionally ignored request are recorded as unknown. The original request-only lookup gap remains unchanged when the response is genuinely lost; no private DB read or product API change is added.

A's exact run/turn anchors tool outputs, including runtime frames lacking invocation_id. After B completed is observed, its exact persisted turn is read, **then** the same expert GET. `overlap_candidate` records that sequence and still requires manual substantive-final-text/UI review. Unknown envelopes fail closed; they are not decoded through alternate guessed paths. Internal result receipt fields remain outside the public projections, as previously documented.

## Verification and provenance

Run locally: `node docs/testing/parallel-capture-3f361/ego/test.mjs`. The fixture imports Node test utilities only; the deliverable adapter/probe have no imports and never launch a browser. An isolated evaluate realm catches Node closure capture. Coverage includes explicit direct/nested fake wrappers, pre-arm stale events, serialized destructive reads, delayed202 versus earlier409, post-data/body retrieval, base64 decoding, body-RPC/event overlap, exact A/B/worker identity, causal order, changed worker,403, expired bodies, stop gaps and redaction. See `fixture-result.json`. No old product tests/builds or paid calls ran.

The first fixture execution failed because Node strict deep equality compared cross-realm empty-object prototypes inside the fake cdp assertion; it was corrected to compare serialized parameters. This was a fixture assertion issue, not a real Ego failure. Subsequent checks pass, with consumer acceptance still pending.

`cdp-reference.json` records the official Chromium protocol source and hash used for command/parameter verification. [Chromium Network protocol](https://chromedevtools.github.io/devtools-protocol/tot/Network/) defines Network.enable, getResponseBody and loadingFinished; it does not define Ego's event wrapper or buffering guarantees. `compatibility-register.patch` adds a narrowly scoped pending C12 entry against the exact supplied register; it does not mark field adaptation complete. Apply only that entry during integration, preserving all existing incidents and later owner updates.
