# Harmony audit: fixed Bot contact and persisted task directory

Read-only consumer audit against Harmony product source `1155614c210c131968a4eb3e586cb515ad7d625b`. No product, shared UI, server, SDK, permission, version, HAP, signing, icon or field configuration is changed. The original cloud worktree is executable and clean at this source; the previously downloaded SDK remains present. No environment replacement or SDK download was used.

## Verified incoming source

Library `libfile_aeb7be4b95f08191bef4fa9938346a46` version2 was materialized by the current official helper in this consumer environment. Actual ZIP:197753 bytes, SHA256 `ac46b9e5543ad5cdc00617d3a4b2594930f11cb4a3e7af5790c4834b87ceb3af`. CRC and all39 `SHA256.json` members pass. Its supported Git bundle verifies against local a0 ancestry and contains HEAD `e5a1aa75cf79267b659b63b24bc0ee60cd46b7e7`; objects were read without switching or merging the consumer branch. The old `incomplete` filename is consistent with the owner's explicit incomplete-feature handoff, not evidence of a damaged ZIP.

Consume only the requested shared differences from contact commit `9f3dc3a288dd472aa039b8098b9f0d6808a2a46a` and directory commit `e5a1aa75cf79267b659b63b24bc0ee60cd46b7e7` after unified review. The cumulative bundle also contains earlier result-origin/server changes; this audit does not implement or copy them. Owner tests/screenshots in the ZIP are prior evidence, not tests or device acceptance run by this consumer.

## Minimum consumption list

| Concern | Actual1155614 gap | Minimum existing integration seam |
| --- | --- | --- |
| Plain Bot entrance / same-Bot return | `pages/home/index.vue` skips selection when resolved Bot equals current Bot. `chat/bootstrap.ts` can keep an explicitly restored task or draft and can inspect/adopt a unique historical candidate. No `contactNavigationPending` exists. | Consume9f deltas in `index.vue`, `store/chat-list.ts`, `store/chat/bootstrap.ts`, `store/workspace-tabs.ts`. Plain entrance resolves the existing server contact directly; explicit `?session=` retains history. Preserve the phone app-title and file-reference adapters. No new contact endpoint or grant. |
| Mobile cold layout | Current `workspace-tabs.restoreLayout` intentionally skips desktop layout on mobile, clears the dock and may create a fallback draft. Empty-dock guards do not know contact pending. | Retain this mobile layout rule and add the shared pending/complete guards to prevent fallback draft or panel focus cancelling contact resolution. Do not import desktop splits to solve phone identity. |
| Real task directory | Current footer requires `activeSessionId && !isWelcome`; its Tasks trigger reads per-session relations. New component/reader and both SDK GETs are absent. An empty contact cannot expose this directory. | Add shared `bot-task-directory.vue`, `useBotTaskDirectory.ts`, three locale `chat.taskDirectory` subtrees and generated SDK additions. In `chat-pane.vue`, replace only Tasks with BotTaskDirectory under `currentBotId && voiceInputState==='idle'`; retain the separate related-Bots trigger. Keep names, state and original-destination navigation only. No extra native list or detail panel. |
| Builder pins and exact source guards | The consumer is a chain of guarded overlays, not a whole shared Web checkout. Old broad chat-pane import context differs, although the footer and trigger import each have unique current anchors. | Use the existing sourceChanges/SDK/locales/addedFiles mechanism with exact new hashes. Retain native voice, four attachment entries, TLS, login, source/adapter checks and image timing. Do not overwrite full SDK/locales or relax drift assertions. |
| Task deep link / original destination | Old related trigger does `chat.selectBot` before `router.push` and then directly opens a tab. New directory correctly authorizes task+session then lets the explicit route alone own selection/tab opening. | Use the shared directory navigation unchanged. Its compatible route is `/#/bot/{destination.bot_id}?session={destination.session_id}` under the existing hash router. Retain opaque task/record IDs and never substitute current Bot/contact. The remaining related-Bots trigger has the older two-writer path; verify that neighboring path against the new contact guards before claiming combined navigation safe, without changing shared logic independently. |
| Native back from direct task URL | With no layer/header-back action, `harmony-back.ts` unconditionally treats route name `bot` as root, even with `query.session`; SharedChat then moves the ability to background. The narrow chat header is Menu, not a back action. | A concrete phone-owned return seam still needs agreement: only an explicit task/history URL should return to the same authorized Bot's plain contact via the existing route, retaining task tabs/data and never resolving a different origin. No implementation is made in this audit. Root Bot behavior, modal-first back and drafts/receipts must remain. |

