# Account contact initialization during the history observation

**Follow-up received:** the parent now relays the two existing request IDs, Bot
labels, resolver route family and completed200 timing. See [cefdf0 source and
scope alignment](cefdf0-source-alignment.md). Do not repeat the old excerpt
request below. Literal UUID paths/API origins were not forwarded in this thread;
the writer can use its already-saved actual values for the exact manifest.
Old body/initiator/binding effects remain unknown, without another probe.

## a766 field result: truncated observation, not an authorization failure

The parent relays `F:/NAS-Assistant/tmp/initial-history-a766/{OUTCOME.json,journal.jsonl}`:
host Node timing worked, with no Page/890 timer timeout. Two naturally initiated
contact POSTs completed200/`loadingFinished`; the observer did not submit them.
The original history's component messages GET completed200 in27.572ms. Only an
early `interactive` DOM sample (no dock/pane) was collected before the observer
stopped for the POST method. No target runtime/socket was recorded before that
stop. The writer closed space62 with no in-flight activity. This owner has not
read the raw Windows files.

No contact body/before-state was captured, so reuse, first binding, creation and
their side effects remain unknown. The fast message HTTP completion is not SDK
normalization/commit or visible-history success. Absence of an early WS frame
cannot identify the hydration cause. No additional field attempt, manual API,
DB read, model, deployment or history correction accompanies this follow-up.

## Exact9270 source semantics

These paths were compared with commit9270 and have no differences in the relevant
handler/coordinator, Web bootstrap/tab caller and SQL/thread creation seams.

| Request/branch | Actual behavior and authorization |
| --- | --- |
| `POST /api/bots/{bot_id}/sessions/contact`, no body | `SessionHandler.ResolvePrivateChat` rejects chat-route tokens, requires the authenticated account/channel identity, UUID Bot and existing `chat` permission, derives the account on the server and rejects a client body. It subsequently rechecks the returned session's normal read permission, including external-runtime permission where needed. It creates no role/grant or impersonated identity. |
| Same resolver, valid existing contact | `resolveContinuousContact` first inspects the Bot+account `web/contact:<user>` key and returns its live authorized session. Existing bindings win across devices; it does not select a recent task, merge messages or retarget history. Reopening this branch is identity-idempotent. |
| Same resolver, no binding and one eligible legacy candidate | The system calls `BindPrivateChat` internally, under transaction/ownership/visibility/local-parent/route checks. It can attach that ordinary account-owned conversation to the new contact route. This is a real first-binding mutation, not a read. Multiple candidates/cursors conflict409; an uncertain mutation is not repeated. |
| Same resolver, no binding/candidate | `EnsurePrivateChat` atomically ensures its route and stages a new account-owned contact; native lock/uniqueness rules converge concurrent requests. Normal closed/tombstoned/foreign identities are not treated as permission to replace a contact. Creation can publish session-created and invoke the configured `SessionStart` hook after commit. No direct model call is present in this handler; actual hook effects/cost cannot be inferred from these two HTTP200s. |
| `POST .../sessions/contact/bind` | A method body named `BindPrivateChat` remains in source, but9270's actual `SessionHandler.Register` routes this public POST to `rejectParallelChat` (403). Do not classify the dead method's old godoc as a normal initialization endpoint or allow it. |
| Other POST/PATCH/DELETE, explicit account/route/session body, query or unknown Bot/origin | Outside this observation's initialization exception; record the request and stop without claiming its already-sent effects were prevented. |

Source anchors:

- `internal/handlers/session.go:148` (real route registration),
  `internal/handlers/session_contact.go:37` (account/permission/body checks).
- `internal/handlers/fixed_contact.go:37` (reuse/unique adoption/creation decision),
  `internal/channel/route/contact_navigation.go:125` (transactional first binding),
  `internal/channel/route/contact.go:30` (native route/session creation).
- `db/postgres/queries/channel_routes.sql:51` (`EnsurePrivateChatRoute` conflict
  update preserves the existing active session), `internal/chat/thread/route_create.go:13`
  and `service.go:830` (creation notification/SessionStart hook).
- `apps/web/src/composables/api/useChat.chat-api.ts:232` submits the bodyless
  generated resolver; `store/chat/bootstrap.ts:67,74,205` calls it for normal
  contact admission. The explicit history branch skips bootstrap's ordinary
  contact resolution while its history intent is visible.
