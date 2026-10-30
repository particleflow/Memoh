# cefdf0 source alignment and next initial-history observation

## Existing-only receipt update

The parent relays the writer's existing-file read from
`F:/NAS-Assistant/tmp/hap136-publication/observations-existing-only.json`:

| Original request | Bot label in that receipt | Relayed route family | Terminal evidence |
| --- | --- | --- | --- |
| `181448.409` | 红玉 | POST `.../sessions/contact` | HTTP200 + `Network.loadingFinished`,18.706ms |
| `181448.417` | 文本校验Bot | POST `.../sessions/contact` | HTTP200 + `Network.loadingFinished`,17.660ms |

No new page/API/DB was used for that excerpt. Initiator was not recorded; body
was not captured. Prior binding/creation/adoption/hook effects remain unknown.
This owner has not read the Windows JSON. The relayed route **family** now
matches the normal account resolver, not the closed `/contact/bind` endpoint.
Do not keep asking for the already-supplied method/status/terminal evidence.

The parent message gives names, not the literal full paths/Bot UUIDs/API origin.
Do not infer a UUID from either display name or equate either with recipient T.
For the next run the sole writer can use its already-saved actual path values
directly to fill544's exact Bot/origin manifest. No new identity/API lookup is
needed. No request-body/initiator value can be reconstructed from the HTTP200s.
Against a matching native resolver,200 implies its account/permission/body-length
checks succeeded; it still does not prove an unchanged binding or which branch
ran. Live `hasPostData`/query/origin checks remain in the observer.

## Source comparison actually performed

Compared full commits `9270da0252128a296d9a641235cdfd32ab44cfda` and
`cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f` in Git. The parent reports production
Server now atcefdf0 with schema181; that report is not a new field read here.

**Byte-identical relevant seams:** Home route selection/index; task-run anchor;
chat-pane/footer DOM; BotTaskDirectory navigation; bootstrap and workspace tabs;
message/session/contact Web API calls; `transcript.ts` masked initial load;
`realtime.ts` initial-snapshot/atomic commit; server runtime WS subscription
handler; session/contact registration and authorization; native contact inspect,
bind/create coordinator and staged session creation. No new endpoint, permission,
task destination or initial-history wait is introduced in these seams.

**Relevant changes, not ignored:** the cefdf0 commit also contains Web sources,
including `message-item.vue` source labels, `chat-list.normalize.ts`,
`transcript-history.ts`, `runtime-client.ts`, `runtime-projection.ts`, shared
types/SDK and server UI-turn/input provenance. History normalizers copy typed
`input_source`; runtime projection carries the exact input-turn source under
Bot/session scope, with missing/foreign source kept unknown. Runtime-client's
delta projection receives that scope. These changes are downstream of the
unchanged REST/subscription/initial-snapshot boundary and do not remove it.
They are not evidence that hydration is fixed, nor proof of its cause.

The REST route and outer runtime frame's `type/session_id/epoch/seq/snapshot`
contract remain usable by544. The observer already ignores extra typed
`input_source` fields and records no prompt/frame transcript. Message IDs,
timestamps, snapshot scope and the pane's existing DOM witnesses remain
appropriate. It can therefore capture original component REST and runtime
hydration on the new source without modifying product behavior.

The Server revision alone does not establish which Web asset the browser will
execute. Do not state that the deployed browser definitely uses cefdf0 or that
the whole Web source was unchanged. The observer now records separately:
reviewed source revision; Server/Web revisions from existing receipts, with
`null` for unknown; and actual observed static asset paths/cache flags. This
metadata is not an asset-hash or production acceptance claim. Do not initiate
a new release/version API probe just to fill an unknown field.

## One next capture, scheduled only by the parent

1. Use the writer's two **already-read** actual request rows. Normalize them in
   its existing script to `{requestId,path,method,status,terminal}` using their
   actual file structure; no root property name for the unseen JSON is invented
   here. Derive the two Bot UUIDs from those exact paths. Keep both observed Bot
   scopes plus the already-authorized original recipient T in the manifest,
   deduplicated by actual UUID.
   Names are evidence labels, never route identity. A missing/malformed path is
   a configuration gap, not a reason to guess or open another page.
2. Use the existing verified API deployment origin(s), and optional saved
   `created_by_user_id`/contact/session/route authority. Do not read DB, fetch a
   contact, invent a before-state, copy a token or substitute a global account
   ID. Leave `knownContacts: []` if no prior authority was recorded.
3. When the parent schedules it, start one owned authorized page, explicitly
   `page.goto('about:blank')`, arm observation, and navigate once to the original
   ordinary history session. No task dialog/row click, manual API, snapshot send
   or refresh. Use [the initial-load collector](capture-initial-history.mjs),
   unchanged in its HTTP/WS/DOM collection since544:

   ```js
   await captureInitialHistory(page, {
     historyUrl: originalSavedOrdinaryHistoryUrl,
     messageIds: [originalUserUiId, originalAssistantUiId],
     append: appendOneJsonRecord,
     initializationContactBotIds: [...new Set([originalRecipientBotId, ...botIdsFromTheTwoSavedPaths])],
     initializationApiOrigins: existingVerifiedApiOrigins,
     knownContacts: savedKnownContacts ?? [],
     reviewedSourceRevision: 'cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f',
     serverRevision: 'cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f',
     // Existing artifact receipt only; keep null when not supplied/verified.
     webRevision: existingVerifiedWebRevision ?? null,
   })
   ```

   The existing45-second observation/500ms sample/3-second command budgets remain
   diagnostic budgets, not a product timeout or an assertion of WS completeness.
   Host Node timing was confirmed in a766; do not put the wait inside a Page
   evaluate timer or rerun the passed synthetic buffer contract.
4. Continue through only exact manifested no-query/bodyless resolver requests;
   save completed natural response identity, keeping `bindingEffect: unknown`.
   Do not reclassify old HTTP200 as proof of no change. Unknown body/origin/Bot,
   `/bind`, non200/failure, changed known identity, actual read denial or other
   mutation still stops without replay or correction. The collector never sends
   a resolver POST, prevents an already-sent one, or rebinds history.
5. Preserve the component message GET's actual terminal/body schema and target
   socket/subscription/snapshot timeline together with DOM skeleton/message-ID
   witnesses, then close only that owned page/space normally. Missing events
   stay unknown. The recorded27.572ms HTTP completion on the old run is not
   interchangeable with the new run's SDK commit or with a manual API200.

This owner made no field call and does not take over the released writer.
Only source comparison, metadata correction, lint/syntax/diff checks were done;
old8/4/3 contracts, builds and prior samples were not rerun. Neither the natural
resolver effects nor the historical hydration root cause has been established.
