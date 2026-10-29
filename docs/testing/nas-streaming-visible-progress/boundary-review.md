# Boundary review: own public output versus internal Agent/Bot dialogue

Reviewed original production commit `12d753df21cd508f0b0d962f4b71cfd3b1830a6c`
and durable handoff `fa39572ca4a218807a34e8348a9d3b37de71789a` against the
2026-10-29 clarification: show the current main Bot's own public reasoning,
status and incremental reply; do not project its internal interactions with
other Agents/Bots into the main contact. Preserve execution-context history
under the existing permissions.

## Decision

**The original production patch can enter source integration for this structured
provenance boundary, accompanied by this additional test evidence.** No runtime
implementation gap was found in the reviewed paths. The review did find a
coverage gap: the original 77 Web tests exercised own deltas, generic raw-tool
filtering and a host-result run, but did not directly interleave parent, child,
cross-Bot and internal-return traffic. A new mixed-sequence test fills that gap.
Only the test and evidence files change in this follow-up.

`message-item.vue` is byte-identical to the original corrected version:
Git blob `c7f574be6a4d79eb0c4da68952e0a4a6075770f7`, SHA-256
`b1ad58fc11a51c6441eaae3353109738459e99ac77dc87571765260054444d38`.
No Agent core, provider prompt, ACL, queue, persisted history, migration or
production service is changed. This source admission decision does **not**
establish a fully green baseline lint, live NAS nonstreaming root cause,
Harmony integration/build, human QA or device acceptance.

## Where the identity boundary is enforced

