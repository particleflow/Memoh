# One instrumented recipient task entrance

**Next-action update:** d74's production precheck found ordinary history itself
unrendered (eleven skeleton nodes, zero rows, no anchor placeholder). Use
[initial-load observation](capture-initial-history.md) next. Do not require
healthy loaded history or open/click the task directory in that round. The
earlier row-click procedure below is retained as history, not the current step.

Read the applicable C05/C11 entries in the
[compatibility incident register](https://github.com/particleflow/Memoh/blob/3a2e5d97e7f26fdcd47195f0f89bbb5c852b01a5/docs/testing/compatibility-incident-register.md)
first. This follows the parent's new
explicit instruction for one instrumented, read-only navigation. The first
incident remains unresolved; the unchanged bare click and old tests are not a
plan. The old process-metadata denial/cleanup and all retained data are untouched.

## Consuming interface and limits

The parent supplies these Ego2.1 contracts:

```
page.cdp(method, params?, { timeout? })
page.events() // returns and clears an array
page.evaluate(JSON-parameters) // not a Playwright function/string callback
```

This environment has no Ego SDK/browser tool or independently readable Ego2.1
help. Do not claim field SDK verification. The observer uses only the given
`cdp` and destructive-array `events` signatures. DOM reads use the standard
CDP `Runtime.evaluate` JSON parameters, so it does not guess the convenience
`page.evaluate` object's keys. It creates no external Playwright client and
uses no `.on()` listener. CDP method/parameter references:
[Runtime](https://chromedevtools.github.io/devtools-protocol/tot/Runtime/),
[Network](https://chromedevtools.github.io/devtools-protocol/tot/Network/).

The parent now relays `navigation603-preflight/OUTCOME.json`: the writer checked
direct `{method, params, sessionId}` events, direct CDP/evaluation results and a
completed GET body, without a production row click. This replaces the earlier
unknown-envelope diagnosis; this owner has not read the raw receipt. Do not
repeat that shape/body preflight or ask for a new read-only authorization.
Official help does not promise click-buffer retention. First execute the
[one disposable-page buffer experiment](capture-buffer-contract.md), then
return that receipt to the parent. No production DOM or new service is involved.
The experiment cannot establish permanent retention. During the original-record
capture, always mark the click interval as potentially incomplete and treat
missing events as unknown, even if the experiment retained its synthetic events.

At runtime, the observer checks the envelopes before allowing a click. It requires an existing component directory poll to
produce request, response200 and `loadingFinished`; a response200 alone is not
completion. A different envelope or missing event delivery stops with no click.
Do not guess a wrapper, silently accept empty events or switch to another
browser/event interface. A newly different schema is a changed condition to
report, not a navigation retry grant. Unknown envelopes after the click make
coverage explicitly partial.

## Sole writer preconditions

Execute only after the writer is free, in its supported original Ego/executor
flow. No deployment or configuration changes are included.

1. Read the saved original receipt for its authoritative turn ID and two UI
   message IDs. Do not query DB metadata or invent IDs. The fixed target is:

   ```
   Bot: 791af2c4-2d89-4c57-b863-5d5ff53ff9f7
   session: c736a091-4c7d-4196-9cd6-00588de2a0ae
   type: bot_delegation
   task_id = record_id: 1d742cc8-2760-44f7-a889-d66cd8461502
   run: 9dfd7812-592c-482c-8083-abacda7278d9
   ```

2. Start at that already loaded read-only execution history. Both original
   messages must exist, no input must be visible, and Home must not be masked.
   Its URL must have only the original `session` association and no typed task/
   run query. A row click on an already identical task URL is a duplicate
   navigation and cannot stand in for first entrance. If normal setup of the
   plain historical route is needed, use the writer's supported existing history
   navigation once, before capture; never inject `history.replaceState`, route
   params, store flags or a contact binding. This observer does not promise an
   arbitrary cold app startup is write-free. If this precondition cannot be
   reached within the authorized existing flow, stop and report it.
3. Open the existing recipient Tasks dialog once as preparation and identify
   the same original delegation row using the writer's previously verified
   interaction. This is preparation, not another button/identity acceptance
   test. Leave it open for the existing directory poll used in interface checks.
   Stop for an unavailable/disabled/ambiguous target, permission error or changed
   identity; do not force a click.

## Execute once

The observer is an imported function, not a new browser connection or launcher.
Pass the original page, the actual normal row-click operation already used by
the writer, and its supported append-only local JSON writer. No new Ego locator,
CLI flag or SDK connection API is invented here. The integration is:

```js
import { captureRecipientNavigation } from './capture-ego-navigation.mjs'

const result = await captureRecipientNavigation(page, {
  turnId: originalReceiptTurnId,
  messageIds: [originalUserUiId, originalAssistantUiId],
  // Exactly one normal visible-row click. No wait for messages/URL, no repeat,
  // no navigation, refresh or events() read inside this callback.
  // Use page.click with the previously verified selector and supported options.
  // No separate events() reader; retention across the click stays unknown.
  clickOnce: originalVerifiedRowClick,
  // Persist every sanitized JSON record before another events() drain.
  // Use the writer's existing supported local file operation; no download or
  // Library materialization helper is part of this observer.
  append: appendOneJsonRecord,
  captureMs: 30000,
  preflightMs: 6500,
  sampleMs: 500,
  commandMs: 3000,
})
```

These are observation/command budgets, not a product timeout, retry policy or
proof of failure at30 seconds. The writer may choose a suitable single window
before starting and records it. Do not rerun a failed self-check unchanged.
The foreground script must return through its real original exec session;
retain that session if the executor yields. No detached fixture is required.
Only this observer may explicitly read `page.events()` during the window. Use
the direct documented `page.click(selector, {timeout: ...})`, without an added
helper that explicitly reads events. There is no invented retention option or
official promise to wait for. The disposable experiment supplies bounded
evidence; `page.click`'s production boundary remains an event-coverage gap.
Before starting, append the experiment receipt reference and
`productionClickEventCoverage: "potentially-incomplete"` using the same journal
sink. If it fails, do not start the collector. This is evidence metadata, not a
new UI field. Retain positive events and DOM witnesses; never infer an absent
snapshot/request or completed initial component load across that boundary.

Before arming, the first drained batch is labelled `pre-arm` and retained as
sanitized metadata, then excluded from the new action's request inventory. Each
later drained array is persisted before another read. The pipeline self-check
uses an already occurring directory poll; the observer sends no HTTP probe.
Only then is the supplied row click invoked once. `clickAttempted` means the
callback was entered; `clickReturned` is separately recorded. Neither means
navigation completed. A click/command timeout has an uncertain outcome: do not
call the callback again.

The observer records:

- Component request IDs/paths, selected identity query fields, method, CDP start/
  response/terminal times/status and abort/error metadata. No headers, cookies,
  token query, request body or raw response transcript is saved. A completed
  target component response may be read through `Network.getResponseBody` for
  task/session/turn/message IDs only; there is no fetch-URL fallback.
  In the journal, `event` is the CDP event name and a request's `method` is its
  HTTP method. Network completion/body metadata still does not prove the SDK
  accepted that response or committed it to the visible transcript.
- Target-session runtime subscribe/unsubscribe/snapshot/delta/dropped metadata,
  run/turn/epoch/seq and socket close/error IDs. It does not reconnect the socket
  or synthesize a snapshot. Absence of a frame is not proof of missing delivery;
  capture starts after the original page loaded, so a snapshot may predate it.
  A snapshot's current run may be newer than the historical task; it must not
  replace the original run pinned by the task association.
- Actual DOM: Home's `invisible` ancestor, original readonly footer/pane, the
  anchor placeholder and scroll-area `display`, history skeleton count, and
  original message-ID presence/visibility/viewport intersection. It reads no
  Vue private instance, store, token, localStorage or guessed loading flag.
- Runtime exception class and source-frame function/path/scheme metadata, and
  console-error argument types/history-load signature. Extension frames remain
  distinguishable; raw user content/console argument values are not retained.

`hostUtc` is the journal-receipt time, not the original event occurrence. Network
CDP timestamps are monotonic seconds; Runtime timestamps are their own protocol
timestamps. Preserve units and original values; do not fabricate UTC or tool
execution durations from message/append times.

Unexpected writes, a scoped read denial, changed original task association,
unknown consuming result or CDP read error end the collection, preserving the
partial report. No fallback reader or repeated action follows. A write already
observed is not undone by stopping; its outcome must remain unconfirmed until
an authorized readback. The collector never intercepts/blocks/rewrites traffic.
If the journal write itself fails, it is not retried: no further event drain or
click follows, and the rejected error retains sanitized `captureState` in memory.
Use the original tool result and read back the existing file before deciding
its outcome; do not restart the row action or change a rejected file path.
Do not reset cache, disable TLS, change network/permission policy, modify DOM,
enable Vue devtools or add product UI to make observation pass.

Persist the summary, then close only the writer's owned Ego space/temporary
script through its normal interface. Leave original incident files intact.
Network/Runtime domains are not disabled by this helper because the page's Ego
owner may already use them. No new app listener, timer or global instrumentation
survives completion. No refresh is included in this capture window.

## Interpret evidence, not imagined state

| Observable witness | Next narrow isolation target; not yet a field root cause |
| --- | --- |
| Home's workspace ancestor remains `invisible` | Home admission/session-selection lifecycle; correlate the actual task/session request terminal states |
| History skeleton nodes persist and original IDs are absent | Message hydration; if a new subscribe is actually observed, correlate its target snapshot and completed REST metadata |
| Home unmasked, original IDs present in DOM, but task scroll-area remains hidden with anchor placeholder | Original-turn admission/anchor lifecycle; the transcript already rendered those rows underneath its mask |
| Completed metadata/snapshot but no DOM rows, or event envelope/coverage incomplete | Keep the result undetermined; do not claim the anchor or snapshot wait as cause |
| The messages appear without refresh during this run | This new sample converged; preserve the original failed first entrance, do not call its cause fixed |

The gates may overlap. Do not force a single classification from a generic
loading string. No observed `runtime_subscribe` means no new-snapshot obligation
has been established by this capture. The server always snapshots a subscription;
do not remove history/runtime atomicity to work around an unproved wait.

## Local validation boundary

`node --test capture-ego-navigation.test.mjs` covers synthetic DOM gate witnesses,
privacy projection, demonstrated/unknown CDP envelopes, serial draining and one
callback, denial without fallback, 200-without-terminal remaining pending,
uncertain click outcome without repetition, and journal rejection without retry.
The saved local result contains eight passing tests and no skipped tests;
[targeted test output](capture-contract-tests.txt) records the run. Both observer
files also passed targeted ESLint and `git diff --check`.
It is a contract simulation and jsdom test, not Ego execution, a live network
capture, product red/green, fee/model execution, or NAS acceptance. No previous
product tests or old field samples are rerun. Update the original README and C11
only after a new cause/recovery is actually established.
