# Minimal activity navigation and unrelated workspace error

Base: `50d69c7b77be0dda884e2708db6167a1d561bba0`. This is an independent UI follow-up, not part of that published image or HAP 1.0.13. No backend/schema/SDK/permission/runtime changes.

## Requested surface

- Keep the existing compact sidebar list of genuinely active recorded entries, their names/states, and original conversation links. Hide terminal/unknown records from this active surface without changing their stored state.
- Remove the chat-footer Task records dialog entry. The existing components/APIs/history are retained; the ordinary computer task tool card still retains its existing native resume control. This change does not invent a replacement diagnostic surface.
- A missing conversation title falls back to the ordinary Bot name, not the user's request body. A professional Bot's recorded authorized session is navigable before its RunID exists; run-specific history consumers retain their original stricter guard. Relation and target-session permission revalidation remain mandatory.

## Confirmed workspace error path

In the existing isolated Server/PG page, Cloud Computer was selected and persisted task/delegation GETs returned 200. Before this follow-up, the hidden Files panel's FileTree mounted and called `/container/fs/list` three times; its 503 became the visible workspace-unreachable toast in the chat page. This reproduces an unrelated error source, but does not establish that every reported field occurrence had the same HTTP cause.

Only the directory-list function is changed: hidden panels do not issue a read; a failed read does not retry or toast after the panel is hidden or its Bot changes. Explicitly opening Files still issues its original read/retry and displays the real workspace error. Upload/download handlers, background journals and attachment bytes are unchanged. No remote target, origin or authority is substituted.

## Executed checks

All final commands exited 0:

- `pnpm exec vitest run apps/web/src/components/sidebar/session-activity.test.ts apps/web/src/pages/home/components/session-tasks.test.ts`: 71 passed, including existing run-history denial/stale-relation cases.
- `pnpm exec vitest run apps/web/src/components/sidebar/files-pane.test.ts`: 4 passed. First test run failed because the new test supplied a non-Pinia stub to real `storeToRefs`; test adapter corrected, product not changed for that failure.
- `pnpm --filter @memohai/web typecheck`: full Web typecheck passed. First earlier run found the new request-turn fixture missing required turn fields; fixture corrected, initial output retained.
- ESLint on the six changed source/test files: passed.
- `pnpm --filter @memohai/web build`: passed, existing >500 kB chunk warning retained.
- `node scripts/check-ui-contract.mjs`: passed, three existing contract warnings and existing baselines retained.
- Real Chromium rendered current source against the existing isolated Server/PG, existing synthetic account/session relationship and ordinary API authentication. Activity list states were intercepted synthetic data, not live model/device work. Account runtime list was deliberately 503 and an unselected PC was offline; Cloud Computer remained selected. Actual target relation/session GET revalidation and navigation reached the original specialist Bot/session without requiring its RunID. Zero business writes, model calls, device observations, hidden file-tree reads or page errors. Desktop/dark and narrow/Chinese screenshots inspected.

The browser result is not a production, physical PC, Harmony native engine or real provider acceptance. Human QA remains incomplete. No HAP build/signature or NAS deployment was performed. The current projection still covers synchronized observations for the selected Bot and its selected root's task/delegation API; it is not proof of exhaustive global live discovery across every Bot.

## Harmony-only recovery entry

The exact unsolicited text was found in `mobile/client/mobile/memoh/HarmonyAttachments.vue` at HAP source `5d0f2df38d57588f313831459dac749d125918a9`; current shared Web has no matching button. Independent one-file follow-up `f7637546` removes exactly three lines: the recovery button, its explanatory paragraph, and the now-unused `restore` binding. It adds no replacement entry or automatic recovery request. Original selection, removal, preview authorization, cancellation, native transfer implementation and stored records remain unchanged.

Vue SFC parse/compile plus a rendered component check passed: both recovery button variants/explanation absent, original attachment row/removal retained, zero native calls. This is not a new signed package or device verification. Apply the precise one-file commit through the original Harmony owner; do not import the full phone branch into the Web source or silently rebuild the existing 1.0.13.
