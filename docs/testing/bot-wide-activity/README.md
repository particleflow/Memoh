# Activity discovery across accessible Bots

## Gap and source checkpoints

The reviewed d2b UI was a current-context projection, not global discovery:
`session-activity.vue` filtered `chat.knownSessions` to the selected Bot, its
runtime observation required that Bot's synchronized WebSocket, and its task
and delegation readers took the current root session. Unopened Bot/session
activity could therefore be absent. The ordinary sessions list exposes no run
state; Bot `ready` and Agent `enabled` describe configuration, not execution.

Local merge checkpoint `9253f2515370812a1bb0fccdf1f29e5e69de4ca7`, tree
`23dd0813918a44061a959ff109805c44ab2de91f`, preserves both reviewed parents:

- UI: `d2bade8b30e4b6df778bc2c0b402f3b1fe592eeb`.
- Agenvo retirement: `e2cb0767eb12a54abd670d754c72ab7b4b7798c8`.

The merge had no conflicts. d2b did not change locales; all three locale files
retain e2cb exactly. Old queues/history and the native shared adapters are kept.
This follow-up does not modify official Agent runtime/contextview code.

## Minimum read contract

`GET /bots/{bot_id}/activity?limit=100&cursor=...` reads existing persisted
session runs and computer operations, independently of the open conversation.
The UI consumes the existing accessible Bot catalog, with bounded concurrent
reads, and follows original cursors internally. Page size is not a total count
limit. It does not open every historical transcript or create runtime sockets.

Response: `items[{id, kind, bot_id, session_id, name, state}], next_cursor`.

- Bot: active model/professional Bot session runs; name is the ordinary Bot name.
- Agent: active subagent or external Agent runs; use registered Agent name when
  the session has a Bot Agent binding, otherwise its actual session title.
- Task: submitted computer receipts, reusing the existing owner, original
  target/fingerprint and task-state projection. Submitted does not prove that
  Pi is running or that business results were delivered.

Only the existing active ledger states accepted/running/waiting_decision/
finishing and submitted task state are listed. Unknown, terminal, deleted,
history-reset and superseded-generation records are not promoted into activity.
No queue, ledger, execution/recovery action, service, configuration framework or
permission is created. Unadmitted drafts, configured idle Bots/Agents and queued
inputs that have not become runs are not claimed as active executions.

The account JWT follows the existing middleware; chat-route credentials cannot
use this account-management read. Ordinary session permissions are checked for
every ancestor, with the stricter existing owner/target checks retained for
computer receipts. No prompt, transcript, logs, target config, token or receipt
content is serialized. Genuine candidate/session database errors propagate;
an unreadable or vanished item is filtered. Cursors are encrypted and scoped
to the caller, Bot and this projection, distinct from owned task-list cursors.

## Shared surface and compatibility

The existing `panel-sessions.vue` still mounts `SessionActivity` above Folders;
the Bot Settings shortcut remains unchanged. The rendered rows contain only a
name, real state and normal original-session navigation. Navigation rechecks the
ordinary session GET before changing Bot/session; it never submits work.
The new reader is `pages/home/composables/useBotActivity.ts`; generated SDK and
`components/sidebar/session-activity.vue` are its shared consumer changes.

This new global read requires this backend. A server without the route falls
back to the old current-context projection, not a false claim of global
coverage. A denied global read is not replaced with cached success. A normal
page reload after the backend upgrade enables the new route;404 is not polled
indefinitely. The earlier d2b normal-session navigation itself uses preexisting
APIs and does not require this new backend. HAP1.0.14 was built before this
follow-up; neither a new signed package nor its native entry discoverability is
claimed. The original Harmony owner owns that port and device verification.
The deleted unsolicited attachment recovery button/explanation is not restored.

## Executed evidence and remaining checks

- Local merged Web typecheck and the retirement workspace-target test passed.
- 40 affected Web tests passed, covering unopened Bot discovery, three row types,
  original links, missing names, denied/foreign rows, internal continuation,
  transient recovery, stale A→B→A success/rejection and old-server fallback.
- Full Web and SDK typechecks, changed-file ESLint, Web build and UI guard
  passed. Existing chunk warning and three UI-contract warnings remain.
- Handler/service targeted tests passed, including account/chat-token boundary,
  permission revocation, ancestor checks, target revocation, stale run IDs,
  cursor scope and read-only projection. No runtime dispatch port is called.
- Real isolated PG plus the real HTTP handler/JWT and permission services passed:
  unopened roots/child Agent, actual registered Agent name, active-only/latest
  generation, original run row hash preservation, stable continuation and
  cross-team exclusion. This test is appended to the existing mandatory PG
  step and required-PASS set; previous gates/coverage are retained.
- Actual Chromium with current shared source and the existing isolated Server/PG
  showed all three compact row kinds and navigated to the original authorized
  specialist session. Activity states were synthetic HTTP responses; ordinary
  target-session authorization was real. Zero model/business writes, native
  observations, hidden file-tree reads or page errors. Desktop and narrow/Chinese
  images were inspected. This is not PC/Harmony/production execution acceptance.
- Initial new fixture ordering/type/cast failures and final successes are kept.
  Local scoped Go lint twice failed with no disk space in its type cache; one
  intervening analysis found four specific style diagnostics, all fixed. Final
  formatting passed. Full lint must be established by exact-head remote CI,
  not described as locally green. Only known task-owned reproducible caches
  were reclaimed; source, logs, DB and originals were preserved.

SQL was generated with the already-installed official sqlc v1.31.1 binary.
The source invocation required Go1.26 while the environment uses1.25.7; no tool
or dependency was upgraded. Swagger/SDK were regenerated normally. Compared
with the merge checkpoint, exactly one GET operation was added and none removed.
Schema remains177. Application rollback preserves newer data and uses the prior
image; no generic migrate-down or history/volume deletion is applicable.

Human QA and native-shell/physical-device acceptance remain incomplete. Source,
CI, image publication and NAS deployment are separate checkpoints.