[Exact file hashes and SDK declarations](consumer-difference.json). Contact adds four product files' deltas and five companion tests. Directory adds/changes ten shared product inputs (six Web/locales plus four SDK files) and two companion tests. This is a consumption list, not a new phone implementation or full-tree equality claim.

## Existing API and identity contracts

Contact uses the already generated resolve/inspect/bind functions; no new credential or local session-ID configuration is required. Directory adds account-authenticated, no-store reads:

- `GET /bots/{bot_id}/tasks?limit=20&cursor=…` → `{items, next_cursor?}`. Continue nonempty cursor even for an empty filtered page; no total-history truncation or automatic full-history load.
- `GET /bots/{bot_id}/tasks/{task_type}/{record_id}` → the original occurrence rechecked before navigation, followed by the existing session GET.
- Item: `task_type` (native_agent/computer_operation/bot_delegation/schedule_fire), opaque `task_id`, separate durable `record_id`, `name`, source `state`, optional `destination:{bot_id,session_id,run_id?}`. `created_at` is transport data, not an extra list column. Missing destinations leave real rows visible and disabled. A chat-scoped token is rejected; the current account login remains the correct identity.

All calls use the existing configured HTTPS API base and account Authorization path. No new native network/bridge/TLS permission is needed. `/activity` and session relations are not fallback substitutes for a missing directory. An older server404 must remain an unavailable interface, never a successful empty directory or a new task submission.

## Login persistence is distinct from primary-conversation identity

The two incoming commits do not change the shared auth-session, login page or router relative to a0, and do not touch any mobile file. Consumer native settings, old HTTP→HTTPS binding migration, token namespace and WebView storage remain unchanged. Therefore this is not a request to clear login, rebind a session, move task ownership or add a login prompt. What is missing is route-intent priority over a saved task/draft, not a new credential store. A successful upgrade login remains a device acceptance item; none is claimed here.

## Source-only narrow-back probe

[Probe source](back-route-probe.cjs), [actual output](back-route-probe.json). It transpiles the existing phone handler using the already installed TypeScript and an empty synthetic DOM. Plain Bot and explicit task-session Bot routes both return `false` with zero route calls. The corresponding unchanged `SharedChat.ets:124–139` dispatches the Web handler and calls `moveAbilityToBackground` for a nontrue result. This demonstrates the source-level missing distinction; it does not claim a new ArkWeb/phone execution or failure screenshot.

The existing narrow header (`mobile-top-bar.vue`) uses Menu for every active chat, so it provides no dedicated direct-task return action. Shared9f has desktop and390px upstream browser evidence, but it does not automatically verify this native Back bridge. After the unified head is fixed, the minimum phone checks are empty-contact Tasks visibility, correct original task deep link/reload, same-Bot contact return with saved task/draft present, modal Back before contact return, and names/state/navigation only. Do not repeat unrelated matrices or call paid models for this source audit.

## Deliberately unclosed upstream scope

The dedicated owner's directory covers persisted native admissions, computer receipts, incoming/outgoing handoffs and schedule fires; it explicitly excludes background work lost before durable evidence, and does not finish unknown reconciliation, publication barriers or full real-model/media/PC/phone acceptance. The phone must not invent missing tasks, reconstruct opaque IDs or replay unknown work to fill this gap. Unified composition is still under review; no production, CI, device or complete directory acceptance is claimed by this audit. All icon drafts remain excluded.
