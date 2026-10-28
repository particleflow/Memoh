# Task-to-Agent navigation: P1 correction

Corrected source: `2b3ffb40fe615426a01bff8720111eef22e6e0e3`, appended to
`fix/task-agent-entry-20261028` without rewriting the original branch/history.
Production base remains `565c0b4b3386d87d057234efe6828479d35fcee0`.
Unified delivery owner: `01a15840-f35b-757a-a2a0-e20479062007`.

The previous `0c0b99c4250793696b609fc74768a5cfc09d3c00` was rejected: filtering
Tasks to `kind === 'agent'` hid real computer tasks and falsely rendered an empty
list when there was no subagent. Its test treating that behavior as success has
been replaced. Root-level screenshots/logs outside `p1/` record that superseded
attempt; they are not current acceptance evidence.

## Actual data and navigation contract

- The existing scoped task API's `session_id` is the receipt's persisted owning
  conversation. It is not automatically an Agent. There is no separate task-to-
  Web-Agent-session pointer in that response, and a native Pi session/file/profile
  name or `bot_agent_id` configuration ID is not a Web session ID.
- Tasks excludes structural Bot nodes but retains both Agent and computer-task
  rows, including submitted/running, terminal/history and unknown states. It does
  not infer Agent execution from a computer receipt's state.
- The reader reuses the scoped API's root-membership guarantee. A computer-only
  receipt does not need a separate subagent ledger row to remain visible. If an
  owning Agent node exists, nesting is preserved regardless of response order;
  otherwise the receipt stays under its authorized aggregation root.
- A computer task gets a destination only when the ordinary session GET confirms
  its persisted owner is a subagent or an existing external Agent runtime. The
  existing canonical session-mode/runtime helpers handle current and legacy
  forms. The original Agent title is retained; an unbound/missing owner uses the
  existing localized Computer task label. Names/prompts are never identity keys.
- A plain Bot owner, missing session reference, missing old owner, changed ancestry
  or inaccessible owner leaves the real receipt and original state visible, with
  its row disabled. It is not substituted by an empty list, Bot link, first Agent,
  configured Agent or new session. A denied scoped task-list read still clears
  that unauthorized view according to the existing reader policy.
- Clicking a linked computer task first re-reads its original scoped task GET and
  verifies type, task ID, aggregation root and owning Agent session. It then
  re-reads that ordinary session, verifies Bot/session identity and that it is
  still an Agent, and opens the original Bot + `?session=<id>` route. Permission
  failure, changed association, source-list changes and auth/scope changes do not
  become navigation or fallback. Existing subagent and Bots-mode routes remain.

The new `ConversationRelation.task` object is an internal view contract containing
only the original receipt/root/confirmed destination. No API/SDK/schema change,
new detail field, logs, runtime service, Agent core change, dispatch or migration
is added. History, old queues and existing permissions are preserved.

## Executed evidence

- [Rejected-source regression](p1/tests-rejected-source.log): counterexamples fail
  against `0c0b99c`, including task-only and multiple-task cases.
- [Current directed tests](p1/tests-current.log): 68 passed, no skips, using only
  the changed trigger/reader and affected existing row fixtures. Beyond preserving
  prior Agent routes, these check computer-only lists, distinct task/Agent IDs,
  supported current/legacy Agent owners, no-subagent-ledger cases, response-order
  independent nesting, terminal/history states, missing/mismatched old owners,
  task and session 401/403/404, changed type/ID/root/session and a pending response
  after the list changes its association. These behaviors, not the count, are
  the acceptance coverage.
- [Web TypeScript](p1/typecheck.log), [changed-file ESLint](p1/eslint.log),
  [UI guard](p1/ui-guard.log), and [repository staged checks](p1/staged-checks.log):
  exit 0. UI guard retains its three existing warnings. No full test matrix,
  backend test/build, Web production build or native package build was repeated
  for this shared UI-only correction.

The existing current-baseline Server/embedded Channel, PostgreSQL/pgvector and
current-source Vite were reused through the prepared helper. Real login,
authenticated database reads, proxy and unauthenticated-access rejection passed.
Chromium used `http://127.0.0.1:18082` with Go Server `http://127.0.0.1:18080` and
the same isolated QA Bot and original sessions from the first attempt. No new
fixture database records or runtime runs were required for this correction.

