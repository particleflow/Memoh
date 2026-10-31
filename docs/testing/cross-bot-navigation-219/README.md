# Cross-Bot historical conversation navigation

Baseline: `219a722da3b8e4a316164011dd106350dfab2c9e`. This is a shared Web navigation fix, not a deployment or an execution/recovery change. No API, SDK, migration, media transfer, or model request changes are included.

## Reproduction and correction

The field writer reported that both conversation task and related-Bot entries reached the correct expert session but also requested the supervisor session under the expert Bot, returning 404. The isolated browser fixture now returns 404 for a mismatched Bot/session pair instead of accepting any session UUID.

The first real browser run reproduced two wrong-scope GETs after the click. Pinning the Dockview panel owner removed one. CDP async initiator evidence located the remaining GET in `useComposerPair.reload`, invoked while Dockview cleared the old panel: a child watcher observed the new global Bot alongside the old pane's session before disposal. Capturing the pane's original Bot removed that request too. Neither guard relaxes permission checks or changes the actual session identity.

Separate red/green route tests reproduce an older Bot lookup consuming a newer route's session query and an older name lookup replacing a newer historical route. The homepage now checks the initiating route and current selection after those awaits.

## Checks

- Navigation/entry/contact/panel tests: 26 passed; the initial two route and two panel ownership assertions failed before their corresponding fixes.
- Final route, composer pair, pair synchronization, send-context, and conversation-relation tests: 72 passed. The route file was rerun after adding three denied-history cases; these totals are not additive counts of distinct tests.
- Full Web types and affected ESLint passed. UI contract passed with three existing warnings; no rule or baseline was weakened.
- Actual Chromium, current Vite source at `http://127.0.0.1:18407`, synthetic intercepted HTTP/WS: related-Bot row opens the original expert session; durable fixture text renders before and after reload. Wrong-scope reads: 0. Post-click business writes/contact creation: 0. Page errors: 0. This is not a real model/device/production test.
- [Narrow screenshot](expert-narrow.png) and [desktop screenshot](expert-desktop.png) show the original expert conversation and retained synthetic message after reload. [Request result](browser-result.json) records the exact synthetic paths. The [browser fixture](browser.py) accepts `tasks` to exercise the task entry instead of the related-Bot entry; it expects the current dev server on the loopback URL above and writes evidence only under `/tmp/unc-integration`.

## Old snow conversation boundary

An empty historical UI does not establish whether the original image tool executed. No synthetic message, tool receipt, terminal state, or output is inserted into that history. The current field evidence lacks the old snow session's messages response. The minimal remaining read is its real session ID, HTTP status and `items` count for `GET /bots/{bot}/messages?session_id={session}&limit=30`, plus returned role/turn IDs and message-block types (without text, attachments, credentials, or URLs). If necessary, compare persisted visible-turn/message existence for the original run. Do not retry generation to fill this gap.

## Version boundary

NAS remains on the separately deployed 219a release until a new candidate is explicitly released and applied. The signed HAP 1.0.22 is unchanged. A future mobile package needs the same three shared source changes (`home/index.vue`, `home/components/dockview/panel-chat.vue`, `home/components/chat-pane.vue`) to include this fix; this task does not build or sign it. Existing permission failures remain failures; no contact fallback, new session, Pi replay, or model dispatch is introduced. Human QA and field regression remain unverified.