- `store/workspace-tabs.ts:995,1170` also permits empty-dock recovery through
  `openDraftChat -> openContact`, with pending/selection/loading fences. That is
  another possible startup caller. These source paths do **not** establish the
  initiator or cause of either specific field POST; no initiator was supplied.

Thus ordinary already-authorized login/Bot viewing can include contact admission
without obtaining any new user privilege. An observation guard need not stop
solely on its HTTP method. The guard is not a server ACL and cannot prevent an
already-sent request. Its exception must still preserve actual binding effects
as unknown rather than treating every resolver200 as proven no-op.

## Minimal existing-file excerpt still needed

The parent summary lacks the two exact paths. Read only the already-saved journal
once for each POST's `requestId`, `path` (Bot UUID included, no query), `method`,
`status` and `terminal`. If initiator source positions were already captured,
include them; otherwise mark unrecorded. No request/response body, cookie, token,
DB, browser reopening or new HTTP call is needed for this classification. This
excerpt request is pending, not denied. It distinguishes repeated same-Bot
resolution, two Bot scopes, `/bind` or another actual route. Do not claim those
two field calls match the normal resolver before seeing their paths.

## Narrow observer correction, ready for cloud review

[The policy](initialization-contact-policy.mjs) is used only by the diagnostic
[initial-load observer](capture-initial-history.mjs). No product permission,
handler, UI or Bot code changed. It permits continued passive observation only
for a bodyless/no-query POST to exact `/api/bots/<UUID>/sessions/contact` on an
explicit API-origin/Bot manifest. Defaults are the original history's origin
and recipient Bot. An additional Bot/origin can be included from the existing
authorized deployment/journal receipt; adding it to a diagnostic manifest
issues no request, grants no access and does not select its conversation.
Unknown route, unmanifested scope, nonempty/opaque body, `/bind`, denial/error,
changed schema/identity or a non-observation WS command still stops. No force,
retry, permission change or fallback reader is added.

After a **naturally issued** resolver request completes200/`loadingFinished`,
the observer reads that already-delivered response through `Network.getResponseBody`
and saves only returned contact session/Bot/account/route IDs, type and creation
timestamp, plus sanitized existing initiator source positions. It never sends a
POST or replacement GET. Without a saved before-state, `bindingEffect` stays
`unknown`; repeating the same ID in this capture is not proof of no mutation.
Saved known contact IDs are optional and must come from an existing authoritative
receipt. `ownerIdentityId` means the exact saved `created_by_user_id` channel
identity returned by the contact resource; do not substitute a guessed login
account ID, JWT subject or display name. A different known identity or changed account/Bot scope is recorded
then stops; the observer never repoints the task URL or restores a binding.

For the same original-record observation, the sole writer can later supply:

```js
await captureInitialHistory(page, {
  historyUrl: originalSavedOrdinaryHistoryUrl,
  messageIds: [originalUserUiId, originalAssistantUiId],
  append: appendOneJsonRecord,
  // Exact IDs/origins from existing receipts; never guess another Bot or fetch
  // a contact to populate this manifest. The raw two field paths are pending.
  initializationContactBotIds: verifiedInitializationBotIds,
  initializationApiOrigins: verifiedApiOrigins,
  // Optional saved authority only. Omit if unknown; no new metadata/API probe.
  knownContacts: savedKnownContacts ?? [], // {botId, ownerIdentityId, sessionId, routeId?}
})
```

This is preparation, not an instruction to occupy the writer while distribution/
HTTP investigation has priority. No field retry is executed here. On a future
separately coordinated capture, keep the original initial-load plan's one
navigation/no refresh/no task click and real-exec-session cleanup rules.

## Narrow verification and unresolved boundary

One new synthetic integration case first failed under a766 specifically at
`Non-read request observed`, before response metadata/WS could be collected.
With the route/scope policy it continues through two manifested normal resolver
responses and retains original messages/WS evidence. New countercases preserve
stopping for `/bind`, another unmanifested Bot, body/identity injection, foreign
origin, and returned identity differing from the saved authority. Their effects
stay unknown; there is no second navigation or rebinding. Three new targeted
tests passed with lint/syntax checks. The old8/4 tests and unchanged builds were
not rerun. [Saved red/green result](initialization-contact-contract-tests.txt)
is **collector** validation, not product hydration red/green or NAS acceptance.

The a766 natural POST effects and actual startup caller are unresolved until
existing evidence can distinguish them. The historical rendering/REST-runtime
commit cause is still unknown. Do not turn the fast HTTP sample, an early empty
DOM, this collector correction or source-only authorization into a product fix.