Only task-list/detail and onboarding metadata were intercepted. All task entries
in this browser fixture are computer operations, with no subagent-list rows.
Authentication, ordinary session identity/authorization and missing-owner GET404
used the actual local Server/PostgreSQL. Synthetic task metadata tests the UI
contract; it is not evidence of live computer/Agent execution or native mapping.

Screenshots were captured and inspected:

- [Rejected filter](p1/rejected-filter-false-empty.png): one real-shaped submitted
  computer task incorrectly becomes an empty list in the rejected source.
- [Only task, no Agent](p1/only-task-no-agent.png): that receipt remains visible,
  submitted and disabled under the corrected source.
- [Multiple tasks/history](p1/multiple-tasks-and-history.png): four distinct task
  rows survive; the two confirmed Agent owners are clickable and the ordinary
  Bot/missing-owner records remain visible and disabled.
- [Denied/changed association](p1/denied-association-stays-on-tasks.png): the list
  stays in place after a denied task read and changed owner; no fallback occurs.
- [Original Agent destination](p1/bound-task-original-agent.png): the task owned
  by Agent A opens A's original tab/session; the narrow flow separately opens B.
- [390px](p1/narrow-retained-tasks.png): all four real-shaped tasks and their
  original statuses remain, including disabled records.

[Browser result](p1/browser-result.json) records exact original session reads,
scoped task checks, denial/association-change outcomes, zero API writes and zero
page exceptions. The initial harness expected an incorrect English status
literal; it was corrected to the existing locale string. One test invocation
[overlapped the temporary rejected-source screenshot capture](p1/tests-during-baseline-capture.log)
and failed against that temporarily restored source. It is not a current-source
result. Final tests ran after restoration with stable corrected files; final
browser evidence was also refreshed against the corrected source.

## Harmony handoff and remaining acceptance

The inspected 1.0.26 artifact ref `b6896b78405c793e318aae3e4231ab0a7658fed2`
pins mobile source `87477f610a68cf99d383bc505266c322d242d747`. Its existing builder
copies hash-locked `sharedConversationMonitor.files` over the shared Web paths.
The original five changed overlay files matched baseline565 byte-for-byte.

[harmony-overlay.patch](harmony-overlay.patch) replaces the previous handoff
against that original mobile source. It now includes the trigger source/test,
reader source/test and row source: five mirrors, plus their five SHA256 and
`sourceCommit` entries pinned to corrected source `2b3ffb40...`. The reader/row
changes are necessary; updating only the original two trigger mirrors would
leave the disabled-row and trusted-association contract inconsistent.

`git apply --check`, application to extracted original files, exact equality of
all five mirrors to the corrected source commit, and post-apply lock hashes and
sourceCommit values passed. [Compatibility record](harmony-compatibility.json)
contains full hashes and paths. The mobile builder's pinned Web base `9bb6fc3...`
already provides the canonical session helpers and existing task-detail SDK
export; no new API/SDK overlay is required. No mobile branch was modified,
package built, signature produced or deployment performed.

Unified owner should combine the source correction and this updated evidence/
replacement handoff. If the old mobile patch was already applied or mobile head
changed, read it back and reconcile these exact files before applying; do not
stack the replacement patch blindly or overwrite other mobile adaptations.

Field acceptance must explicitly cover: a submitted/running computer task with
no subagent; multiple tasks with different recorded Agent owners; tasks with an
ordinary Bot owner, no owner or deleted/denied owner; terminal/history states;
changed task associations and permission revocation during navigation. Compare
the original task IDs, owning session IDs, Bot IDs and route destination rather
than names alone. Unlinked tasks must remain visible and disabled, with no
fallback or new execution. Recheck existing subagent/Bots navigation, history and
queues. On Harmony, verify installed package/source/hash versions and repeat the
same interactions on the actual device. Human, NAS, ArkWeb/device and real native
computer-task acceptance remain unverified; source/tests/screenshots do not mark
them passed.
