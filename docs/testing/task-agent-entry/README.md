# Task entry opens related task Agents

Base: `565c0b4b3386d87d057234efe6828479d35fcee0`. Independent branch:
`fix/task-agent-entry-20261028`. Delivery is for unified owner
`01a15840-f35b-757a-a2a0-e20479062007`; no deployment, package build, or signing.

The mounted footer entry is `components/sidebar/session-activity-trigger.vue`,
not the retired global `session-activity.vue`. Its Tasks mode previously rendered
the entire conversation relation tree, including the root Bot and linked Bots.
Clicking those rows correctly followed their Bot session, which was the wrong
object to offer as a task Agent. The existing Agent-row route already preserves
the exact Bot and session; it does not require a new route or configuration page.

Tasks now renders only the tree's `kind === 'agent'` entries. These are built
from the scoped task's original `session_id`, authorized session GET and persisted
ancestry, not Agent names, provider names, run IDs, `bot_agent_id` configuration
IDs, or the first available Agent. Names and states retain their existing source.
The original Bot nodes remain in the reader for membership validation, and the
Bots entry keeps its original behavior. Filtering the view preserves nested
Agent relationships and promotes an Agent whose immediate parent is a Bot to a
visible list root. Cross-Bot child Agents retain their own original Bot/session.

Missing or denied old Agent objects produce no invented destination. Unnamed
Agents retain the existing localized Agent fallback. Computer-operation receipts
whose only recorded owning conversation is a Bot are not Agent destinations;
they remain in existing task/history APIs and are not offered here as Bot links.
The existing empty state applies when no related Agent exists. No stored task,
history, queue, permission, API, SDK, schema, Agent core, name/status rule, detail
field or log surface was changed.

## Executed checks

- [Regression reproduction](tests-red.log): before the product change, 7 failed
  and 6 passed in the expanded trigger fixture; the root/linked Bots and old
  receipts were still visible under Tasks.
- [Final directed tests](tests-final.log): 30 passed, no skips, in the existing
  trigger, conversation-relation reader and home navigation fixtures. Covers
  exact whole-row routing, two same-name Agents, nested Agents, linked-Bot Agents,
  unnamed and missing old objects, original states including unknown, unchanged
  Bots mode, 401/403/404 navigation denial and mismatched returned identities.
- [Web TypeScript](typecheck.log), [changed-file ESLint](eslint.log), and
  [UI contract](ui-guard.log): exit 0. UI guard retains its three existing warnings.
  [Repository staged checks](staged-checks.log) also passed (size checks and
  lint-staged/ESLint; no Go package selected). No full test matrix or production build was rerun for this one-expression UI
  change. Initial installed dependencies contained two Vue versions; frozen
  lockfile installation restored the declared versions without tracked changes.
  Restarting the owned dev processes resolved Vite's cached missing-import error.
- Current baseline Server was compiled and run with embedded Channel plus the
  existing local PostgreSQL/pgvector and current-source Vite. The prepared
  startup helper's login/database/proxy/authentication smoke checks passed.

## Actual browser evidence and limits

Chromium used the actual Vite UI at `http://127.0.0.1:18082` and Go Server at
`http://127.0.0.1:18080`. One isolated QA Bot and three original sessions were
inserted in the local development database; no runtime run was submitted.
Authentication and original session GETs used the real Server/PostgreSQL.
Scoped task GET metadata supplied synthetic running/waiting-decision states;
delegations were empty. The browser's onboarding read was adapted in memory.
These are synthetic task fixtures, not live Agent or production run evidence.

- [Before](before-bot-entry.png): actual baseline Tasks includes its Bot row.
- [Desktop after](desktop-agents.png): only Task Agent A/B and their original
  projected states; no Bot, receipt IDs, prompts, logs or new detail fields.
- [Agent B destination](agent-b-destination.png): clicking the second Agent opens
  its own named tab and original session, not the Bot conversation or Agent A.
- [390px after](narrow-agents.png): same two Agent rows and states; clicking Agent
  A opens its own session. Both destinations and real session reads are recorded
  in [browser-result.json](browser-result.json). Zero API writes and page errors
  during the final browser interaction. Screenshots were inspected.

The first browser attempt hit the stale Vite import; a second used network-idle
waiting against persistent streams and timed out. After dependency/process
recovery, DOM-ready plus actual control/route assertions passed. Baseline was
temporarily restored only to capture the before image; final source was restored
and the corrected flow/screenshots were freshly verified afterward.

Human QA, real task execution, NAS and physical Harmony/ArkWeb acceptance remain
unverified. Local checks and screenshots do not establish any of those.

## Harmony compatibility and owner handoff

Read-only inspected artifact ref
`b6896b78405c793e318aae3e4231ab0a7658fed2` (1.0.26); its README pins source
`87477f610a68cf99d383bc505266c322d242d747` and compatible unified baseline565.
At that source, `mobile/client/mobile/build_memoh_web.py` loads
`config/memoh_web.lock.json` → `sharedConversationMonitor.files`, checks hashes,
and copies the mobile shared monitor files into the Web paths. All six trigger,
reader and row source/test overlays were byte-identical to this Web baseline.

The current mobile override therefore retains this bug even if Web alone is
cherry-picked. [harmony-overlay.patch](harmony-overlay.patch) is a separate,
reviewable handoff against that exact mobile source. It updates only the trigger
mirror, its test mirror and their two lock hashes. `git apply --check`, application
to extracted original files, post-apply byte equality with the fixed Web files,
and matching lock hashes passed. Exact paths and before/after hashes are in
[harmony-compatibility.json](harmony-compatibility.json). No mobile branch was
modified or pushed and no HAP was built. Preserve the existing shell, login,
media/transfer, directories, themes and signing adaptations.

## Acceptance plan

1. Unified owner cherry-picks this commit onto the intended combined candidate;
   if its shared source differs, reconcile only this Tasks projection and retain
   its current identity/authorization route.
2. Apply the mobile overlay handoff to the actual mobile source, retaining the
   exact shared source/test bytes and updated lock hashes. A different mobile
   head requires read-back and reconciliation before applying; do not overwrite
   concurrent changes or reuse stale package evidence.
3. In Web and the eventual owner-built Harmony client, open a conversation with
   at least two recorded task Agents. Tasks must show those Agent names/states,
   omit root/linked Bot and Bot-owned receipt links, preserve nested Agents, and
   navigate each clicked row to its own original session. Check same-name Agents
   separately by original session IDs; do not infer identity from display text.
4. Verify a linked Bot's child Agent, unknown/terminal recorded statuses, an
   unnamed legacy Agent, no-Agent empty state, missing/deleted Agent and revoked
   target permission. Missing or denied objects must never select another Agent,
   open the Bot, create a new session, or resubmit work. Confirm Bots mode and old
   history/queues remain available.
5. Record exact source/overlay/package versions, original session IDs, screenshots
   and actual observed results. Native device/production acceptance belongs to
   the unified delivery owner; this handoff does not mark it passed.
