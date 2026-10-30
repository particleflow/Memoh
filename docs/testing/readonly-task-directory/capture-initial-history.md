# Observe the original history from its initial document load

This is the next action for the sole writer. It supersedes the requirement in
the earlier row-click plan that the history must already be rendered. Do not
open Tasks or click the delegation in this round. Do not rerun the passed Ego
buffer experiment or its old eight local tests.

## New evidence and its limit

The parent relays `F:/NAS-Assistant/tmp/navigation-d74-live/OUTCOME.json`:
the ordinary original history URL had no task/run query, Home was unmasked,
the read-only pane had no input, and its scroll-area display was `block`.
There were eleven history skeleton nodes, zero message rows, neither original
UI message ID, and no anchor placeholder. No directory/row click, refresh,
manual API probe or model invocation followed. Spaces58/59 were closed with no
in-flight writer. This owner has not independently read the Windows JSON.

That sample supports **unrendered initial history**, not rendered messages hidden
by the task anchor. It does not identify whether the original message REST read
was pending/failed, or whether its commit was awaiting runtime. The earlier
first-click symptom may share this dependency, but that causal link is unproved.

Source9270 (the relevant files are unchanged from this branch's product patch):

- `pages/home/components/chat-pane.vue:1533` derives visible messages from the
  pane's transcript. Its skeleton condition is empty visible messages plus
  `loadingMessages`, revealed after a delay. This is a source-based inference
  from the observed skeleton, not a read of private Vue state.
- `store/chat/transcript.ts:218` sets the masked initial load, fetches original
  messages and awaits `commitInitialHistory`; only its owned `finally` clears
  the loading mask. `visibleMessages` is empty during that mask. Therefore zero
  rendered rows do not establish that the database/history response is empty.
- `store/chat/realtime.ts:226` starts a target-session subscription and the REST
  preparation. Its commit callback awaits `initialSnapshotReady`, then applies
  history and buffered runtime projections together. The runtime snapshot
  handler resolves that promise. Keep this atomicity intact.
- `composables/api/useChat.message-api.ts:17` uses the component's generated GET
  `/api/bots/T/messages?session_id=S`; after decoding it serializes timestamp
  Dates. The observer records only IDs and timestamp type/parseability, not text.
- `store/chat/bootstrap.ts:282` and `pages/home/index.vue:260` select the explicit
  historical session. The history-route guard skips contact resolution; do not
  open the plain Bot/contact route as setup. Source does not by itself prove a
  cold boot is write-free. Observe unexpected commands without claiming to block
  or undo them.

Existing realtime/transcript tests already cover REST-before-snapshot,
snapshot-before-REST, atomic commit and stopping a pending subscription. They
were inspected, not rerun or counted as reproduction of this incident. No
specific faulty transition is established yet, so no product fix/red-green is
claimed and no snapshot wait is removed.

## One bounded initial-load capture

1. Reuse the writer's supported authorized browser context and create a newly
   owned disposable page. Explicitly call its official `page.goto('about:blank')`
   before starting; a fresh Ego page did not default to blank in the earlier
   experiment. This corrects a setup assumption, not a permission denial. Do not
   touch a user tab, clear storage/caches, inject auth, or reopen a loaded history
   page as a prerequisite. If the authorized context cannot be reused or the
   target redirects to login, record that exact setup/auth outcome and stop.
2. Read the **existing** saved ordinary history URL and the two original UI
   message IDs from the writer receipt. Do not acquire them through DB or a new
   HTTP probe. URL identity is recipient Bot
   `791af2c4-2d89-4c57-b863-5d5ff53ff9f7`, session
   `c736a091-4c7d-4196-9cd6-00588de2a0ae`, with only `?session=S`.
   A slug canonicalization is allowed; task/run query or a different session is
   not. The original delegation/run identities remain preserved in `RECORD`.
3. Import [the initial-load observer](capture-initial-history.mjs) into the
   writer's existing foreground script, passing the original page and supported
   append-only sanitized JSON sink:

   ```js
   import { captureInitialHistory } from './capture-initial-history.mjs'

   const report = await captureInitialHistory(page, {
     historyUrl: originalSavedOrdinaryHistoryUrl,
     messageIds: [originalUserUiId, originalAssistantUiId],
     append: appendOneJsonRecord,
     captureMs: 45000,
     sampleMs: 500,
     commandMs: 3000,
   })
   ```

   These are observation budgets, not a new product timeout or proof of a hang
   at45 seconds. Keep the original executable session if the executor yields.
   Do not use another reader, screenshot helper or background process that
   drains `page.events()` during the window. No new server/service is started.
4. The observer verifies blank, enables Network/Runtime/Page **before application
   navigation**, saves/drains setup events, then makes exactly one supported
   `page.cdp('Page.navigate', {url: originalUrl}, {timeout: ...})` call. There is
   no `goto` completion/network-idle wait on the application. Sampling starts
   after that command returns and does not depend on DCL, a pane, an unmasked
   Home, successful messages, or a task button. If a transient execution-context
   error prevents a sample, retain that actual error and partial capture; this
   helper does not restart navigation or silently retry the failed command.
5. Save the journal and returned summary; on an exception retain its original
   `cause` through the writer's supported error receipt and its sanitized
   `captureState`. Do not blindly print raw SDK errors, URLs, headers or content.
   A journal failure is not retried and no subsequent drain/navigation occurs.
   A command failure stops; no alternate reader/fetch or permission workaround.
   The navigation result can be uncertain even when its command throws. Do not
   invoke the collector again, refresh, open Tasks, or correct the route.
6. After the window, capture/inspect the unchanged final UI through the writer's
   supported screenshot flow if available, labelled after-window (not complete
   event coverage). Close only that owned page/space normally and retain its
   cleanup receipt. No old PID/proc cleanup or production deployment is included.

## What the journal supplies

- Original **component** request IDs, selected identity query, resource type,
  status, cache/SW flags and terminal events. Status200 without `loadingFinished`
  stays pending. Expected SSE/EventSource streams are separately listed and
  must not be diagnosed as finite requests stuck forever. Unrelated workdir/
  container404 is recorded but does not stop the history observation.
- Bodies are read only with `Network.getResponseBody` after completion200 of
  the exact original session/messages request. The saved projection contains
  IDs, role, array/count and timestamp schema metadata only. This creates no
  second API request. No directory/task-detail request is sent. A parsed body
  does not prove SDK normalization, client commit or the selected pane accepted
  it. Initial static document/script paths and cache flags help identify what
  actually loaded; no asset body/header/token is copied.
- Creation/handshake/close/error IDs for the target Bot's real socket;
  sent subscribe/unsubscribe and received target-session snapshot/delta metadata,
  epoch/seq and snapshot Bot/session/run/turn. No raw frame text, transcript or
  synthetic subscription is sent. Preserve each socket lifecycle: a snapshot
  on an old/closed socket cannot automatically satisfy a newer subscription.
- DOM samples including Home mask, scroll display, skeleton count, message-ID
  presence/visibility and anchor placeholder, even when history never renders.
  No private store, guessed internal flag or new UI field is read/added.
- Runtime exception class/source-frame paths, Console error types and the exact
  static history-error signature, without user content. Extension errors remain
  separate. All CDP `sessionId` fields identify target sessions, not Memoh S.

The passed d74 experiment retained its local Console/data-fetch markers and
DOM count1. It did not test WS or guarantee retention across this navigation.
Navigation/transport event completeness remains explicitly unknown. Raw
`{method,params,sessionId}`/direct-result shape is already field-confirmed;
an actually changed envelope is saved as a changed condition, not guessed away.
Only retained positive events can be used; absence alone cannot establish that
the component never requested data, the server omitted a snapshot, or Ego lost
one. Host journal time and CDP timestamps have different meanings/units.

The observer makes one document navigation and reads already-delivered component
responses; it submits no business command or WS frame. Unexpected non-read HTTP or
non-observation outbound WS causes observation to stop after preserving metadata;
this cannot undo traffic already sent by the application. Original history
permission/availability failures stop without fallback. It does not alter TLS,
permissions, network, DOM, consistency semantics, storage or product behavior.

## Narrow decision after this single run

| Positive evidence in this run | Next isolation target, not yet a product root cause |
| --- | --- |
| Original message request starts but fails/is canceled, or remains without a terminal event | Its REST/SDK lifecycle; inspect the recorded terminal/request evidence, not a manual API200 |
| Original message body completes with original IDs, target subscription is observed, but no matching snapshot is retained | Snapshot-gated commit remains a candidate; WS delivery/observer coverage is unresolved, so absence does not prove the cause |
| Original body completes and the matching snapshot is retained, yet skeleton/zero rows persist | Client consumption, view/generation ownership or commit/normalization lifecycle; isolate the observed order in the existing real controllers before changing code |
| REST body is malformed/missing original IDs, or a static history-load error is observed | Narrow schema/window/SDK failure using this request's evidence; do not invent/delete/replay history |
| Home masks before the pane exists | Admission/explicit-session selection; preserve it as a different gate |
| Original messages appear without refresh | This initial-load sample converged; original field failure remains unproved/unfixed |

No field execution of this new observer has occurred here. Its four new narrow
contract tests and targeted ESLint/syntax/diff checks passed locally; the old
eight tests and passed contract experiment were not rerun. No full app/runtime,
new product fix, field red-green or Harmony/ArkWeb acceptance is claimed. The
next writer result, not a source-only hypothesis, selects any product isolation.