- The same-Bot child uses its own `req.agentSessionID` in
  [SpawnIdentity](../../../internal/agent/tool/subagent.go#L1036).
  [SubagentRunObserver](../../../internal/agent/application/subagent_step_commit.go#L213)
  publishes through the admitted child's Bot/session/run handle. It does not
  relabel child public deltas as the parent's own provider output.
- A cross-Bot delegation starts a command for `TargetBotID` and
  `TargetSessionID` in [delegationHost.Run](../../../cmd/internal/core/bot_delegation.go#L301).
  The source result delivery later admits its distinct result-summary run.
  It does not merge the target's dialogue stream into the source contact.
- [WS subscription authorization](../../../internal/handlers/runtime_ws.go#L307)
  remains in place and subscribes using the authorized Bot/session pair.
  Outbound `session_id` comes from that subscription. The public WS frame need
  not carry a top-level Bot ID; the Bot comes from its authenticated connection.
- [Realtime callbacks](../../../apps/web/src/store/chat/realtime.ts#L118)
  reject old connection generations/Bots. The runtime client tracks
  [subscriptions, epoch and seq](../../../apps/web/src/store/chat/runtime-client.ts#L145)
  per session. Projection delivery selects the corresponding Bot/session view,
  including when an authorized child/debug view is open concurrently.
- Main-contact `hideProcessActivity` still hides structured `tool` blocks except
  the existing completed user Q&A. Therefore `spawn_agent`, `send_message` and
  `delegate_to_bot` inputs, outputs and progress do not render in main chat.
  Restoring reasoning/status removes no tool filter and does not expand the
  selected transcript to other sessions.
- Host-only `TaskResultReceipt` / `DelegationResultID` make
  [NewRequestUserTurn return nil](../../../internal/agent/view/uimessage_request_turn.go#L54).
  Internal input remains saved with provenance but
  [does not become a history user bubble](../../../internal/agent/view/uimessage_convert.go#L247).
  A source Bot's newly generated natural result reply uses its own new run;
  that reply and its own public generation events may render.

None of these decisions searches prose for tool names, Agent names, task IDs,
"thinking", or other keywords. The test intentionally puts `delegate_to_bot`
in **own** public reasoning and text and requires it to remain visible.
The public text/reasoning contract attributes a block to a run; it does not
provide sentence-level provenance inside that run's prose. This review proves
that target dialogue/tool payloads are not inserted as main-chat blocks; it does
not claim semantic redaction of arbitrary quotations a model might generate
inside its own public output. No hidden chain of thought is requested or invented.

## Matrix and exact assertions

The new test is
[`keeps parent public deltas isolated from child, cross-Bot and internal-return traffic`](../../../apps/web/src/pages/home/components/message-item.test.ts#L178).
It uses the actual realtime controller, runtime projection client, transcript
controller and message component, with the existing manager-emitted wire fixtures.
Two Bot transports and three simultaneously subscribed views deliberately reuse
block IDs while keeping Bot/session/run ownership distinct. Its tool-group test
double serializes tool payloads when mounted, so the negative assertions cannot
pass merely because a stub silently ignores tool input/output/progress.

| Traffic | Main contact | Existing execution/debug view | Proof |
| --- | --- | --- | --- |
| Parent own public reasoning/status/first text delta | Visible before later text/terminal | Preserved | New assertions at lines 247-250; own prose includes a tool name |
| Parent `spawn_agent`, `send_message`, `delegate_to_bot` tool input/output/progress, live and completed | Hidden | Payloads still readable | New lines 251-265: main lacks `PRIVATE_TOOL`; debug positively contains input, output and progress |
| Same Bot, different child session: request/reasoning/retry status/reply | Absent from main | Child transcript contains it | New interleaving at 242-268; main lacks `PRIVATE_ALPHA` and child retry status; debug contains them |
| Other Bot, target session: request/reasoning/status/reply | Absent from main | Target transcript contains it | Independent target transport; main lacks `PRIVATE_BETA`; debug positively contains target reply |
| Internal saved-result return, same source Bot/session but new summary run | No machine user bubble; own natural summary streams | Original task context remains separate | Captured `result` contract; new lines 273-277 require visible natural reply and exactly one original user turn |
| Late snapshot from the old target Bot connection after Bot switch | Cannot alter main or old cached target projection | Existing data retained | New lines 278-288 require unchanged target seq and main DOM |
| Ordinary text and completed user Q&A | Visible | Retained | Original 77-test assertion, linked below; unchanged |

The `PRIVATE_*` strings are neutral fixture payload markers used only for test
assertions; filtering never uses those strings. Child/target output is positively
rendered in the separate debug section, proving it was processed and retained.
The new test does not grant access: server-side authorization is unchanged, and
the debug sections model views that an already-authorized subscriber opened.

## Reused evidence, without rerunning it

The original [77-test log](web-tests.txt) remains the result from 08:46 UTC.
At exact commit `12d753df`:

- [Direct and host-result delta tests, lines 177-219](https://github.com/particleflow/Memoh/blob/12d753df21cd508f0b0d962f4b71cfd3b1830a6c/apps/web/src/pages/home/components/message-item.test.ts#L177)
  assert public reasoning/status, first fragment before second, streaming state,
  no machine-context text and zero result-run user bubbles.
- [Raw tool/Q&A preservation, lines 222-244](https://github.com/particleflow/Memoh/blob/12d753df21cd508f0b0d962f4b71cfd3b1830a6c/apps/web/src/pages/home/components/message-item.test.ts#L222)
  assert ordinary text, public reasoning and completed Q&A; original tool object
  and result remain intact; switching the presentation restores the tool group.
- The existing [Go result-origin log](result-origin-tests.txt) was also reused.
  `TestBotTaskResultQueueVisibilityAndReplay` asserts empty queue text, no live
  user bubble, noneditable internal input and retained origin/model input.
  `TestBotTaskResultHistoryRetainsInputAndOnlyRendersExtractedReply` asserts saved
  machine input is retained but only the natural assistant reply enters UI turns.
  `TestNativeTaskResultIsInternalAcrossQueueLiveAndResume` asserts native host
  receipts hide queue/live input while preserving the receipt. These cover
  `DelegationResultID` and `TaskResultReceipt`, respectively. The old log contains
  a skipped PostgreSQL admission integration test; this review does not upgrade
  it to passed or claim new database/authorization testing.

## New executed evidence

- [Only the new mixed test](boundary-mixed-test.txt): 1 passed, 13 other tests in
  that file skipped by `-t`. The old 77 Web and 16 Go checks were not rerun.
- [Changed-test ESLint](boundary-eslint.txt): zero errors, three existing stub
  warnings. [Typecheck](boundary-typecheck.txt) passes after the added test.
- The actual current Vite/Chromium page at
  `http://127.0.0.1:28082/bot/stream-fixture` was exercised without rebuilding or
  any model request. Existing captured runtime frames supplied own reasoning,
  status and first text; typed delegation tool input/output/progress and stale
  unsubscribed-child frames were interleaved before the next own text fragment.
  [Current screenshot](boundary-parent-first.png) was captured and inspected;
  [result flags](boundary-ui-result.json) record own output visible, internal
  payloads absent, first text before later/terminal events and zero page errors.
  [Replay input](boundary-ui.cjs.txt) is included. This local browser case covers
  parent tool hiding and unsubscribed child frames; the new controller/DOM test
  covers concurrently subscribed child and cross-Bot views. Neither is live
  NAS/device evidence or a new permission test.
- No provider call, paid request, deployment, signing or history deletion.
  Full lint's existing `subagent.go:389,401` findings remain as recorded in
  [full-lint.txt](full-lint.txt); no unrelated repair or rerun was performed.
