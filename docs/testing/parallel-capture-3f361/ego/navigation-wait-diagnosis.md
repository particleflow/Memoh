#890 preflight: page-owned pacing failure, no business submission

Status: source defect localized; actual timing/root trigger and a supported Ego host-wait binding require one read-only consumer excerpt. No replacement timer API is invented or field-ready patch claimed. Do not rerun890, repeat the free self-check, send A/B, enlarge timeouts, remove pacing or move to another browser.

## What is known

Parent readback from writer turn `01a18c6a-52f2-71ca-aec9-32e493efa35e` reports: after the first empty event batch, the page-evaluate timer failed with `PageEvaluationTimeoutError` / `EGO_PAGE_EVALUATION_TIMED_OUT`, approximately15 seconds of page nonresponse. GET/socket readiness was not established. Execution stopping could not be confirmed at that moment. The tool subsequently closed owned space61, four temporary scripts were removed, no resources remain in flight. No A/B, model or other business submission occurred; unknown status is confined to that evaluation in the already-closed Page. Do not label it an unknown business run.

The source blob of capture.js at both4120 and890 is exactly `d5a1269a9695ae8b41392d6bd4b3a3975d50324f`.890 changed the plan/documentation, not this implementation. The4120 self-check remains valid evidence:20 reads/waits, minimum253ms,10 completed GETs, clean stop, natural WS frames. It proves one successful execution, not survival of every navigation timing. No claim can yet be made that890 actually used a different call order, or that its execution context was destroyed; the parent summary does not include those records.

## Source-localized dependency

`capture.js:143–151` implements observer pacing as a Promise containing setTimeout inside `page.evaluate`. `drainOne` calls it before every subsequent buffer read, including before readiness is known and during final draining. The factory already does one initial drain. Therefore the next drain after a fast reload can call the page-owned timer while the page is still loading or replacing its execution context. If the first drain itself is empty, that does not prove either navigation completion or page execution availability.

The old README's sequence is create collector → Page.reload → collectFor; the live plan permits refresh → drain/arm. Both retain the same vulnerable dependency. A slower first action or already-settled page could avoid it; a faster sequence could hit it. These are explanations of how the same code can behave differently, **not established facts about the two field sequences**.15 seconds is the observed wrapper failure interval, not a business timeout or evidence that the configured250ms delay itself ran for15 seconds. The collector's `timeoutMs` is passed to cdp and read-only GETs, not as an option to `page.evaluate`; increasing it would not establish timer lifecycle safety.

The official Chromium [Page protocol](https://raw.githubusercontent.com/ChromeDevTools/devtools-protocol/master/json/browser_protocol.json) defines reload separately from load/lifecycle events, without a returned ready-state guarantee. The [Runtime protocol](https://raw.githubusercontent.com/ChromeDevTools/devtools-protocol/master/json/js_protocol.json) places awaited promises in execution contexts and exposes context-destroyed/cleared events. These protocol facts explain the architectural hazard; they do not establish Ego's host timer API or diagnose this actual failure. Do not enable new domains or collect new browser traffic to fill historical gaps.

If an evaluate rejection reaches the still-running collector, its existing catch records observer_wait_unknown, sets fault and stops further reads; stop then returns without another evaluate. A tool-level timeout/terminated outer execution may prevent that catch/finally from running. The cloud cannot infer the latter lifecycle from the error string. Source cleanup is not proof of confirmed field cleanup; use the writer's explicit closed-space receipt.

## New isolated evidence

`test-page-wait-failure.mjs` injects the observed error into the exact unchanged4120/890 source. Two narrow cases (with/without a recorded reload) confirm first empty read → page wait error → no second read/business operation. The catch/stop works only when the fake host actually returns the rejection into the collector. This does **not** reproduce a real browser navigation or Ego process termination. `page-wait-diagnostic-result.json` preserves that distinction. No old fixture/self-check/build was rerun; no live/browser/model request occurred.

## One minimal read-only excerpt for the original writer

Read only existing journal/OUTCOME and the installed Ego2.1.0 help/skill; do not recreate the space, reload, evaluate, inspect new network traffic or run a timer probe. Return a single sanitized text reply containing:

1. From `parallel-live890-20261030/journal.jsonl`: sequence number, operation name, start/end or elapsed time, and success/error code for collector creation/Network.enable, the first page.events read, any Page.reload or normal navigation action, the failing evaluate, stop/finally if reached, and the existing close-space result. For the evaluate include only the timer callback shape, JSON delayMs and any explicitly supplied timeout. No URL, headers, cookies, authorization, message content or private address.
2. The actual wrapper script's small call-order excerpt spanning createEgoCapture → reload/navigation → drain/collectFor/arm, plus where its outer timeout/catch/finally was configured. Include any difference from the successful4120 script at those same points, if those original files/records still exist. If cleanup removed source and the journal does not record it, mark missing; do not reconstruct or replay.
3. Exact installed help/skill excerpts for **host-side** awaited delay/timer support and outer execution lifetime: primitive name/signature/import rules if documented; whether it runs outside the target Page; how top-level await/object lifetime survives Page.reload; and what evaluate/outer-execution timeout does to catch/finally/cancellation. If no host timer is documented, explicitly report that fact. Do not assume Node globals, `task.sleep`, `page.waitForTimeout`, or a tool-call sleep API from another environment.
4. Only if already present in the journal: order of Page load/navigation and Runtime context-created/destroyed/cleared events around the failure, with opaque identifiers replaced consistently. If never recorded, state not recorded. No new subscription is requested.

The known no-A/B/model and closed-space facts need no additional DB or business-state reads. This is a consumer contract/timing evidence gap, not a fresh authorization request or a denied operation.

## Minimal repair boundary after the excerpt

Replace only the observer pacing dependency with the **documented host-side** awaited delay that survives target-page navigation, retaining the existing explicit interval, serialized destructive reader, finite observer/drain budgets and error/unknown reporting. Record the supported host execution evidence alongside the wrapper contract. No default guessed binding or fallback to page.evaluate, busy spinning, blocking shell sleep or alternate framework.

Before production reuse, a narrow offline test must model page evaluate being unavailable during reload while the supported host wait continues and events are read at the configured interval; stop/error must not cancel business work or silently drop pending responses. A consumer check should be proposed only once the exact supported primitive and execution lifetime have been read, with no business submission. If no such primitive/lifetime exists, the remaining blocker is the Ego execution contract; return that precise limitation rather than launching a browser/service or pretending a generic injected callback is a verified Ego implementation.

Remaining page.evaluate uses for exact GET and base64 decoding are actual data operations, not pacing. They must retain explicit failure/unknown behavior; moving the timer does not prove those calls survive arbitrary navigation. Business A/B remains held until the observer lifecycle is resolved. The existing source finding that experts run independently is unchanged, and prior inconclusive business samples remain unmodified.
