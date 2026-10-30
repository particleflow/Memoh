# Native contact transcript projection

**Later field evidence:** See [the original Bot acceptance ledger](bot-continuity-acceptance.md) for the completed b965 Web parallel/result sample and earlier046 historical-directory acceptance. The isolated results and limits below describe this original slice; they are preserved rather than treated as current all-client acceptance.

This slice changes shared Web presentation only. The server-resolved contact
entrance opens a contact-scoped panel. Native (`model`) contact panels omit
raw structured assistant tool activity and typed background-task notifications.
Provider-returned reasoning and runtime process status remain visible.
Ordinary user and assistant text is never matched
against tool names or result wording. Completed questions, errors, notices and
attachments remain visible; pending decisions continue to use the intact store.

Task and history entries open the complete transcript. Contact and history panels
can refer to the same session without sharing their presentation mode. The mode
is local presentation intent, not an authorization claim. Session access and task
destination checks are unchanged. No messages, tool results, execution state,
permissions, API definitions, or runtime-core code are modified.

## Executed validation

Environment: isolated local PostgreSQL, existing round-4 Server binary, current
Vite source at `http://127.0.0.1:28082/bot/local-dialog`, real Chromium, and a
loopback scripted provider. No paid model calls or production deployment.

- Red: before the change, the actual contact showed four native "Spawn agent in
  background" activity rows. Fresh screenshot: `projection-red.png`.
- Green live: a real `spawn_agent` and real child `exec` ran. A provider barrier
  held the coordinator's final response after its tool call. DOM observation
  recorded zero process-row appearances. The contact showed the existing generic
  working indicator. Screenshot: `projection-live-pending.png`.
- While the child remained held, another user message received a new natural
  response in 0.55 seconds. After release, the normal result summary appeared.
  Screenshots: `projection-live-conversation.png`, `projection-contact-final.png`.
- User text `Spawn agent in background; execution state: unknown` remained visible
  live and after reload. The same words in assistant text are covered by component
  tests. No keyword-based filtering is used.
- The actual Tasks directory opened the child's execution session. Its command,
  successful output and final reply remained available live and after reload.
  Screenshot: `projection-task-full-final.png`.
- An explicit history deep link to the contact session itself displayed its
  original spawn tools. Returning to the contact entrance and reloading hid them
  again. Screenshot: `projection-contact-debug.png`.
- A Chinese, dark, 390-pixel browser retained the contact summary and usable input.
  Screenshot: `projection-dark-zh-narrow.png`.
- Switching and inspecting the two projections preserved all 147 source-Bot
  history rows, including content and metadata; before/after ordered SQL digest
  was identical (`9c68d0fc233ed6f8109aa570f6778037`). Zero page errors were observed.
- Four affected test files passed, 119 tests total: message rendering, workspace
  panels, panel ownership, and pending decisions. Web typecheck passed. Changed
  files passed ESLint with only three pre-existing test-stub warnings. UI contract
  guard passed with its three existing unrelated warnings. `git diff --check`
  passed. Unchanged broad backend suites were not rerun.

Screenshots and sanitized JSON/log evidence are under
`/workspace/bot-dialog-handoff`. The first browser fixture waited for a phrase its
provider did not emit because it selected a background-status input. That
expectation was excluded; the corrected V6 provider/barrier supplied the live
evidence above. The original V5 task was retained and completed normally.

## Acceptance boundaries

This is shared Web source for Web/HAP integration, not a signed HAP build or a
device/production acceptance result. Human QA remains unverified. Exact run/turn
anchoring is still separate work: existing task links open the authorized original
execution session. External-agent UI projections are unchanged. No additional
attention/recovery management interface was added or made an acceptance gate.
